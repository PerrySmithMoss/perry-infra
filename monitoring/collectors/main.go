// Administrator-only installation. This is never a release-broker command.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const stateDir = "/var/lib/perry-private-metrics"

// Only these files are installed, backed up and eligible for rollback removal.
var managedFiles = map[string]string{
	"monitoring/collectors/perry-metrics@.socket":  "/etc/systemd/system/perry-metrics@.socket",
	"monitoring/collectors/perry-metrics@.service": "/etc/systemd/system/perry-metrics@.service",
	"monitoring/collectors/tmpfiles.conf":          "/etc/tmpfiles.d/perry-metrics.conf",
	"monitoring/collectors/traefik.conf":           "/etc/netdata/go.d/traefik.conf",
	"monitoring/collectors/prometheus.conf":        "/etc/netdata/go.d/prometheus.conf",
	"monitoring/collectors/health.conf":            "/etc/netdata/health.d/perry-private-metrics.conf",
	"postgres/tls/ca.crt":                          "/etc/netdata/private-metrics-ca.crt",
}

var generatedFiles = []string{"/etc/netdata/go.d/postgres.conf", "/etc/netdata/go.d/redis.conf"}
var installationPaths = []string{
	"/opt/perry-infra-release", "/usr/local/libexec/perry-infra-release",
	"/etc/sudoers.d/perry-infra-release", "/etc/ssh/sshd_config.d/11-perry-infra-release.conf",
	"/var/lib/infra-release/.ssh/authorized_keys", "/etc/hosts",
}

func password() (string, error) {
	var value [32]byte
	_, err := rand.Read(value[:])
	return hex.EncodeToString(value[:]), err
}

// Preserve every existing application/admin rule byte for byte.
func metricsACL(current, secret string) (string, error) {
	if current == "" {
		return "", errors.New("refusing an empty existing ACL")
	}
	for _, line := range strings.Split(current, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "user" && fields[1] == "netdata" {
			return "", errors.New("Redis netdata user already exists; review it before installation")
		}
	}
	return current + "\n" + fmt.Sprintf("user netdata reset on #%x resetkeys resetchannels -@all +ping +info +hello +client|setinfo\n", sha256.Sum256([]byte(secret))), nil
}

func hostsWithAlias(current string) (string, error) {
	for _, line := range strings.Split(current, "\n") {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		for _, field := range fields[min(1, len(fields)):] {
			if field == "perry-infra_postgres" {
				return "", errors.New("existing perry-infra_postgres host alias requires administrator review")
			}
		}
	}
	return current + "\n127.0.0.1 perry-infra_postgres # perry-private-metrics\n", nil
}

func render(template, secret string) (string, error) {
	if len(secret) != 64 || strings.Count(template, "{{PASSWORD}}") != 1 {
		return "", errors.New("invalid credential or collector template")
	}
	if _, err := hex.DecodeString(secret); err != nil {
		return "", errors.New("credential must be generated hexadecimal data")
	}
	return strings.ReplaceAll(template, "{{PASSWORD}}", secret), nil
}

// Child output can contain credentials on failure. Keep it private; callers
// print only phase names, never the captured output or command arguments.
func command(input []byte, timeout time.Duration, name string, args ...string) ([]byte, error) {
	return captureCommand(input, false, timeout, name, args...)
}

func captureCommand(input []byte, keepInputOpen bool, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(input)
	if keepInputOpen {
		reader, writer, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		defer writer.Close()
		// Go collectors terminate on EOF from their agent function channel.
		cmd.Stdin = reader
		// A terminal child has its own session. Stop it with terminal Ctrl-C
		// before the outer deadline rather than leaving it to a parent's HUP.
		interrupt := time.AfterFunc(max(time.Millisecond, timeout-3*time.Second), func() { writer.Write([]byte{3}) })
		defer interrupt.Stop()
	}
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	return cmd.CombinedOutput()
}

func checked(input []byte, name string, args ...string) ([]byte, error) {
	output, err := command(input, 15*time.Minute, name, args...)
	if log, openErr := os.OpenFile(filepath.Join(stateDir, "operations.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); openErr == nil {
		log.Write(output)
		log.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("%s failed; inspect the root-only operations.log", filepath.Base(name))
	}
	return output, nil
}

func task(service string) (string, error) {
	output, err := checked(nil, "docker", "ps", "--filter", "label=com.docker.swarm.service.name="+service, "--format", "{{.ID}}")
	ids := strings.Fields(string(output))
	if err != nil || len(ids) != 1 {
		return "", fmt.Errorf("expected exactly one running task for %s", service)
	}
	return ids[0], nil
}

func psql(container string, sql []byte) ([]byte, error) {
	return checked(sql, "docker", "exec", "-i", container, "sh", "-c", `exec psql -X -qAt -v ON_ERROR_STOP=1 -U "$(cat /run/secrets/postgres_user)" -d postgres`)
}

func privateWrite(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func backup() error {
	paths := append([]string{}, installationPaths...)
	for _, path := range managedFiles {
		paths = append(paths, path)
	}
	paths = append(paths, generatedFiles...)
	var present, absent []string
	for _, path := range paths {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			absent = append(absent, path)
		} else if err != nil {
			return err
		} else {
			present = append(present, strings.TrimPrefix(path, "/"))
		}
	}
	data, _ := json.Marshal(absent)
	if err := privateWrite(filepath.Join(stateDir, "absent.json"), data); err != nil {
		return err
	}
	_, err := checked(nil, "tar", append([]string{"-czpf", filepath.Join(stateDir, "before.tar.gz"), "-C", "/", "--"}, present...)...)
	return err
}

func installFile(source, target string, gid int, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	directory := filepath.Dir(target)
	if _, err := os.Stat(directory); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
		if err := os.Chmod(directory, 0o755); err != nil {
			return err
		}
	}
	if err := privateWrite(target, data); err != nil {
		return err
	}
	if err := os.Chown(target, 0, gid); err != nil {
		return err
	}
	return os.Chmod(target, mode)
}

func install(root, broker string) error {
	group, err := user.LookupGroup("netdata")
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	if _, err := os.Stat("/lib/systemd/systemd-socket-proxyd"); err != nil {
		return err
	}
	// Refuse to overwrite another collector configuration, including symlinks.
	for _, target := range append(append([]string{}, generatedFiles...), mapValues(managedFiles)...) {
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("review existing path before installation: %s", target)
		}
	}
	for _, port := range []string{"15432", "16379", "18082"} {
		listener, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			return fmt.Errorf("loopback port %s is already in use", port)
		}
		listener.Close()
	}
	hosts, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return err
	}
	newHosts, err := hostsWithAlias(string(hosts))
	if err != nil {
		return err
	}
	if _, err := checked(nil, "sh", filepath.Join(root, "monitoring/verify.sh"), "--local-only"); err != nil {
		return err
	}
	if _, err := os.Lstat("/run/perry-metrics"); !errors.Is(err, os.ErrNotExist) {
		return errors.New("review existing /run/perry-metrics before installation")
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		return errors.New("private state already exists or cannot be created; inspect/rollback instead of regenerating credentials")
	}
	fmt.Println("Recovery state:", stateDir)
	if err := backup(); err != nil {
		return err
	}
	if err := privateWrite(filepath.Join(stateDir, "backup-ready"), nil); err != nil {
		return err
	}
	nodes, err := checked(nil, "docker", "node", "ls", "-q")
	if err != nil || len(strings.Fields(string(nodes))) != 1 {
		return errors.New("installation requires the existing single-node Swarm")
	}
	postgres, err := task("perry-infra_postgres")
	if err != nil {
		return err
	}
	existing, err := psql(postgres, []byte("SELECT count(*) FROM pg_roles WHERE rolname = 'netdata_monitor';\n"))
	if err != nil || strings.TrimSpace(string(existing)) != "0" {
		return errors.New("review the existing PostgreSQL monitoring role before installation")
	}
	redis, err := task("perry-infra_redis")
	if err != nil {
		return err
	}
	// This read uses administrator privileges, never the release account.
	acl, err := checked(nil, "docker", "exec", redis, "cat", "/run/secrets/redis_users")
	if err != nil {
		return err
	}
	pgPassword, err := password()
	if err != nil {
		return err
	}
	redisPassword, err := password()
	if err != nil {
		return err
	}
	newACL, err := metricsACL(string(acl), redisPassword)
	if err != nil {
		return err
	}
	for name, value := range map[string]string{"postgres.password": pgPassword, "redis.password": redisPassword, "redis-users.acl": newACL, "redis.url": "redis://netdata:" + redisPassword + "@perry-infra_redis:6379/0"} {
		if err := privateWrite(filepath.Join(stateDir, name), []byte(value)); err != nil {
			return err
		}
	}
	for _, name := range []string{"postgres", "redis"} {
		template, err := os.ReadFile(filepath.Join(root, "monitoring/collectors", name+".conf.tmpl"))
		if err != nil {
			return err
		}
		secret := pgPassword
		if name == "redis" {
			secret = redisPassword
		}
		config, err := render(string(template), secret)
		if err != nil {
			return err
		}
		if err := privateWrite(filepath.Join(stateDir, name+".conf"), []byte(config)); err != nil {
			return err
		}
	}
	fmt.Println("Creating dedicated database identities; application credentials are preserved.")
	// Write intent before SQL so interruption after COMMIT is recoverable too.
	if err := privateWrite(filepath.Join(stateDir, "role-created"), nil); err != nil {
		return err
	}
	if _, err := psql(postgres, []byte(monitorSQL(pgPassword))); err != nil {
		return err
	}
	if _, err := checked([]byte(newACL), "docker", "secret", "create", "redis_users_metrics_v2", "-"); err != nil {
		return err
	}
	for source, target := range managedFiles {
		if err := installFile(filepath.Join(root, source), target, 0, 0o644); err != nil {
			return err
		}
	}
	for _, name := range []string{"postgres", "redis"} {
		if err := installFile(filepath.Join(stateDir, name+".conf"), "/etc/netdata/go.d/"+name+".conf", gid, 0o640); err != nil {
			return err
		}
	}
	if err := os.WriteFile("/etc/hosts", []byte(newHosts), 0o644); err != nil {
		return err
	}
	if _, err := checked(nil, "systemd-tmpfiles", "--create", "/etc/tmpfiles.d/perry-metrics.conf"); err != nil {
		return err
	}
	key, err := os.ReadFile("/var/lib/infra-release/.ssh/authorized_keys")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(key))
	var publicKey string
	for i, field := range fields {
		if field == "ssh-ed25519" && i+1 < len(fields) {
			publicKey = field + " " + fields[i+1] + "\n"
			break
		}
	}
	if publicKey == "" {
		return errors.New("existing dedicated infra-release Ed25519 key not found")
	}
	keyPath := filepath.Join(stateDir, "release.pub")
	if err := privateWrite(keyPath, []byte(publicKey)); err != nil {
		return err
	}
	fmt.Println("Installing administrator-reviewed infrastructure snapshot; brief database/cache/proxy restarts are expected.")
	if _, err := checked(nil, "sh", filepath.Join(root, "deploy/install.sh"), broker, keyPath); err != nil {
		return fmt.Errorf("administrator snapshot installation: %w", err)
	}
	fingerprint, err := checked(nil, broker, "fingerprint", root)
	if err != nil {
		return err
	}
	if _, err := checked(nil, "/usr/local/libexec/perry-infra-release", strings.TrimSpace(string(fingerprint))); err != nil {
		return err
	}
	if _, err := checked(nil, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if _, err := checked(nil, "systemctl", "enable", "--now", "perry-metrics@15432.socket", "perry-metrics@16379.socket", "perry-metrics@18082.socket"); err != nil {
		return err
	}
	if _, err := checked(nil, "systemctl", "restart", "netdata"); err != nil {
		return err
	}
	return verify(root)
}

func monitorSQL(secret string) string {
	return fmt.Sprintf(`SET log_statement = 'none';
SET log_min_duration_statement = -1;
SET log_min_error_statement = 'panic';
BEGIN;
SET LOCAL lock_timeout = '3s';
CREATE ROLE netdata_monitor LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 3 PASSWORD '%s';
GRANT pg_monitor TO netdata_monitor;
GRANT CONNECT ON DATABASE postgres TO netdata_monitor;
ALTER ROLE netdata_monitor SET default_transaction_read_only = on;
ALTER ROLE netdata_monitor SET statement_timeout = '3s';
ALTER ROLE netdata_monitor SET idle_in_transaction_session_timeout = '30s';
COMMIT;
`, secret)
}

func mapValues(values map[string]string) []string {
	var result []string
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func rollback() error {
	if _, err := os.Stat(filepath.Join(stateDir, "backup-ready")); err != nil {
		return errors.New("no complete installation backup is available")
	}
	// Refuse concurrent infrastructure releases before changing running services.
	lock, err := os.OpenFile("/run/lock/perry-infra-release.lock", os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("an infrastructure release is running")
	}
	if _, err := checked(nil, "systemctl", "stop", "netdata"); err != nil {
		return err
	}
	for _, port := range []string{"15432", "16379", "18082"} {
		// A partial install may not yet have installed/started these units.
		command(nil, time.Minute, "systemctl", "disable", "--now", "perry-metrics@"+port+".socket")
		command(nil, time.Minute, "systemctl", "stop", "perry-metrics@"+port+".service")
	}
	if _, err := command(nil, time.Minute, "docker", "service", "inspect", "perry-infra_metrics-relay"); err == nil {
		if _, err := checked(nil, "docker", "service", "rm", "perry-infra_metrics-relay"); err != nil {
			return err
		}
	}
	if _, err := checked(nil, "tar", "-xzpf", filepath.Join(stateDir, "before.tar.gz"), "-C", "/"); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "absent.json"))
	if err != nil {
		return err
	}
	var absent []string
	if err := json.Unmarshal(data, &absent); err != nil {
		return err
	}
	allowed := append(append([]string{}, generatedFiles...), mapValues(managedFiles)...)
	for _, path := range absent {
		for _, candidate := range allowed {
			if path == candidate {
				if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	for _, check := range []string{"visudo", "sshd"} {
		flag := "-c"
		if check == "sshd" {
			flag = "-t"
		}
		if _, err := checked(nil, check, flag); err != nil {
			return err
		}
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		return err
	}
	fingerprint, err := checked(nil, "/usr/local/libexec/perry-infra-release", "fingerprint", "/opt/perry-infra-release")
	if err != nil {
		return err
	}
	if _, err := checked(nil, "/usr/local/libexec/perry-infra-release", strings.TrimSpace(string(fingerprint))); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(stateDir, "role-created")); err == nil {
		container, err := task("perry-infra_postgres")
		if err != nil {
			return err
		}
		if _, err := psql(container, []byte("SELECT 'ALTER ROLE netdata_monitor NOLOGIN; REVOKE pg_monitor FROM netdata_monitor;' WHERE EXISTS (SELECT FROM pg_roles WHERE rolname='netdata_monitor')\n\\gexec\n")); err != nil {
			return err
		}
	}
	for _, args := range [][]string{{"daemon-reload"}, {"reload", "ssh"}, {"start", "netdata"}} {
		if _, err := checked(nil, "systemctl", args...); err != nil {
			return err
		}
	}
	fmt.Println("Restored prior snapshot and host files. Retained credentials, secrets, data and backup for review.")
	return nil
}

func redisURLPassword(data []byte) (string, error) {
	u, err := url.Parse(strings.TrimSpace(string(data)))
	if err != nil || u.Scheme != "redis" || u.Host != "perry-infra_redis:6379" || u.User == nil || u.User.Username() != "netdata" || u.Path != "/0" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("expected the private shared Redis monitoring URL")
	}
	secret, _ := u.User.Password()
	if len(secret) != 64 {
		return "", errors.New("expected a generated 256-bit monitoring password")
	}
	if _, err := hex.DecodeString(secret); err != nil {
		return "", errors.New("expected a generated hexadecimal monitoring password")
	}
	return secret, nil
}

func configureRedis(root, urlPath string) error {
	data, err := os.ReadFile(urlPath)
	if err != nil {
		return err
	}
	secret, err := redisURLPassword(data)
	if err != nil {
		return err
	}
	container, err := task("perry-infra_redis")
	if err != nil {
		return err
	}
	acl, err := checked(nil, "docker", "exec", container, "cat", "/run/secrets/redis_users")
	if err != nil {
		return err
	}
	valid := false
	for _, line := range strings.Split(string(acl), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && fields[0] == "user" && fields[1] == "netdata" {
			for _, field := range fields[2:] {
				valid = valid || field == fmt.Sprintf("#%x", sha256.Sum256([]byte(secret)))
			}
		}
	}
	if !valid {
		return errors.New("deploy the versioned Redis ACL containing the new monitoring hash first")
	}
	template, err := os.ReadFile(filepath.Join(root, "monitoring/collectors/redis.conf.tmpl"))
	if err != nil {
		return err
	}
	config, err := render(string(template), secret)
	if err != nil {
		return err
	}
	old, err := os.ReadFile("/etc/netdata/go.d/redis.conf")
	if err != nil {
		return err
	}
	if err := privateWrite(filepath.Join(stateDir, "redis.conf.before-"+time.Now().Format("20060102T150405.000000000")), old); err != nil {
		return err
	}
	group, err := user.LookupGroup("netdata")
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp("/etc/netdata/go.d", ".redis.conf.*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	if _, err := staged.WriteString(config); err != nil {
		return err
	}
	if err := staged.Chown(0, gid); err != nil {
		return err
	}
	if err := staged.Chmod(0o640); err != nil {
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Rename(staged.Name(), "/etc/netdata/go.d/redis.conf"); err != nil {
		return err
	}
	for name, value := range map[string][]byte{"redis.password": []byte(secret), "redis.url": data, "redis-users.acl": acl} {
		if err := os.WriteFile(filepath.Join(stateDir, name), value, 0o600); err != nil {
			return err
		}
	}
	if _, err := checked(nil, "systemctl", "restart", "netdata"); err != nil {
		return err
	}
	return verifyRedisACL()
}

func run(args []string) error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("run this administrator utility as root on the Debian VPS")
	}
	syscall.Umask(0o077)
	if err := os.MkdirAll("/run/perry-netdata", 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile("/run/perry-netdata/install.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another monitoring operation is running")
	}
	switch {
	case len(args) == 3 && args[0] == "install":
		root, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		broker, err := filepath.Abs(args[2])
		if err != nil {
			return err
		}
		return install(root, broker)
	case len(args) == 2 && args[0] == "verify":
		return verify(args[1])
	case len(args) == 2 && args[0] == "diagnose":
		root, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		return diagnose(root)
	case len(args) == 1 && args[0] == "rollback":
		return rollback()
	case len(args) == 3 && args[0] == "configure-redis":
		return configureRedis(args[1], args[2])
	default:
		return errors.New("usage: perry-metrics-admin install CHECKOUT BROKER_BINARY | verify|diagnose CHECKOUT | configure-redis CHECKOUT PRIVATE_URL_FILE | rollback")
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "Do not rerun installation over partial state. Inspect the private log and use rollback if needed.")
		os.Exit(1)
	}
}
