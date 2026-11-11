package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const migrationState = "/var/lib/perry-redis-migration"
const oldRedis = "im-listening_redis"
const sharedRedis = "perry-infra_redis"
const apiService = "im-listening_api"
const redisImage = "redis:8.10.3-alpine@sha256:71885d8cc6e42f2bc0aa2eb853bfc4be200b5108a46ac50e117ae1df0531fb80"

// Only hashes and expiration times leave Redis, never session keys or contents.
const proofScript = `local out = {}; for _, k in ipairs(redis.call('KEYS', '*')) do
local d = redis.call('DUMP', k); if d then
table.insert(out, {redis.sha1hex(k), redis.sha1hex(d), redis.call('PEXPIRETIME', k)})
end end; return out`

type serviceSnapshot struct {
	ID      string
	Version struct{ Index uint64 }
	Spec    struct {
		Name         string
		Mode         struct{ Replicated *struct{ Replicas uint64 } }
		TaskTemplate struct {
			ContainerSpec struct {
				Image  string
				Mounts []struct{ Source, Target string }
			}
		}
	}
}

func command(input io.Reader, program string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock"}
	cmd.Stdin = input
	out, err := cmd.Output()
	if err != nil {
		// Docker diagnostics and service specs may contain private configuration.
		return nil, fmt.Errorf("%s operation failed; inspect privately on the VPS", filepath.Base(program))
	}
	return out, nil
}

func docker(args ...string) ([]byte, error) { return command(nil, "/usr/bin/docker", args...) }

func service(name string) (serviceSnapshot, []byte, error) {
	out, err := docker("service", "inspect", name)
	var values []serviceSnapshot
	if err != nil || json.Unmarshal(out, &values) != nil || len(values) != 1 || values[0].Spec.Name != name {
		return serviceSnapshot{}, nil, errors.New("cannot inspect expected service " + name)
	}
	return values[0], out, nil
}

func privateFile(name string, value []byte) error {
	return os.WriteFile(filepath.Join(migrationState, name), value, 0o600)
}

func rootOnly() error {
	if os.Geteuid() != 0 {
		return errors.New("use administrator sudo; this command cannot run through a release account")
	}
	syscall.Umask(0o077)
	return nil
}

func applicationLock() (*os.File, error) {
	return exclusiveLock("/opt/portfolio-release/im-listening.lock")
}

func exclusiveLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("a release is in progress; retry outside its deployment window")
	}
	return file, nil
}

func scale(name string, replicas int) error {
	return scaleWith(docker, time.Sleep, name, replicas)
}

func scaleWith(run func(...string) ([]byte, error), sleep func(time.Duration), name string, replicas int) error {
	// Docker's attached progress monitor can hang for a zero-replica service.
	// Submit the change, then independently check local containers on this node.
	_, err := run("service", "scale", "--detach=true", fmt.Sprintf("%s=%d", name, replicas))
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 180; attempt++ {
		out, err := run("ps", "-q", "--filter", "label=com.docker.swarm.service.name="+name)
		if err != nil {
			return err
		}
		ids := strings.Fields(string(out))
		ready := len(ids) == replicas
		for _, id := range ids {
			if !ready {
				break
			}
			status, err := run("inspect", "--format", "{{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", id)
			if err != nil {
				return err
			}
			health := strings.TrimSpace(string(status))
			ready = health == "true healthy" || health == "true none"
		}
		if ready {
			return nil
		}
		sleep(time.Second)
	}
	return errors.New("service containers did not reach the requested healthy replica count")
}

func container(name string) (string, error) {
	out, err := docker("ps", "-q", "--filter", "label=com.docker.swarm.service.name="+name)
	ids := strings.Fields(string(out))
	if err != nil || len(ids) != 1 {
		return "", errors.New("expected exactly one running container for " + name)
	}
	return ids[0], nil
}

type proofRow struct {
	Key, Value string
	Expires    int64
}

func parseProof(data []byte) ([]proofRow, error) {
	var rows [][]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, errors.New("invalid Redis preservation proof")
	}
	var result []proofRow
	for _, row := range rows {
		var p proofRow
		if len(row) != 3 || json.Unmarshal(row[0], &p.Key) != nil || json.Unmarshal(row[1], &p.Value) != nil || json.Unmarshal(row[2], &p.Expires) != nil {
			return nil, errors.New("invalid Redis proof row")
		}
		result = append(result, p)
	}
	return result, nil
}

func preserved(before, after []proofRow, now int64) error {
	lookup := map[string]proofRow{}
	for _, p := range after {
		lookup[p.Key] = p
	}
	for _, p := range before {
		if p.Expires >= 0 && p.Expires <= now {
			continue // Natural expiration during the maintenance window is valid.
		}
		if current, ok := lookup[p.Key]; !ok || current != p {
			return errors.New("an unexpired Redis key, value, or expiration changed; keep the API stopped and inspect the backup")
		}
	}
	return nil
}

func redisProof(old bool) ([]byte, error) {
	name, user := sharedRedis, "admin"
	var input io.Reader
	script := `IFS= read -r REDISCLI_AUTH; export REDISCLI_AUTH; exec redis-cli --user "$1" --json EVAL "$2" 0`
	if old {
		name, user = oldRedis, "default"
		script = `read -r _ REDISCLI_AUTH < /run/secrets/redis_config; export REDISCLI_AUTH; exec redis-cli --user "$1" --json EVAL "$2" 0`
	} else {
		data, err := os.ReadFile(filepath.Join(migrationState, "secrets/redis_admin_url_v1"))
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(string(data))
		if err != nil || u.User == nil {
			return nil, errors.New("invalid private administrator URL")
		}
		password, _ := u.User.Password()
		input = strings.NewReader(password + "\n")
	}
	id, err := container(name)
	if err != nil {
		return nil, err
	}
	return command(input, "/usr/bin/docker", "exec", "-i", id, "sh", "-c", script, "--", user, proofScript)
}

func migrate(root, broker string) error {
	if err := rootOnly(); err != nil {
		return err
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(broker) {
		return errors.New("checkout and broker paths must be absolute")
	}
	lock, err := applicationLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	infraLock, err := exclusiveLock("/run/lock/perry-infra-release.lock")
	if err != nil {
		return err
	}
	defer infraLock.Close()
	if _, err := command(nil, broker, "fingerprint", root); err != nil {
		return err
	}
	api, apiJSON, err := service(apiService)
	if err != nil {
		return err
	}
	old, oldJSON, err := service(oldRedis)
	if err != nil {
		return err
	}
	if api.Spec.Mode.Replicated == nil || old.Spec.Mode.Replicated == nil || api.Spec.Mode.Replicated.Replicas != 1 || old.Spec.Mode.Replicated.Replicas != 1 || old.Spec.TaskTemplate.ContainerSpec.Image != redisImage {
		return errors.New("expected one API and Redis replica at the reviewed Redis digest")
	}
	if len(old.Spec.TaskTemplate.ContainerSpec.Mounts) != 1 || old.Spec.TaskTemplate.ContainerSpec.Mounts[0].Source != "im-listening_redis-data" || old.Spec.TaskTemplate.ContainerSpec.Mounts[0].Target != "/data" {
		return errors.New("existing Redis data mount differs from the reviewed volume")
	}
	if _, err := docker("service", "inspect", sharedRedis); err == nil {
		return errors.New("shared Redis already exists; inspect rather than retrying migration")
	}
	nodes, err := docker("node", "ls", "-q")
	if err != nil || len(strings.Fields(string(nodes))) != 1 {
		return errors.New("migration supports only the existing single-node Swarm")
	}
	if err := os.Mkdir(migrationState, 0o700); err != nil {
		return errors.New("migration state already exists or cannot be created; inspect before retrying")
	}
	for name, data := range map[string][]byte{"api-service.json": apiJSON, "redis-service.json": oldJSON} {
		if err := privateFile(name, data); err != nil {
			return err
		}
	}
	// Save every path touched by the existing installer, plus the app policy.
	_, err = command(nil, "/bin/tar", "-C", "/", "-czf", migrationState+"/installation.tar.gz",
		"opt/perry-infra-release", "usr/local/libexec/perry-infra-release",
		"etc/sudoers.d/perry-infra-release", "etc/ssh/sshd_config.d/11-perry-infra-release.conf",
		"var/lib/infra-release/.ssh/authorized_keys", "opt/portfolio-release/im-listening.sha256")
	if err != nil {
		return err
	}
	// The existing installer and broker acquire this lock themselves below.
	if err := syscall.Flock(int(infraLock.Fd()), syscall.LOCK_UN); err != nil {
		return err
	}
	if err := credentials(filepath.Join(migrationState, "secrets")); err != nil {
		return err
	}
	files, err := os.ReadDir(filepath.Join(migrationState, "secrets"))
	if err != nil {
		return err
	}
	for _, file := range files {
		if _, err := docker("secret", "inspect", file.Name()); err == nil {
			return errors.New("a proposed Redis secret name already exists; refusing replacement")
		}
		data, err := os.ReadFile(filepath.Join(migrationState, "secrets", file.Name()))
		if err != nil {
			return err
		}
		if _, err := command(bytes.NewReader(data), "/usr/bin/docker", "secret", "create", file.Name(), "-"); err != nil {
			return err
		}
	}
	if _, err := docker("network", "create", "--driver", "overlay", "--internal", "--opt", "encrypted", "redis_clients"); err != nil {
		return err
	}
	if _, err := docker("pull", redisImage); err != nil {
		return err
	}
	return cutover(root, broker, lock)
}

// Resume is deliberately limited to an interrupted stop before any data backup
// or infrastructure install. Preserve the original backups and credentials.
func resume(root, broker string) error {
	if err := rootOnly(); err != nil {
		return err
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(broker) {
		return errors.New("checkout and broker paths must be absolute")
	}
	lock, err := applicationLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	if _, err := command(nil, broker, "fingerprint", root); err != nil {
		return err
	}
	if _, err := docker("service", "inspect", sharedRedis); err == nil {
		return errors.New("shared service already exists; this resume phase is not applicable")
	}
	for _, name := range []string{"redis-data.tar.gz", "status"} {
		if _, err := os.Stat(filepath.Join(migrationState, name)); !os.IsNotExist(err) {
			return errors.New("migration progressed beyond the supported resume point")
		}
	}
	for _, pair := range [][2]string{{apiService, "api-service.json"}, {oldRedis, "redis-service.json"}} {
		saved, err := os.ReadFile(filepath.Join(migrationState, pair[1]))
		if err != nil {
			return err
		}
		_, current, err := service(pair[0])
		if err != nil {
			return err
		}
		if !samePausedService(saved, current) {
			return errors.New("service spec changed since migration started; refusing resume")
		}
	}
	for _, name := range []string{"installation.tar.gz", "secrets/redis_users_v1", "secrets/redis_admin_url_v1", "secrets/im_listening_redis_url_v2"} {
		if _, err := os.Stat(filepath.Join(migrationState, name)); err != nil {
			return err
		}
	}
	// If the first stop captured a proof before interruption, retain it for
	// audit. The resumed stop must capture the current state afresh after the
	// temporary restoration of application availability.
	proof := filepath.Join(migrationState, "before.json")
	if _, err := os.Stat(proof); err == nil {
		if err := os.Rename(proof, filepath.Join(migrationState, fmt.Sprintf("before-interrupted-%d.json", time.Now().UnixNano()))); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	fmt.Println("Resuming the saved migration without regenerating credentials or replacing backups.")
	return cutover(root, broker, lock)
}

func samePausedService(saved, current []byte) bool {
	decode := func(data []byte) (string, []byte, bool) {
		var rows []struct {
			ID   string
			Spec map[string]any
		}
		if json.Unmarshal(data, &rows) != nil || len(rows) != 1 {
			return "", nil, false
		}
		mode, ok := rows[0].Spec["Mode"].(map[string]any)
		if !ok {
			return "", nil, false
		}
		replicated, ok := mode["Replicated"].(map[string]any)
		if !ok {
			return "", nil, false
		}
		count, ok := replicated["Replicas"].(float64)
		if !ok || (count != 0 && count != 1) {
			return "", nil, false
		}
		replicated["Replicas"] = float64(1)
		// A failed start may exhaust Swarm's restart budget during recovery.
		// A forced restart changes this counter but no runtime permissions.
		if task, ok := rows[0].Spec["TaskTemplate"].(map[string]any); ok {
			delete(task, "ForceUpdate")
		}
		value, err := json.Marshal(rows[0].Spec)
		return rows[0].ID, value, err == nil
	}
	sid, s, ok := decode(saved)
	if !ok {
		return false
	}
	cid, c, ok := decode(current)
	return ok && sid != "" && sid == cid && bytes.Equal(s, c)
}

func cutover(root, broker string, lock *os.File) error {
	fmt.Println("Stopping Im Listening for the Redis maintenance window.")
	if err := scale(apiService, 0); err != nil {
		return err
	}
	before, err := redisProof(true)
	if err != nil {
		return err
	}
	if _, err := parseProof(before); err != nil {
		return err
	}
	if err := privateFile("before.json", before); err != nil {
		return err
	}
	if err := scale(oldRedis, 0); err != nil {
		return err
	}
	backup, err := os.OpenFile(migrationState+"/redis-data.tar.gz", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/docker", "run", "--rm", "--network", "none", "--user", "999:1000", "--read-only", "--cap-drop", "ALL",
		"--mount", "type=volume,source=im-listening_redis-data,target=/data,readonly", redisImage, "tar", "czf", "-", "-C", "/data", ".")
	cmd.Stdout = backup
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock"}
	err = cmd.Run()
	closeErr := backup.Close()
	if err != nil || closeErr != nil {
		return errors.New("stopped-volume backup failed; do not start the shared service")
	}
	if _, err := command(nil, "/bin/tar", "-tzf", migrationState+"/redis-data.tar.gz"); err != nil {
		return err
	}
	authorized, err := os.ReadFile("/var/lib/infra-release/.ssh/authorized_keys")
	if err != nil {
		return err
	}
	key := regexp.MustCompile(`ssh-ed25519 [A-Za-z0-9+/=]+(?: [^\r\n]*)?`).Find(authorized)
	if key == nil {
		return errors.New("cannot reuse the installed infrastructure release public key")
	}
	if err := privateFile("release.pub", append(key, '\n')); err != nil {
		return err
	}
	if _, err := command(nil, "/bin/sh", root+"/deploy/install.sh", broker, migrationState+"/release.pub"); err != nil {
		return err
	}
	digest, err := command(nil, "/usr/local/libexec/perry-infra-release", "fingerprint", "/opt/perry-infra-release")
	if err != nil {
		return err
	}
	if _, err := command(nil, "/usr/local/libexec/perry-infra-release", strings.TrimSpace(string(digest))); err != nil {
		return err
	}
	after, err := redisProof(false)
	if err != nil {
		return err
	}
	b, err := parseProof(before)
	if err != nil {
		return err
	}
	a, err := parseProof(after)
	if err != nil {
		return err
	}
	if err := preserved(b, a, time.Now().UnixMilli()); err != nil {
		return err
	}
	fmt.Println("Verified preservation of all unexpired key contents and absolute expiration times.")
	if _, err := docker("service", "update", "--detach=false", "--network-rm", "im-listening_sessions", "--network-add", "redis_clients",
		"--secret-rm", "im_listening_redis_url_v1", "--secret-add", "source=im_listening_redis_url_v2,target=redis_url,uid=1000,mode=0400", "--replicas", "1", apiService); err != nil {
		return err
	}
	if err := verifyProduction(root); err != nil {
		return err
	}
	// The old policy rejects the changed spec until record acquires this same lock.
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		return err
	}
	if _, err := command(nil, "/usr/local/libexec/portfolio-release", "record", "im-listening"); err != nil {
		return err
	}
	if err := privateFile("status", []byte("awaiting-browser-verification\n")); err != nil {
		return err
	}
	fmt.Println("Cutover passed. Keep the old Redis service at zero replicas until browser authentication/session checks pass; rollback remains available.")
	return nil
}

func verifyProduction(root string) error {
	if err := rootOnly(); err != nil {
		return err
	}
	api, _, err := service(apiService)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("redis-verify-%d", time.Now().UnixNano())
	if _, err := command(nil, "/usr/bin/docker", "config", "create", name, root+"/redis/verify.mjs"); err != nil {
		return err
	}
	defer docker("config", "rm", name)
	args := []string{"service", "create", "--detach=true", "--name", name, "--mode", "replicated-job", "--restart-condition", "none", "--network", "redis_clients",
		"--read-only", "--cap-drop", "ALL", "--limit-memory", "128M", "--limit-cpu", "0.25",
		"--config", "source=" + name + ",target=/app/shared-redis-verify.mjs"}
	for _, secret := range []string{"redis_admin_url_v1", "redis_probe_url_v1", "im_listening_redis_url_v2"} {
		args = append(args, "--secret", "source="+secret+",target="+secret+",uid=1000,mode=0400")
	}
	args = append(args, "--entrypoint", "node", api.Spec.TaskTemplate.ContainerSpec.Image, "/app/shared-redis-verify.mjs")
	if _, err := docker(args...); err != nil {
		return err
	}
	defer docker("service", "rm", name)
	for attempt := 0; attempt < 120; attempt++ {
		tasks, err := docker("service", "ps", "-q", "--no-trunc", name)
		ids := strings.Fields(string(tasks))
		if err != nil {
			return err
		}
		if len(ids) == 1 {
			state, err := docker("inspect", "--type", "task", "--format", "{{.Status.State}}", ids[0])
			if err != nil {
				return err
			}
			switch strings.TrimSpace(string(state)) {
			case "complete":
				logs, err := docker("service", "logs", "--raw", name)
				if err != nil {
					return err
				}
				fmt.Print(string(logs)) // Verifier deliberately logs no credentials.
				client := &http.Client{Timeout: 10 * time.Second}
				response, err := client.Get("https://im-listening-api.perrysmithmoss.com/healthz")
				if err != nil {
					return errors.New("public API health check failed")
				}
				response.Body.Close()
				if response.StatusCode != http.StatusOK {
					return errors.New("public API health check was not healthy")
				}
				return nil
			case "failed", "rejected":
				return errors.New("private ACL/application verification job failed")
			}
		}
		time.Sleep(time.Second)
	}
	return errors.New("private verification job timed out")
}

// Restore the saved Docker service spec via the administrator's local socket.
// This also recreates the old service if it was removed after verification.
func restoreService(name, file string) error {
	data, err := os.ReadFile(filepath.Join(migrationState, file))
	if err != nil {
		return err
	}
	var saved []struct{ Spec json.RawMessage }
	if json.Unmarshal(data, &saved) != nil || len(saved) != 1 {
		return errors.New("invalid saved service specification")
	}
	endpoint := "/v1.41/services/create"
	if current, _, err := service(name); err == nil {
		endpoint = fmt.Sprintf("/v1.41/services/%s/update?version=%d", current.ID, current.Version.Index)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	response, err := client.Post("http://docker"+endpoint, "application/json", bytes.NewReader(saved[0].Spec))
	if err != nil {
		return errors.New("cannot restore the saved service specification")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Docker rejected the saved service specification")
	}
	return nil
}

func rollback() error {
	if err := rootOnly(); err != nil {
		return err
	}
	lock, err := applicationLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	infraLock, err := exclusiveLock("/run/lock/perry-infra-release.lock")
	if err != nil {
		return err
	}
	defer infraLock.Close()
	// Check recovery inputs before stopping anything.
	for _, name := range []string{"installation.tar.gz", "api-service.json", "redis-service.json"} {
		if _, err := os.Stat(filepath.Join(migrationState, name)); err != nil {
			return err
		}
	}
	if err := scale(apiService, 0); err != nil {
		return err
	}
	if _, _, err := service(sharedRedis); err == nil {
		if err := scale(sharedRedis, 0); err != nil {
			return err
		}
	}
	// Retain all writes made since cutover by reusing the current data volume.
	if err := restoreService(oldRedis, "redis-service.json"); err != nil {
		return err
	}
	if err := scale(oldRedis, 1); err != nil {
		return err
	}
	if err := restoreService(apiService, "api-service.json"); err != nil {
		return err
	}
	if err := scale(apiService, 1); err != nil {
		return err
	}
	// The archive is private administrator-created input, never accepted from CI.
	if _, err := command(nil, "/bin/tar", "-C", "/", "-xzf", migrationState+"/installation.tar.gz"); err != nil {
		return err
	}
	for _, check := range [][]string{{"/usr/sbin/visudo", "-c"}, {"/usr/sbin/sshd", "-t"}, {"/usr/bin/systemctl", "reload", "ssh"}} {
		if _, err := command(nil, check[0], check[1:]...); err != nil {
			return err
		}
	}
	return privateFile("status", []byte("rolled-back\n"))
}
