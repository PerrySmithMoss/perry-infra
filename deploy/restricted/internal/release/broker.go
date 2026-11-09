package release

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"time"
)

var (
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commandPattern     = regexp.MustCompile(`^infrastructure ([0-9a-f]{64})$`)
)

func SSHArgs(command string) []string {
	match := commandPattern.FindStringSubmatch(command)
	if match == nil {
		return nil
	}
	return match[1:]
}

func validArgs(args []string) bool {
	return len(args) == 1 && fingerprintPattern.MatchString(args[0])
}

func Run(args []string, stdout, stderr io.Writer) error {
	if !validArgs(args) || os.Geteuid() != 0 {
		return errors.New("expected a root release with the reviewed snapshot fingerprint")
	}
	syscall.Umask(0o077)
	return deploy("/opt/perry-infra-release", "/run/lock/perry-infra-release.lock", args[0], 15*time.Minute, stdout, stderr)
}

func deploy(root, lockPath, expected string, timeout time.Duration, stdout, stderr io.Writer) error {
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err
	}
	// Keep the lock through verification and deployment. The installer uses it too.
	if actual, err := Fingerprint(root); err != nil || actual != expected {
		return errors.New("installed infrastructure snapshot does not match")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `set -eu; set -a; . ./deployment.env
docker stack config -c docker-compose.stack.yaml >/dev/null
docker stack deploy --resolve-image always --detach=false -c docker-compose.stack.yaml perry-infra
curl --fail --retry 12 --retry-delay 5 --retry-all-errors --max-time 10 https://bulkbets-api.perrysmithmoss.com/api/v1/health`)
	cmd.Dir = root
	cmd.Env = []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock",
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}
