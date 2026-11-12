package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the actual Netdata binary, PostgreSQL HBA/TLS/role, Redis ACL and
// Traefik router using disposable containers. No host binds or published ports.
func TestDisposableCollectors(t *testing.T) {
	if os.Getenv("METRICS_DOCKER_TEST") != "1" {
		t.Skip("set METRICS_DOCKER_TEST=1 with a Docker daemon")
	}
	prefix := "perry-metrics-test-" + time.Now().Format("150405.000000000")
	var containers []string
	network, denied, volume := prefix+"-network", prefix+"-denied", prefix+"-files"
	docker := func(input string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdin = strings.NewReader(input)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture docker %s failed: %s", args[0], output)
		}
		return string(output)
	}
	t.Cleanup(func() {
		for i := len(containers) - 1; i >= 0; i-- {
			exec.Command("docker", "rm", "-fv", containers[i]).Run()
		}
		exec.Command("docker", "volume", "rm", volume).Run()
		exec.Command("docker", "network", "rm", network, denied).Run()
	})
	docker("", "network", "create", "--internal", network)
	docker("", "network", "create", "--internal", denied)
	subnet := strings.TrimSpace(docker("", "network", "inspect", "--format", "{{(index .IPAM.Config 0).Subnet}}", network))
	docker("", "volume", "create", volume)
	redisImage := "redis:8.10.3-alpine@sha256:71885d8cc6e42f2bc0aa2eb853bfc4be200b5108a46ac50e117ae1df0531fb80"
	haproxyImage := "haproxy:3.4.7-alpine@sha256:2caaf8b1a1ef67f3f1ea9b721f2821675d3ba4500fb518e23884fecb93e8dbaa"
	traefikImage := "traefik:v3.7.17@sha256:f4bb143d1135a579b630ed491e17ec50f6f7c9c6a30585ff3d385471d2a86f5a"
	netdataImage := "netdata/netdata:v2.12.3@sha256:c7eab00e20901eb60595ebae0fbfe88e59464e663924461874719d40de0735d2"
	docker("", "pull", netdataImage)
	start := func(name, image string, options, command []string) string {
		t.Helper()
		name = prefix + "-" + name
		containers = append(containers, name)
		args := []string{"run", "-d", "--name", name, "--network", network, "--memory", "128m", "--pids-limit", "64"}
		args = append(args, options...)
		args = append(args, image)
		args = append(args, command...)
		docker("", args...)
		return name
	}
	seed := start("seed", redisImage, []string{"--mount", "type=volume,source=" + volume + ",target=/fixtures", "--entrypoint", "sleep"}, []string{"600"})
	directory := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		docker("", "cp", path, seed+":/fixtures/"+name)
		docker("", "exec", seed, "chmod", "644", "/fixtures/"+name)
	}
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	secret := strings.Repeat("a", 64) // disposable fixture only
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture"}, DNSNames: []string{"perry-infra_postgres", "postgres"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	write("ca.crt", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	write("server.key", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	docker("", "exec", seed, "chown", "999:999", "/fixtures/server.key")
	docker("", "exec", seed, "chmod", "400", "/fixtures/server.key")
	write("pg_hba.conf", strings.ReplaceAll(read("../../postgres/pg_hba.conf"), "10.42.20.0/24", subnet))
	pg := start("postgres", "postgres:17.11-bookworm", []string{"--network-alias", "postgres", "--network-alias", "perry-infra_postgres", "--mount", "type=volume,source=" + volume + ",target=/fixtures,readonly", "-e", "POSTGRES_HOST_AUTH_METHOD=trust"}, []string{"postgres", "-c", "ssl=on", "-c", "ssl_cert_file=/fixtures/ca.crt", "-c", "ssl_key_file=/fixtures/server.key", "-c", "hba_file=/fixtures/pg_hba.conf"})
	for attempt := 0; attempt < 40; attempt++ {
		// initdb's temporary server accepts Unix sockets before the final server
		// starts; require TCP so the subsequent psql cannot hit that restart gap.
		if err := exec.Command("docker", "exec", pg, "pg_isready", "-h", "postgres", "-U", "postgres").Run(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	docker(monitorSQL(secret), "exec", "-i", pg, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "postgres")
	role := docker("SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls AND pg_has_role(oid,'pg_monitor','MEMBER') FROM pg_roles WHERE rolname='netdata_monitor';\n", "exec", "-i", pg, "psql", "-XqAt", "-U", "postgres", "-d", "postgres")
	if strings.TrimSpace(role) != "t" {
		t.Fatal("monitoring role has excess privileges")
	}
	for _, dsn := range []string{
		"host=postgres dbname=postgres user=netdata_monitor sslmode=disable",
		"host=postgres dbname=template1 user=netdata_monitor sslmode=verify-full sslrootcert=/fixtures/ca.crt",
		"host=/var/run/postgresql dbname=postgres user=netdata_monitor",
	} {
		cmd := exec.Command("docker", "exec", "-e", "PGPASSWORD="+secret, pg, "psql", "-XqAt", dsn, "-c", "SELECT 1")
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "pg_hba.conf rejects connection") {
			t.Fatal("PostgreSQL HBA did not reject plaintext, another database or local-trust access for the monitoring role")
		}
	}
	acl, err := metricsACL("user default reset off\nuser application reset on >fixture-app ~app:* -@all +get +set\n", secret)
	if err != nil {
		t.Fatal(err)
	}
	write("users.acl", acl)
	write("redis.conf", strings.ReplaceAll(read("../../redis/redis.conf"), "/run/secrets/redis_users", "/fixtures/users.acl"))
	redis := start("redis", redisImage, []string{"--network-alias", "redis", "--mount", "type=volume,source=" + volume + ",target=/fixtures,readonly", "--mount", "type=volume,source=" + volume + ",target=/run/perry-metrics", "--tmpfs", "/data:rw,size=16m", "--read-only", "--cap-drop", "ALL"}, []string{"redis-server", "/fixtures/redis.conf"})
	write("haproxy.cfg", read("haproxy.cfg"))
	relay := start("relay", haproxyImage, []string{"--user", "0:0", "--mount", "type=volume,source=" + volume + ",target=/fixtures,readonly", "--mount", "type=volume,source=" + volume + ",target=/run/perry-metrics", "--read-only", "--cap-drop", "ALL"}, []string{"haproxy", "-W", "-db", "-f", "/fixtures/haproxy.cfg"})
	traefikConfig := strings.ReplaceAll(read("traefik.yaml"), "10.42.22.0/24", subnet)
	traefikConfig = strings.Replace(traefikConfig, "  routers:\n", "  routers:\n    fixture-error:\n      entryPoints: [web]\n      rule: Path(`/fixture-error`)\n      service: fixture-broken\n", 1)
	traefikConfig += "  services:\n    fixture-broken:\n      loadBalancer:\n        servers:\n          - url: http://127.0.0.1:9\n"
	write("traefik.yaml", traefikConfig)
	traefik := start("traefik", traefikImage, []string{"--network-alias", "traefik", "--mount", "type=volume,source=" + volume + ",target=/fixtures,readonly", "--read-only", "--cap-drop", "ALL"}, []string{"--entrypoints.web.address=:8080", "--entrypoints.metrics.address=:8082", "--metrics.prometheus=true", "--metrics.prometheus.entrypoint=metrics", "--metrics.prometheus.manualrouting=true", "--metrics.prometheus.addserviceslabels=true", "--metrics.prometheus.addrouterslabels=false", "--providers.file.filename=/fixtures/traefik.yaml"})
	docker("", "network", "connect", "--alias", "traefik-denied", denied, traefik)
	// Wait for DNS/backend readiness before exercising the shared Unix socket.
	for attempt := 0; attempt < 30; attempt++ {
		if strings.TrimSpace(docker("", "inspect", "--format", "{{.State.Running}}", relay)) != "true" {
			t.Fatalf("relay exited: %s", docker("", "logs", relay))
		}
		out, _ := exec.Command("docker", "exec", "-e", "REDISCLI_AUTH="+secret, redis, "redis-cli", "--user", "netdata", "-s", "/run/perry-metrics/16379.sock", "PING").CombinedOutput()
		if strings.TrimSpace(string(out)) == "PONG" {
			break
		}
		time.Sleep(time.Second)
	}
	if got := docker("", "exec", "-e", "REDISCLI_AUTH="+secret, redis, "redis-cli", "--user", "netdata", "-s", "/run/perry-metrics/16379.sock", "PING"); strings.TrimSpace(got) != "PONG" {
		t.Fatal("Unix relay did not carry authenticated Redis traffic")
	}
	for _, args := range [][]string{{"GET", "app:secret"}, {"SET", "app:secret", "bad"}, {"SCAN", "0"}, {"CONFIG", "GET", "*"}, {"SLOWLOG", "GET", "1"}, {"EVAL", "return 1", "0"}} {
		got := docker("", append([]string{"exec", "-e", "REDISCLI_AUTH=" + secret, redis, "redis-cli", "--user", "netdata"}, args...)...)
		if !strings.Contains(got, "NOPERM") {
			t.Fatalf("monitoring ACL permitted %s", args[0])
		}
	}
	// Only disposable Traefik receives a controlled 502; production is not faulted.
	docker("", "exec", redis, "sh", "-c", "wget -q -O /dev/null http://traefik:8080/fixture-error 2>/dev/null || true")
	metrics := docker("", "exec", redis, "wget", "-qO-", "http://traefik:8082/metrics")
	if !strings.Contains(metrics, `code="502"`) || !strings.Contains(metrics, `service="fixture-broken@file"`) || !strings.Contains(metrics, "traefik_service_request_duration_seconds_bucket") {
		t.Fatal("Traefik did not export service-labelled 5xx and latency histograms")
	}
	deniedOutput := docker("", "run", "--rm", "--network", denied, "--entrypoint", "sh", redisImage, "-c", "wget -S -O /dev/null http://traefik-denied:8082/metrics 2>&1 || true")
	if !strings.Contains(deniedOutput, "403 Forbidden") {
		t.Fatal("Traefik permitted metrics from an unrelated network")
	}
	for _, name := range []string{"postgres", "redis"} {
		config, err := render(read(name+".conf.tmpl"), secret)
		if err != nil {
			t.Fatal(err)
		}
		config = strings.ReplaceAll(config, "perry-infra_postgres:15432", "perry-infra_postgres:5432")
		config = strings.ReplaceAll(config, "127.0.0.1:16379", "redis:6379")
		config = strings.ReplaceAll(config, "/etc/netdata/private-metrics-ca.crt", "/fixtures/ca.crt")
		write(name+".conf", config)
	}
	for _, name := range []string{"traefik", "prometheus"} {
		write(name+".conf", strings.ReplaceAll(read(name+".conf"), "127.0.0.1:18082", "traefik:8082"))
	}
	for _, module := range []string{"postgres", "redis", "traefik", "prometheus"} {
		t.Log("checking actual Netdata collector", module)
		name := prefix + "-plugin-" + module
		containers = append(containers, name)
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		cmd := exec.CommandContext(ctx, "docker", "run", "--tty", "--rm", "--name", name, "--network", network, "--memory", "128m", "--pids-limit", "64", "--read-only", "--cap-drop", "ALL", "--mount", "type=volume,source="+volume+",target=/fixtures,readonly", "--mount", "type=volume,source="+volume+",target=/etc/netdata/go.d,readonly", "--entrypoint", "/usr/libexec/netdata/plugins.d/go.d.plugin", netdataImage, "-d", "-m", module)
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdin = reader
		output, _ := cmd.CombinedOutput()
		cancel()
		reader.Close()
		writer.Close()
		cycles := collectedContexts(string(output))
		required := map[string][]string{
			"postgres":   {"postgres.connections_usage", "postgres.db_transactions_rate", "postgres.db_locks_held_count", "postgres.db_locks_awaited_count", "postgres.db_deadlocks_rate"},
			"redis":      {"redis.memory", "redis.key_eviction_events", "redis.clients", "redis.keyspace_lookup_hit_rate"},
			"traefik":    {"traefik.entrypoint_requests", "traefik.entrypoint_request_duration_average"},
			"prometheus": {"prometheus.traefik_services.traefik_service_requests", "prometheus.traefik_services.traefik_service_request_duration_seconds"},
		}[module]
		var missing []string
		for _, required := range required {
			found := false
			for metric, count := range cycles {
				if count >= 2 && strings.HasPrefix(metric, required) {
					found = true
				}
			}
			if !found {
				missing = append(missing, required)
			}
		}
		if len(missing) != 0 {
			log, err := os.CreateTemp("", "perry-metrics-fixture-"+module+"-*.log")
			if err == nil {
				log.Write(output)
				log.Close()
				t.Log("fixture diagnostics:", log.Name())
			}
			var diagnostics []string
			for _, line := range strings.Split(string(output), "\n") {
				if strings.Contains(line, "level=error") || strings.Contains(line, "level=warn") || strings.Contains(line, "level=") && strings.Contains(line, module) {
					diagnostics = append(diagnostics, line)
				}
			}
			t.Fatalf("%s lacks repeated numeric metrics %v: %s", module, missing, strings.Join(diagnostics, "\n"))
		}
		exec.Command("docker", "rm", "-fv", name).Run()
	}
}
