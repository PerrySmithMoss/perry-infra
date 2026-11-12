package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Parse Netdata's quoted plugin protocol without executing shell input.
func protocolFields(line string) []string {
	var result []string
	var field strings.Builder
	var quote rune
	started, escaped := false, false
	for _, r := range line {
		switch {
		case escaped:
			field.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				field.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case r == ' ' || r == '\t' || r == '\r':
			if started {
				result = append(result, field.String())
				field.Reset()
				started = false
			}
		default:
			started = true
			field.WriteRune(r)
		}
	}
	if quote != 0 || escaped {
		return nil
	}
	if started {
		result = append(result, field.String())
	}
	return result
}

func collectedContexts(output string) map[string]int {
	contexts, chartCycles := map[string]string{}, map[string]int{}
	chart, numeric := "", false
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		fields := protocolFields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "CHART":
			if len(fields) >= 7 {
				contexts[fields[1]] = fields[6]
			}
		case "BEGIN", "BEGIN2":
			if len(fields) >= 2 {
				chart, numeric = fields[1], false
			}
		case "SET", "SET2":
			if len(fields) >= 3 {
				value := fields[2]
				if value == "=" && len(fields) >= 4 {
					value = fields[3]
				}
				if number, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
					numeric = true
				}
			}
		case "END", "END2":
			if numeric && contexts[chart] != "" {
				chartCycles[chart]++
			}
			chart, numeric = "", false
		}
	}
	cycles := map[string]int{}
	for chart, count := range chartCycles {
		cycles[contexts[chart]] = max(cycles[contexts[chart]], count)
	}
	return cycles
}

func verifyPlugin(module string, required []string) error {
	fmt.Println("Checking live Netdata collector:", module)
	// Netdata 2.12 auto-enables debug jobs only on a terminal. script supplies
	// that terminal without changing the running daemon's dynamic job state.
	// The service account has a nologin shell. Override SHELL only for this
	// explicit command, without changing its account or granting login access.
	output, _ := captureCommand(nil, true, 22*time.Second, "runuser", "-u", "netdata", "--", "env", "SHELL=/bin/sh", "script", "--quiet", "--return", "--command", "/usr/libexec/netdata/plugins.d/go.d.plugin -d -m "+module, "/dev/null")
	// Debug output may contain the DSN: persist it only in root-owned storage.
	if err := os.WriteFile(filepath.Join(stateDir, module+"-verification.log"), output, 0o600); err != nil {
		return err
	}
	cycles := collectedContexts(string(output))
	for _, requiredContext := range required {
		found := false
		for context, count := range cycles {
			if strings.HasPrefix(context, requiredContext) && count >= 2 {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("collector %s lacks repeated numeric samples for %s; inspect its private verification log", module, requiredContext)
		}
	}
	fmt.Println("PASS:", module, "produced repeated numeric samples for all required metric contexts.")
	return nil
}

func redisReply(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil || len(line) < 3 {
		return "", errors.New("incomplete Redis reply")
	}
	switch line[0] {
	case '+', '-':
		return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
	case '$':
		size, err := strconv.Atoi(strings.TrimSpace(line[1:]))
		if err != nil || size < 0 || size > 4<<20 {
			return "", errors.New("unexpected Redis bulk length")
		}
		data := make([]byte, size+2)
		if _, err := io.ReadFull(reader, data); err != nil {
			return "", err
		}
		return string(data[:size]), nil
	default:
		return "", errors.New("unexpected Redis reply type")
	}
}

func redisRequest(connection net.Conn, args ...string) error {
	var request strings.Builder
	fmt.Fprintf(&request, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&request, "$%d\r\n%s\r\n", len(arg), arg)
	}
	_, err := io.WriteString(connection, request.String())
	return err
}

func verifyRedisACL() error {
	secret, err := os.ReadFile(filepath.Join(stateDir, "redis.password"))
	if err != nil {
		return err
	}
	connection, err := net.DialTimeout("tcp", "127.0.0.1:16379", 3*time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(connection)
	if err := redisRequest(connection, "AUTH", "netdata", string(secret)); err != nil {
		return err
	}
	if reply, err := redisReply(reader); err != nil || reply != "+OK" {
		return errors.New("Redis monitor authentication failed")
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"PING"}, "+PONG"},
		{[]string{"INFO", "ALL"}, "used_memory:"},
		// Read-only probes are safe even if permissions are accidentally broad.
		{[]string{"GET", "__netdata_permission_probe__"}, "-NOPERM"},
		{[]string{"SCAN", "0"}, "-NOPERM"},
		{[]string{"CONFIG", "GET", "maxmemory"}, "-NOPERM"},
		{[]string{"SLOWLOG", "GET", "1"}, "-NOPERM"},
		{[]string{"EVAL", "return 1", "0"}, "-NOPERM"},
	} {
		if err := redisRequest(connection, test.args...); err != nil {
			return err
		}
		reply, err := redisReply(reader)
		if err != nil || !strings.Contains(reply, test.want) {
			return fmt.Errorf("Redis monitoring ACL check failed for %s", test.args[0])
		}
		if test.args[0] == "INFO" {
			for _, name := range []string{"used_memory", "evicted_keys", "connected_clients", "keyspace_hits", "keyspace_misses"} {
				found := false
				for _, line := range strings.Split(reply, "\n") {
					if value, ok := strings.CutPrefix(strings.TrimSpace(line), name+":"); ok {
						_, err := strconv.ParseUint(value, 10, 64)
						found = err == nil
					}
				}
				if !found {
					return fmt.Errorf("Redis INFO lacks numeric %s", name)
				}
			}
		}
	}
	fmt.Println("PASS: Redis monitoring authentication, INFO/PING, key/enumeration/config/slowlog/Lua denials.")
	return nil
}

func verifyPostgresTLS() error {
	data, err := os.ReadFile("/etc/netdata/private-metrics-ca.crt")
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return errors.New("invalid PostgreSQL monitoring CA")
	}
	connection, err := net.DialTimeout("tcp", "127.0.0.1:15432", 3*time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	request := make([]byte, 8)
	binary.BigEndian.PutUint32(request, 8)
	binary.BigEndian.PutUint32(request[4:], 80877103)
	if _, err := connection.Write(request); err != nil {
		return err
	}
	response := []byte{0}
	if _, err := io.ReadFull(connection, response); err != nil || response[0] != 'S' {
		return errors.New("PostgreSQL did not accept TLS")
	}
	secured := tls.Client(connection, &tls.Config{RootCAs: pool, ServerName: "perry-infra_postgres", MinVersion: tls.VersionTLS12})
	if err := secured.Handshake(); err != nil {
		return errors.New("PostgreSQL CA/hostname verification failed")
	}
	if err := secured.ConnectionState().PeerCertificates[0].VerifyHostname("wrong-host.invalid"); err == nil {
		return errors.New("PostgreSQL certificate unexpectedly accepts a wrong hostname")
	}
	fmt.Println("PASS: PostgreSQL private path uses TLS with trusted CA and matching hostname.")
	return nil
}

func verifyBoundaries() error {
	for _, service := range []string{"postgres", "redis", "metrics-relay"} {
		output, err := checked(nil, "docker", "service", "inspect", "perry-infra_"+service, "--format", "{{json .Endpoint.Spec.Ports}}")
		if err != nil || (strings.TrimSpace(string(output)) != "null" && strings.TrimSpace(string(output)) != "[]") {
			return fmt.Errorf("%s unexpectedly publishes a port", service)
		}
	}
	for _, network := range []string{"postgres_metrics", "redis_metrics", "traefik_metrics"} {
		output, err := checked(nil, "docker", "network", "inspect", "perry-infra_"+network)
		if err != nil {
			return err
		}
		var networks []struct {
			Internal, Attachable bool
			Options              map[string]string
		}
		if err := json.Unmarshal(output, &networks); err != nil || len(networks) != 1 {
			return errors.New("cannot inspect metrics network")
		}
		_, encrypted := networks[0].Options["encrypted"]
		if !networks[0].Internal || networks[0].Attachable || !encrypted {
			return fmt.Errorf("%s is not internal/encrypted/non-attachable", network)
		}
	}
	output, err := checked(nil, "ss", "-H", "-lnt")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		host, port, err := net.SplitHostPort(fields[3])
		if err != nil {
			return errors.New("cannot parse socket inventory")
		}
		switch port {
		case "5432", "6379", "8080", "8082", "19999":
			return fmt.Errorf("unexpected host listener on %s", port)
		case "15432", "16379", "18082":
			if host != "127.0.0.1" {
				return fmt.Errorf("metrics port %s is not IPv4 loopback-only", port)
			}
			seen[port] = true
		}
	}
	if len(seen) != 3 {
		return errors.New("private loopback sockets are missing")
	}
	fmt.Println("PASS: no published database/relay ports; encrypted private overlays; only loopback collector listeners.")
	return nil
}

func verifyHTTP(root string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	data, err := os.ReadFile(filepath.Join(root, "monitoring/endpoints.tsv"))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n")[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			return errors.New("invalid health endpoint inventory")
		}
		request, err := http.NewRequest(fields[3], fields[1], nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("health request failed for %s", fields[0])
		}
		io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if strconv.Itoa(response.StatusCode) != fields[2] {
			return fmt.Errorf("unexpected health status for %s", fields[0])
		}
	}
	for _, path := range []string{"/api/overview", "/dashboard/"} {
		response, err := client.Get("http://127.0.0.1:18082" + path)
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			return errors.New("Traefik metrics endpoint unexpectedly serves API/dashboard")
		}
	}
	response, err := client.Get("http://127.0.0.1:18082/metrics")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err = io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || response.StatusCode != 200 {
		return errors.New("private Traefik scrape failed")
	}
	for _, metric := range []string{"traefik_service_requests_total", "traefik_service_request_duration_seconds_bucket", "traefik_service_request_duration_seconds_sum", "traefik_service_request_duration_seconds_count"} {
		if !strings.Contains(string(data), metric+"{") {
			return fmt.Errorf("Traefik lacks %s", metric)
		}
	}
	for _, service := range []string{"bulkbets-api", "chinwag-api", "fleet-api", "im-listening-api", "vita-track-api"} {
		for _, metric := range []string{"traefik_service_requests_total", "traefik_service_request_duration_seconds_count"} {
			found := false
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, metric+"{") && strings.Contains(line, `service="`+service+`@swarm"`) {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("missing Traefik %s for %s", metric, service)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(stateDir, "traefik-metrics-verification.txt"), data, 0o600); err != nil {
		return err
	}
	fmt.Println("PASS: all five public health checks; private service request/latency metrics; no Traefik API/dashboard.")
	return nil
}

func verify(root string) error {
	if err := waitForCloud(time.Minute); err != nil {
		return err
	}
	if _, err := checked(nil, "sh", filepath.Join(root, "monitoring/verify.sh")); err != nil {
		return fmt.Errorf("Netdata agent/Cloud verification: %w", err)
	}
	if err := verifyBoundaries(); err != nil {
		return err
	}
	if err := verifyPostgresTLS(); err != nil {
		return err
	}
	if err := verifyRedisACL(); err != nil {
		return err
	}
	if err := verifyHTTP(root); err != nil {
		return err
	}
	for _, test := range []struct {
		module   string
		contexts []string
	}{
		{"postgres", []string{"postgres.connections_usage", "postgres.db_transactions_rate", "postgres.db_locks_held_count", "postgres.db_locks_awaited_count", "postgres.db_deadlocks_rate"}},
		{"redis", []string{"redis.memory", "redis.key_eviction_events", "redis.clients", "redis.keyspace_lookup_hit_rate"}},
		{"traefik", []string{"traefik.entrypoint_requests", "traefik.entrypoint_request_duration_average"}},
		{"prometheus", []string{"prometheus.traefik_services.traefik_service_requests", "prometheus.traefik_services.traefik_service_request_duration_seconds"}},
	} {
		if err := verifyPlugin(test.module, test.contexts); err != nil {
			return err
		}
	}
	container, err := task("perry-infra_postgres")
	if err != nil {
		return err
	}
	output, err := psql(container, []byte(`SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls AND rolconnlimit = 3 AND pg_has_role(oid, 'pg_monitor', 'MEMBER') FROM pg_roles WHERE rolname = 'netdata_monitor';
SELECT bool_and(s.ssl) FROM pg_stat_activity a JOIN pg_stat_ssl s ON s.pid = a.pid WHERE a.usename = 'netdata_monitor' AND a.application_name = 'netdata';
`))
	if err != nil || strings.TrimSpace(string(output)) != "t\nt" {
		return errors.New("PostgreSQL monitoring role privileges or active collector TLS session did not verify")
	}
	fmt.Println("PASS: non-superuser PostgreSQL monitoring role and active TLS collector session.")
	if _, err := checked(nil, "netdatacli", "reload-health"); err != nil {
		return err
	}
	fmt.Println("Host checks passed. Inspect live Cloud charts and active alert rules, and test external port isolation before closing tasks.")
	return nil
}

// Restart readiness and Cloud reconnection are separate signals. The control
// pipe can respond before ACLK has reconnected after a successful installation.
func waitForCloud(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := command(nil, min(5*time.Second, time.Until(deadline)), "netdatacli", "aclk-state")
		if err == nil && cloudOnline(string(output)) {
			return nil
		}
		time.Sleep(min(2*time.Second, max(0, time.Until(deadline))))
	}
	return errors.New("Netdata did not reconnect to Cloud within one minute; use diagnose, then verify again after reconnecting")
}

func cloudOnline(state string) bool {
	claimed, online := false, false
	for _, line := range strings.Split(state, "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		if name == "Claimed" {
			claimed = strings.TrimSpace(value) == "Yes"
		}
		if name == "Online" {
			online = strings.TrimSpace(value) == "Yes"
		}
	}
	return claimed && online
}

// Output fixed diagnoses only: the log also contains private ACL inputs and
// potentially sensitive collector output, so never echo arbitrary log lines.
func logDiagnoses(log string) []string {
	known := map[string]string{
		"FAIL: agent not claimed/online. Check Cloud setup and outbound HTTPS.": "The final Netdata check ran before the agent was claimed/online.",
		"FAIL: Netdata control pipe did not become ready within 30 seconds.":    "Netdata's control pipe did not become ready within 30 seconds.",
		"FAIL: port 19999 is listening.":                                        "The Netdata check found a TCP listener on port 19999.",
		"Refusing to start: require exactly one [web] mode = none.":             "The Cloud-only configuration guard rejected the Netdata configuration.",
		"An infrastructure release or installation is in progress.":             "The infrastructure installation could not acquire its deployment lock.",
	}
	var result []string
	seen := map[string]bool{}
	for _, line := range strings.Split(log, "\n") {
		if message, ok := known[strings.TrimSpace(line)]; ok && !seen[message] {
			result = append(result, message)
			seen[message] = true
		}
	}
	return result
}

// Use the installed broker for both fingerprints; build artifacts need not be
// retained in the reviewed checkout. Keep child output private on failure.
func verifySnapshot(root, broker, snapshot string) error {
	expected, err := command(nil, 5*time.Second, broker, "fingerprint", root)
	if err != nil {
		return errors.New("cannot fingerprint the staged reviewed snapshot with the installed broker")
	}
	actual, err := command(nil, 5*time.Second, broker, "fingerprint", snapshot)
	if err != nil {
		return errors.New("cannot fingerprint the installed infrastructure snapshot")
	}
	if string(expected) != string(actual) {
		return errors.New("installed infrastructure snapshot differs from the staged reviewed snapshot")
	}
	return nil
}

func diagnose(root string) error {
	data, err := os.ReadFile(filepath.Join(stateDir, "operations.log"))
	if err != nil {
		return err
	}
	for _, message := range logDiagnoses(string(data)) {
		fmt.Println("DIAGNOSIS:", message)
	}
	if err := verifySnapshot(root, "/usr/local/libexec/perry-infra-release", "/opt/perry-infra-release"); err != nil {
		return err
	}
	fmt.Println("PASS: installed infrastructure snapshot matches the staged snapshot.")
	state, err := command(nil, 5*time.Second, "netdatacli", "aclk-state")
	if err != nil {
		return errors.New("cannot read Netdata Cloud status; no private output printed")
	}
	fmt.Println("Cloud claimed and online:", cloudOnline(string(state)))
	return nil
}
