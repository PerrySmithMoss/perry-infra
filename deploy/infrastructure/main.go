package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	installed = "/usr/local/libexec/perry-infra-release"
	snapshot  = "/opt/perry-infra-release"
	lockPath  = "/run/lock/perry-infra-release.lock"
)

var (
	digestRE      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	emailRE       = regexp.MustCompile(`^[A-Za-z0-9_.+@-]+$`)
	snapshotFiles = []string{
		"docker-compose.stack.yaml", "postgres/pg_hba.conf", "docker-proxy/haproxy.cfg", "redis/redis.conf",
		"monitoring/collectors/haproxy.cfg", "monitoring/collectors/traefik.yaml",
		"monitoring/collectors/postgres.conf.tmpl", "monitoring/collectors/redis.conf.tmpl",
		"monitoring/collectors/traefik.conf", "monitoring/collectors/prometheus.conf",
		"monitoring/collectors/health.conf", "monitoring/collectors/perry-metrics@.socket",
		"monitoring/collectors/perry-metrics@.service", "monitoring/collectors/tmpfiles.conf",
		"postgres/tls/ca.crt",
	}
)

func fingerprint(root string) (string, error) {
	hash := sha256.New()
	for _, name := range snapshotFiles {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return "", err
		}
		// Preserve the original wire format: name, NUL, raw file bytes, NUL.
		hash.Write([]byte(name + "\x00"))
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func sshArgs(original string) ([]string, error) {
	digest, ok := strings.CutPrefix(original, "infrastructure ")
	if !ok || !digestRE.MatchString(digest) {
		return nil, errors.New("only the reviewed infrastructure deployment is allowed")
	}
	return []string{"sudo", "-n", "--", installed, digest}, nil
}

func acmeEnv(input io.Reader) (string, error) {
	var env []string
	if err := json.NewDecoder(input).Decode(&env); err != nil {
		return "", errors.New("cannot read Traefik service environment")
	}
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "ACME_EMAIL="); ok {
			if emailRE.MatchString(value) {
				return entry, nil
			}
			break
		}
	}
	return "", errors.New("Traefik service has no valid ACME_EMAIL")
}

func deploy(expected string) error {
	if os.Geteuid() != 0 {
		return errors.New("infrastructure deployment requires root")
	}
	syscall.Umask(0o077)
	return deploySnapshot(snapshot, lockPath, expected, 15*time.Minute)
}

func deploySnapshot(root, lockName, expected string, timeout time.Duration) error {
	return deploySnapshotMode(root, lockName, expected, timeout, false)
}

func deploySnapshotMode(root, lockName, expected string, timeout time.Duration, maintenance bool) error {
	lock, err := os.OpenFile(lockName, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open infrastructure release lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("acquire infrastructure release lock: %w", err)
	}
	// An administrator may be moving services between reviewed overlays. The
	// installed snapshot must not undo an intermediate migration or rollback.
	_, markerErr := os.Lstat(filepath.Join(root, "network-maintenance"))
	if !maintenance && !os.IsNotExist(markerErr) {
		return errors.New("administrator network maintenance is in progress")
	}
	if maintenance && markerErr != nil {
		return errors.New("network finalization requires the administrator maintenance marker")
	}
	actual, err := fingerprint(root)
	if err != nil || actual != expected {
		return errors.New("an administrator must install this exact infrastructure snapshot first")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `set -eu
set -a
. ./deployment.env
docker stack config -c docker-compose.stack.yaml >/dev/null
docker stack deploy --resolve-image always --detach=false -c docker-compose.stack.yaml perry-infra
curl --fail --retry 12 --retry-delay 5 --retry-all-errors --max-time 10 https://bulkbets-api.perrysmithmoss.com/api/v1/health`)
	cmd.Dir = root
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock"}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	// Stop the shell and its children before releasing the lock on timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("infrastructure deployment or health check failed: %w", err)
	}
	return nil
}

func run(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "ssh" && os.Geteuid() != 0:
		command, err := sshArgs(os.Getenv("SSH_ORIGINAL_COMMAND"))
		if err != nil {
			return err
		}
		return syscall.Exec("/usr/bin/sudo", command, []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"})
	case len(args) == 1 && digestRE.MatchString(args[0]):
		return deploy(args[0])
	case len(args) == 2 && args[0] == "network-finalize" && digestRE.MatchString(args[1]) && os.Geteuid() == 0:
		// The SSH forced command and sudo rule accept only the single digest
		// form, so this maintenance path is available only to administrators.
		syscall.Umask(0o077)
		return deploySnapshotMode(snapshot, lockPath, args[1], 15*time.Minute, true)
	case len(args) == 1 && args[0] == "network-maintenance-check" && os.Geteuid() == 0:
		_, err := os.Lstat(snapshot + "/network-maintenance")
		return err
	case len(args) == 2 && args[0] == "fingerprint":
		value, err := fingerprint(args[1])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, value)
		return err
	case len(args) == 1 && args[0] == "acme-env":
		value, err := acmeEnv(os.Stdin)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, value)
		return err
	default:
		return errors.New("infrastructure release command rejected")
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
