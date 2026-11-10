//go:build linux

// portfolio-release provides an image-only deployment path for portfolio services.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	installed    = "/usr/local/libexec/portfolio-release"
	stateDir     = "/opt/portfolio-release"
	dockerSocket = "/var/run/docker.sock"
)

type policy struct {
	service, repository string
}

var apps = map[string]policy{
	"chinwag":      {"chinwag_chinwag-api", "ghcr.io/perrysmithmoss/chinwag"},
	"fleet":        {"fleet_fleet-api", "ghcr.io/perrysmithmoss/logistics-realtime-dashboard"},
	"im-listening": {"im-listening_api", "ghcr.io/perrysmithmoss/im-listening-api"},
	"vita-track":   {"vita-track_vita-track-api", "ghcr.io/perrysmithmoss/vita-track"},
}

var (
	digestRE    = regexp.MustCompile("^sha256:[a-f0-9]{64}$")
	revisionRE  = regexp.MustCompile("^[a-f0-9]{40}$")
	serviceIDRE = regexp.MustCompile("^[a-z0-9]{25}$")
)

func validRelease(args []string) bool {
	return len(args) == 2 && digestRE.MatchString(args[0]) && revisionRE.MatchString(args[1])
}

func sshArgs(account, original string) ([]string, error) {
	app, ok := strings.CutSuffix(account, "-release")
	if _, configured := apps[app]; !ok || !configured || len(original) != len("release sha256:")+64+1+40 {
		return nil, errors.New("release command rejected")
	}
	parts := strings.Split(original, " ")
	if len(parts) != 3 || parts[0] != "release" || !validRelease(parts[1:]) {
		return nil, errors.New("release command rejected")
	}
	return []string{"-n", "--", installed, "release", app, parts[1], parts[2]}, nil
}

type dockerRun func(time.Duration, ...string) ([]byte, error)

var cleanEnv = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "HOME=/", "DOCKER_CONFIG=" + stateDir + "/docker"}

func docker(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/docker", append([]string{"--host", "unix://" + dockerSocket}, args...)...)
	cmd.Env, cmd.Dir, cmd.WaitDelay = cleanEnv, "/", time.Second
	out, err := cmd.Output() // Never forward Docker diagnostics or service configuration to SSH.
	if err != nil {
		return nil, fmt.Errorf("Docker %s %s failed", args[0], args[1])
	}
	return out, nil
}

type service struct {
	ID           string
	Version      struct{ Index uint64 }
	Spec         map[string]any
	UpdateStatus struct{ State string }
}

func inspect(run dockerRun, name string) (service, error) {
	var result []service
	out, err := run(15*time.Second, "service", "inspect", name)
	if err != nil {
		return service{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(out))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil || len(result) != 1 || result[0].Spec["Name"] != name || !serviceIDRE.MatchString(result[0].ID) || result[0].Version.Index == 0 {
		return service{}, errors.New("unexpected service")
	}
	return result[0], nil
}

// The administrator records the entire reviewed service spec, except its image.
// This pins all secret IDs, mounts, network IDs, capabilities, routing and options.
func (s service) fingerprint(p policy) (string, string, error) {
	task, _ := s.Spec["TaskTemplate"].(map[string]any)
	container, _ := task["ContainerSpec"].(map[string]any)
	image, _ := container["Image"].(string)
	prefix, digest, _ := strings.Cut(image, "@")
	if !digestRE.MatchString(digest) || (prefix != p.repository && !strings.HasPrefix(prefix, p.repository+":")) {
		return "", "", errors.New("existing service does not use an immutable image from the approved repository")
	}
	delete(container, "Image")
	data, err := json.Marshal(s.Spec)
	container["Image"] = image
	if err != nil {
		return "", "", errors.New("cannot fingerprint service")
	}
	return fmt.Sprintf("%x", sha256.Sum256(append([]byte(s.ID+"\n"), data...))), digest, nil
}

func ensureSpec(s service, p policy, expected string) (string, error) {
	fingerprint, digest, err := s.fingerprint(p)
	if err != nil || fingerprint != expected {
		return "", errors.New("live service differs from the administrator-reviewed policy")
	}
	return digest, nil
}

var engine = &http.Transport{
	DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", dockerSocket)
	},
}

func updateImage(engine http.RoundTripper, s service, image string) error {
	// Use the exact version and spec that passed the policy check. A Docker CLI
	// service update would re-read the spec and could inherit concurrent changes.
	container := s.Spec["TaskTemplate"].(map[string]any)["ContainerSpec"].(map[string]any)
	container["Image"] = image
	body, err := json.Marshal(s.Spec)
	if err != nil {
		return errors.New("cannot encode service update")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	endpoint := fmt.Sprintf("http://docker/services/%s/update?version=%d", s.ID, s.Version.Index)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("cannot prepare service update")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := engine.RoundTrip(req)
	if err != nil {
		return errors.New("Docker service update failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("Docker rejected the service update; verify the service version and policy before retrying")
	}
	return nil
}

func release(run dockerRun, engine http.RoundTripper, p policy, expected string, args []string, attempts int, interval time.Duration) error {
	if !validRelease(args) {
		return errors.New("invalid release arguments")
	}
	s, err := inspect(run, p.service)
	if err != nil {
		return err
	}
	if _, err := ensureSpec(s, p, expected); err != nil {
		return err
	}
	image := p.repository + "@" + args[0]
	if _, err := run(3*time.Minute, "image", "pull", image); err != nil {
		return err
	}
	label, err := run(15*time.Second, "image", "inspect", "--format", "{{index .Config.Labels \"org.opencontainers.image.revision\"}}", image)
	if err != nil || string(label) != args[1]+"\n" {
		return errors.New("image revision does not match requested commit")
	}
	s, err = inspect(run, p.service)
	if err != nil {
		return err
	}
	current, err := ensureSpec(s, p, expected)
	if err != nil {
		return err
	}
	changed := current != args[0]
	if changed {
		if err := updateImage(engine, s, image); err != nil {
			return err
		}
	}
	// A successful update request only queues the deployment. Wait for both the
	// manager's final UpdateStatus and the running task, with a bound.
	deadline := time.Now().Add(4*time.Minute + 30*time.Second)
	for i := 0; i < attempts && time.Now().Before(deadline); i++ {
		if i > 0 {
			time.Sleep(interval)
		}
		s, err = inspect(run, p.service)
		if err != nil {
			return err
		}
		current, err = ensureSpec(s, p, expected)
		if err != nil {
			return err
		}
		if current != args[0] {
			return errors.New("Swarm service is no longer pinned to the requested digest")
		}
		switch state := s.UpdateStatus.State; {
		case state == "updating", changed && state == "":
			continue
		case state == "completed", !changed && (state == "" || state == "rollback_completed"):
			// A previously completed rollback to this digest is an idempotent
			// release only if the requested task is still running below.
		default:
			return errors.New("Swarm paused or rolled back the release")
		}
		rows, err := run(15*time.Second, "service", "ps", "--no-trunc", "--filter", "desired-state=running", "--format", "{{.Image}}|{{.CurrentState}}", s.ID)
		if err != nil {
			return err
		}
		prefix, status, found := strings.Cut(strings.TrimSuffix(string(rows), "\n"), "|")
		if found && !strings.Contains(status, "\n") && strings.HasSuffix(prefix, "@"+args[0]) && strings.HasPrefix(status, "Running ") {
			return nil
		}
	}
	return errors.New("timed out waiting for the requested digest to converge on exactly one running task")
}

func withLock(name string, action func() error) error {
	lock, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return errors.New("cannot open release lock")
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return errors.New("another release or policy installation is in progress")
	}
	return action()
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "ssh" && os.Getuid() != 0 {
		account, err := user.Current()
		if err != nil {
			return errors.New("unknown release account")
		}
		command, err := sshArgs(account.Username, os.Getenv("SSH_ORIGINAL_COMMAND"))
		if err != nil {
			return err
		}
		return syscall.Exec("/usr/bin/sudo", append([]string{"sudo"}, command...), cleanEnv)
	}
	if os.Getuid() != 0 || len(args) < 2 {
		return errors.New("command rejected")
	}
	app := args[1]
	p, ok := apps[app]
	if !ok {
		return errors.New("unknown release policy")
	}
	return withLock(stateDir+"/"+app+".lock", func() error {
		switch {
		case args[0] == "record" && len(args) == 2:
			s, err := inspect(docker, p.service)
			if err != nil {
				return err
			}
			fingerprint, _, err := s.fingerprint(p)
			if err != nil {
				return err
			}
			target := stateDir + "/" + app + ".sha256"
			tmp, err := os.CreateTemp(stateDir, app+".policy-")
			if err != nil {
				return err
			}
			defer os.Remove(tmp.Name())
			_, writeErr := fmt.Fprintln(tmp, fingerprint)
			if err := errors.Join(writeErr, tmp.Close()); err != nil {
				return err
			}
			return os.Rename(tmp.Name(), target)
		case args[0] == "release" && validRelease(args[2:]):
			expected, err := os.ReadFile(stateDir + "/" + app + ".sha256")
			if err != nil {
				return errors.New("no administrator-reviewed release policy")
			}
			return release(docker, engine, p, strings.TrimSuffix(string(expected), "\n"), args[2:], 270, time.Second)
		default:
			return errors.New("command rejected")
		}
	})
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Portfolio release check passed.")
}
