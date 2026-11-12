// network-admin is an administrator-only migration, never a release command.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

const state = "/var/lib/perry-network-isolation"
const snapshot = "/opt/perry-infra-release"
const broker = "/usr/local/libexec/perry-infra-release"
const marker = snapshot + "/network-maintenance"

const healthTimeout = 2 * time.Minute
const healthRetryInterval = 5 * time.Second

//go:embed verify-client.mjs
var clientScript string

type application struct {
	Name, Service, Ingress, Database, Redis, Health string
	Port                                            int
}

var apps = []application{
	{"fleet", "fleet_fleet-api", "fleet_ingress", "", "", "https://fleet-api.perrysmithmoss.com/health/ready", 5570},
	{"chinwag", "chinwag_chinwag-api", "chinwag_ingress", "chinwag_database", "", "https://chinwag-api.perrysmithmoss.com/healthz", 5560},
	{"vita-track", "vita-track_vita-track-api", "vita_track_ingress", "vita_track_database", "", "https://vita-track-api.perrysmithmoss.com/healthz", 5500},
	{"im-listening", "im-listening_api", "im_listening_ingress", "im_listening_database", "im_listening_redis_clients", "https://im-listening-api.perrysmithmoss.com/healthz", 5050},
}

type overlay struct {
	Name, Subnet string
	Internal     bool
}

var overlays = []overlay{
	{"chinwag_ingress", "10.42.30.0/24", false}, {"chinwag_database", "10.42.31.0/24", true},
	{"fleet_ingress", "10.42.32.0/24", false},
	{"im_listening_ingress", "10.42.34.0/24", false}, {"im_listening_database", "10.42.35.0/24", true},
	{"vita_track_ingress", "10.42.36.0/24", false}, {"vita_track_database", "10.42.37.0/24", true},
	{"im_listening_redis_clients", "10.42.38.0/24", true},
}

var infrastructure = []string{"perry-infra_traefik", "perry-infra_postgres", "perry-infra_redis"}

type service struct {
	ID           string
	Version      struct{ Index uint64 }
	Spec         map[string]any
	UpdateStatus struct{ State string }
	Endpoint     struct {
		VirtualIPs []struct{ NetworkID, Addr string }
	}
}

type network struct {
	ID, Name, Driver, Scope       string
	Internal, Attachable, Ingress bool
	Options                       map[string]string
	IPAM                          struct{ Config []struct{ Subnet string } }
}

func command(input io.Reader, program string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock"}
	cmd.Stdin = input
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s operation failed; inspect diagnostics privately on the VPS", filepath.Base(program))
	}
	return out, nil
}

func docker(args ...string) ([]byte, error) { return command(nil, "/usr/bin/docker", args...) }

func decode(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(target)
}

func inspect(name string) (service, error) {
	data, err := docker("service", "inspect", name)
	var rows []service
	if err != nil || decode(data, &rows) != nil || len(rows) != 1 || rows[0].Spec["Name"] != name || rows[0].ID == "" {
		return service{}, errors.New("cannot inspect expected service " + name)
	}
	return rows[0], nil
}

func task(s service) map[string]any          { return s.Spec["TaskTemplate"].(map[string]any) }
func containerSpec(s service) map[string]any { return task(s)["ContainerSpec"].(map[string]any) }

func inspectNetwork(name string) (network, error) {
	data, err := docker("network", "inspect", name)
	var rows []network
	if err != nil || json.Unmarshal(data, &rows) != nil || len(rows) != 1 {
		return network{}, errors.New("cannot inspect network " + name)
	}
	return rows[0], nil
}

func checkNetwork(n network, want overlay) error {
	_, encrypted := n.Options["encrypted"]
	if n.Name != want.Name || n.Driver != "overlay" || n.Scope != "swarm" || n.Ingress || n.Attachable || n.Internal != want.Internal || !encrypted || len(n.IPAM.Config) != 1 || n.IPAM.Config[0].Subnet != want.Subnet {
		return errors.New("network differs from reviewed isolation policy: " + want.Name)
	}
	return nil
}

func locked(action func() error, infra bool) error {
	paths := []string{}
	for _, app := range apps {
		paths = append(paths, "/opt/portfolio-release/"+app.Name+".lock")
	}
	if infra {
		paths = append(paths, "/run/lock/perry-infra-release.lock")
	}
	return lockFiles(paths, action)
}

func infraLocked(action func() error) error {
	return lockFiles([]string{"/run/lock/perry-infra-release.lock"}, action)
}

func lockFiles(paths []string, action func() error) error {
	var files []*os.File
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	for _, path := range paths {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
		if err != nil {
			return err
		}
		files = append(files, f)
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			return errors.New("a release or administrator change is in progress")
		}
	}
	return action()
}

func private(name string, data []byte) error {
	return atomicFile(filepath.Join(state, name), data, 0600)
}

func atomicFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".network-write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	chmodErr := f.Chmod(mode)
	if err := errors.Join(writeErr, chmodErr, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func saved(name string) (service, error) {
	data, err := os.ReadFile(filepath.Join(state, name+".json"))
	var s service
	if err != nil || decode(data, &s) != nil || s.Spec["Name"] != name || s.ID == "" {
		return s, errors.New("missing or invalid retained service specification")
	}
	return s, nil
}

func sameSpec(a, b service) bool {
	x, e1 := json.Marshal(a.Spec)
	y, e2 := json.Marshal(b.Spec)
	return e1 == nil && e2 == nil && a.ID == b.ID && bytes.Equal(x, y)
}

func update(s service) error {
	fmt.Println("Updating service:", s.Spec["Name"])
	if err := submitService(s); err != nil {
		return err
	}
	return wait(s)
}

func submitService(s service) error {
	body, err := json.Marshal(s.Spec)
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	url := fmt.Sprintf("http://docker/v1.41/services/%s/update?version=%d&registryAuthFrom=spec", s.ID, s.Version.Index)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return errors.New("Docker service update failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("Docker rejected the service version/specification; no retry without inspection")
	}
	return nil
}

func wait(want service) error {
	name := want.Spec["Name"].(string)
	for attempt := 0; attempt < 240; attempt++ {
		if attempt > 0 && attempt%15 == 0 {
			fmt.Println("Still waiting for a healthy current task:", name)
		}
		s, err := inspect(name)
		if err != nil {
			return err
		}
		if !sameSpec(s, want) {
			return errors.New("service changed or rolled back during convergence: " + name)
		}
		switch s.UpdateStatus.State {
		case "paused", "rollback_started", "rollback_paused", "rollback_completed":
			return errors.New("service update failed: " + name)
		}
		out, err := docker("ps", "-q", "--filter", "label=com.docker.swarm.service.name="+name)
		if err != nil {
			return err
		}
		ids := strings.Fields(string(out))
		if len(ids) == 1 && s.UpdateStatus.State != "updating" {
			health, err := docker("inspect", "--format", "{{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", ids[0])
			if err != nil {
				return err
			}
			if h := strings.TrimSpace(string(health)); h == "true healthy" || h == "true none" {
				ready, err := runningTaskMatches(want)
				if err != nil {
					return err
				}
				if ready {
					return nil
				}
			}
		}
		time.Sleep(time.Second)
	}
	return errors.New("timed out waiting for a healthy single replica: " + name)
}

type taskRecord struct {
	ServiceID string
	Status    struct{ State string }
	Spec      map[string]any
}

func taskMatches(s service, t taskRecord) bool {
	if t.ServiceID != s.ID || t.Status.State != "running" {
		return false
	}
	container, _ := t.Spec["ContainerSpec"].(map[string]any)
	if container["Image"] != containerSpec(s)["Image"] {
		return false
	}
	want, e1 := json.Marshal(task(s)["Networks"])
	got, e2 := json.Marshal(t.Spec["Networks"])
	return e1 == nil && e2 == nil && bytes.Equal(want, got)
}

func runningTaskMatches(s service) (bool, error) {
	out, err := docker("service", "ps", "-q", "--no-trunc", "--filter", "desired-state=running", s.ID)
	if err != nil {
		return false, err
	}
	ids := strings.Fields(string(out))
	if len(ids) != 1 {
		return false, nil
	}
	out, err = docker("inspect", "--type", "task", ids[0])
	if err != nil {
		return false, err
	}
	var rows []taskRecord
	if decode(out, &rows) != nil || len(rows) != 1 {
		return false, errors.New("cannot inspect converging task specification")
	}
	return taskMatches(s, rows[0]), nil
}

func networkNames(s service) ([]string, error) {
	var names []string
	rows, _ := task(s)["Networks"].([]any)
	for _, row := range rows {
		n, err := inspectNetwork(row.(map[string]any)["Target"].(string))
		if err != nil {
			return nil, err
		}
		names = append(names, n.Name)
	}
	slices.Sort(names)
	return names, nil
}

func desired(app application) []string {
	names := []string{app.Ingress}
	for _, n := range []string{app.Database, app.Redis} {
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}

func setNetworks(s service, names []string, alias string) error {
	return setNetworksWith(s, names, alias, inspectNetwork)
}

func setNetworksWith(s service, names []string, alias string, resolve func(string) (network, error)) error {
	rows := []any{}
	for _, name := range names {
		n, err := resolve(name)
		if err != nil {
			return err
		}
		row := map[string]any{"Target": n.ID}
		if alias != "" {
			row["Aliases"] = []string{alias}
		}
		rows = append(rows, row)
	}
	task(s)["Networks"] = rows
	return nil
}

func health() error {
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second}
	urls := []string{"https://bulkbets-api.perrysmithmoss.com/api/v1/health"}
	for _, app := range apps {
		urls = append(urls, app.Health)
	}
	return waitForHealth(ctx, client, urls, healthRetryInterval)
}

// Swarm task convergence can precede application startup and Traefik's next
// refresh. Require a successful sweep of every route within one shared deadline.
func waitForHealth(ctx context.Context, client *http.Client, urls []string, interval time.Duration) error {
	var failure error
	for {
		if err := ctx.Err(); err != nil {
			if failure != nil {
				return fmt.Errorf("public HTTPS health did not become ready (%v): %w", failure, err)
			}
			return fmt.Errorf("public HTTPS health check canceled: %w", err)
		}
		failure = nil
		for _, url := range urls {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return errors.New("invalid public HTTPS health URL: " + url)
			}
			resp, err := client.Do(req)
			if err != nil {
				failure = errors.New("public HTTPS health request failed: " + url)
				break
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				failure = fmt.Errorf("public HTTPS health returned HTTP %d: %s", resp.StatusCode, url)
				break
			}
		}
		if failure == nil {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func prepare(binary string) error {
	return locked(func() error { return prepareHeld(binary) }, true)
}

// Held variants run with all application release locks already held. Callers
// additionally hold the infrastructure lock for preparation and rollback.
func prepareHeld(binary string) error {
	if !filepath.IsAbs(binary) {
		return errors.New("use an absolute path to the reviewed Linux infrastructure broker")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		return errors.New("network maintenance already exists")
	}
	nodes, err := docker("node", "ls", "-q")
	if err != nil || len(strings.Fields(string(nodes))) != 1 {
		return errors.New("this migration requires the existing single-node Swarm")
	}
	if err := health(); err != nil {
		return err
	}
	if err := preflightNetworks(); err != nil {
		return err
	}
	if _, err := command(nil, binary, "fingerprint", snapshot); err != nil {
		return err
	}
	if err := os.Mkdir(state, 0700); err != nil {
		return errors.New("migration state already exists; preserve it and inspect before retrying")
	}
	names := append([]string{}, infrastructure...)
	for _, app := range apps {
		names = append(names, app.Service)
	}
	for _, name := range names {
		s, err := inspect(name)
		if err != nil {
			return err
		}
		mode, _ := s.Spec["Mode"].(map[string]any)
		replicated, _ := mode["Replicated"].(map[string]any)
		if replicated["Replicas"] != json.Number("1") {
			return errors.New("expected exactly one configured replica: " + name)
		}
		if s.UpdateStatus.State == "updating" {
			return errors.New("service is still updating")
		}
		if !strings.Contains(containerSpec(s)["Image"].(string), "@sha256:") {
			return errors.New("expected retained immutable image")
		}
		data, _ := json.Marshal(s)
		if err := private(name+".json", data); err != nil {
			return err
		}
	}
	for _, app := range apps {
		s, _ := saved(app.Service)
		nets, err := networkNames(s)
		want := []string{"portfolio_network"}
		if app.Redis != "" {
			want = append(want, "redis_clients")
		}
		slices.Sort(want)
		if err != nil || !slices.Equal(nets, want) {
			return errors.New("application dependencies differ from the reviewed inventory: " + app.Name)
		}
	}
	_, err = command(nil, "/bin/tar", "-C", "/", "-czf", state+"/installation.tar.gz",
		"opt/perry-infra-release", "usr/local/libexec/perry-infra-release", "etc/sudoers.d/perry-infra-release",
		"etc/ssh/sshd_config.d/11-perry-infra-release.conf", "var/lib/infra-release/.ssh/authorized_keys",
		"opt/portfolio-release/chinwag.sha256", "opt/portfolio-release/fleet.sha256",
		"opt/portfolio-release/vita-track.sha256", "opt/portfolio-release/im-listening.sha256")
	if err != nil {
		return err
	}
	authorized, err := os.ReadFile("/var/lib/infra-release/.ssh/authorized_keys")
	if err != nil {
		return err
	}
	key := regexp.MustCompile(`ssh-ed25519 [A-Za-z0-9+/=]+(?: [^\r\n]*)?`).Find(authorized)
	if key == nil {
		return errors.New("cannot retain the installed release public key")
	}
	if err := private("release.pub", append(key, '\n')); err != nil {
		return err
	}
	if err := private("backed-up", []byte("retained specifications, installation and policies\n")); err != nil {
		return err
	}
	if err := os.WriteFile(marker, []byte("network isolation migration; see "+state+"\n"), 0600); err != nil {
		return err
	}
	// An older broker must fail before any network/service mutation. It does
	// not implement this administrator-only maintenance capability check.
	if _, err := command(nil, binary, "network-maintenance-check"); err != nil {
		return err
	}
	// Install only the reviewed broker now. Keep the old snapshot until every
	// application has cut over; its guard prevents CI undoing intermediate work.
	data, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	if err := atomicFile(broker, data, 0755); err != nil {
		return err
	}
	for _, want := range overlays {
		n, err := inspectNetwork(want.Name)
		if err != nil {
			args := []string{"network", "create", "--driver", "overlay", "--opt", "encrypted", "--subnet", want.Subnet, "--attachable=false"}
			if want.Internal {
				args = append(args, "--internal")
			}
			args = append(args, want.Name)
			if _, err := docker(args...); err != nil {
				return err
			}
			n, err = inspectNetwork(want.Name)
			if err != nil {
				return err
			}
		}
		if err := checkNetwork(n, want); err != nil {
			return err
		}
	}
	for _, name := range infrastructure {
		fmt.Println("Preparing shared service:", name)
		s, err := inspect(name)
		if err != nil {
			return err
		}
		nets, err := networkNames(s)
		if err != nil {
			return err
		}
		for _, app := range apps {
			switch name {
			case "perry-infra_traefik":
				nets = append(nets, app.Ingress)
			case "perry-infra_postgres":
				if app.Database != "" {
					nets = append(nets, app.Database)
				}
			case "perry-infra_redis":
				if app.Redis != "" {
					nets = append(nets, app.Redis)
				}
			}
		}
		alias := strings.TrimPrefix(name, "perry-infra_")
		if err := setNetworks(s, nets, alias); err != nil {
			return err
		}
		if err := update(s); err != nil {
			return err
		}
		if err := recoverApplicationClients(); err != nil {
			return err
		}
		if err := health(); err != nil {
			return err
		}
	}
	return private("prepared", []byte("old overlays retained; cut over one application at a time\n"))
}

func require(name string) error {
	if _, err := os.Stat(filepath.Join(state, name)); err != nil {
		return errors.New("required migration phase is missing: " + name)
	}
	return nil
}

func active() error {
	if err := require("backed-up"); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(state, "rolled-back")); !os.IsNotExist(err) {
		return errors.New("migration was rolled back; retain its audit state")
	}
	if _, err := os.Lstat(marker); err != nil {
		return errors.New("administrator maintenance marker is missing")
	}
	return nil
}

func cutover(app application) error {
	return locked(func() error { return cutoverHeld(app) }, true)
}

func cutoverHeld(app application) error {
	if err := active(); err != nil {
		return err
	}
	if err := require("prepared"); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(state, "finalized")); !os.IsNotExist(err) {
		return errors.New("use full rollback after finalization")
	}
	before, err := saved(app.Service)
	if err != nil {
		return err
	}
	current, err := inspect(app.Service)
	if err != nil {
		return err
	}
	if !sameSpec(before, current) {
		return errors.New("service changed since inventory; inspect before cutover")
	}
	if err := setNetworks(current, desired(app), strings.SplitN(app.Service, "_", 2)[1]); err != nil {
		return err
	}
	labels := current.Spec["Labels"].(map[string]any)
	labels["traefik.swarm.network"] = app.Ingress
	if err := update(current); err != nil {
		return err
	}
	if err := verifyClient(app, false); err != nil {
		return err
	}
	if err := health(); err != nil {
		return err
	}
	// Policy recording is a separate administrator command after the user
	// verifies this app's real authentication and core flows. Until then the
	// old policy rejects any image-only release of the changed specification.
	return private("cutover-"+app.Name, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"))
}

func finalize(expected string) error {
	return locked(func() error { return finalizeHeld(expected) }, false)
}

func finalizeHeld(expected string) error {
	if err := active(); err != nil {
		return err
	}
	for _, app := range apps {
		if err := require("cutover-" + app.Name); err != nil {
			return err
		}
	}
	// The installer must have installed the final reviewed checkout first.
	// Its marker is retained, so ordinary CI still cannot deploy this phase.
	if _, err := command(nil, broker, "network-finalize", expected); err != nil {
		return err
	}
	if err := recoverApplicationClients(); err != nil {
		return err
	}
	if err := verify(); err != nil {
		return err
	}
	return private("finalized", []byte(time.Now().UTC().Format(time.RFC3339)+"\n"))
}

func checkResources(s service) error {
	rows, _ := task(s)["Networks"].([]any)
	for _, row := range rows {
		if _, err := docker("network", "inspect", row.(map[string]any)["Target"].(string)); err != nil {
			return errors.New("a retained rollback network was retired; nothing restored")
		}
	}
	for _, pair := range [][3]string{{"Secrets", "secret", "SecretID"}, {"Configs", "config", "ConfigID"}} {
		refs, _ := containerSpec(s)[pair[0]].([]any)
		for _, ref := range refs {
			if _, err := docker(pair[1], "inspect", ref.(map[string]any)[pair[2]].(string)); err != nil {
				return errors.New("a retained rollback secret/config was retired; nothing restored")
			}
		}
	}
	return nil
}

func rollback() error {
	return locked(rollbackHeld, true)
}

func rollbackHeld() error {
	if err := require("backed-up"); err != nil {
		return err
	}
	if err := require("installation.tar.gz"); err != nil {
		return err
	}
	// Validate/extract recovery input before touching any live service. Stage
	// executable restoration and rename it, rather than overwriting a binary
	// that an arriving forced-command SSH session may have open.
	restore, err := os.MkdirTemp(state, "restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(restore)
	if _, err := command(nil, "/bin/tar", "-C", restore, "-xzf", state+"/installation.tar.gz"); err != nil {
		return err
	}
	paths := []string{
		"usr/local/libexec/perry-infra-release", "etc/sudoers.d/perry-infra-release",
		"etc/ssh/sshd_config.d/11-perry-infra-release.conf", "var/lib/infra-release/.ssh/authorized_keys",
	}
	for _, app := range apps {
		paths = append(paths, "opt/portfolio-release/"+app.Name+".sha256")
	}
	for _, path := range paths {
		if info, err := os.Lstat(filepath.Join(restore, path)); err != nil || !info.Mode().IsRegular() {
			return errors.New("retained installation is incomplete; nothing restored")
		}
	}
	names := append([]string{}, infrastructure...)
	for _, app := range apps {
		names = append(names, app.Service)
	}
	var retained []service
	for _, name := range names {
		s, err := saved(name)
		if err != nil {
			return err
		}
		current, err := inspect(name)
		if err != nil {
			return err
		}
		if s.ID != current.ID {
			return errors.New("service was replaced; refusing rollback")
		}
		if err := checkResources(s); err != nil {
			return err
		}
		if _, err := docker("image", "inspect", containerSpec(s)["Image"].(string)); err != nil {
			return errors.New("retained rollback image is missing")
		}
		retained = append(retained, s)
	}
	// Restore old shared-service paths before returning applications to them.
	// No volume, database, Redis key, credential or image is deleted/restored.
	for _, s := range retained {
		current, err := inspect(s.Spec["Name"].(string))
		if err != nil {
			return err
		}
		if sameSpec(s, current) {
			continue
		}
		s.Version = current.Version
		if err := update(s); err != nil {
			return err
		}
	}
	if _, err := command(nil, "/bin/cp", "-a", restore+"/opt/perry-infra-release/.", snapshot+"/"); err != nil {
		return err
	}
	for _, path := range paths {
		from := filepath.Join(restore, path)
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		info, err := os.Stat(from)
		if err != nil {
			return err
		}
		if err := atomicFile("/"+path, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, check := range [][]string{{"/usr/sbin/visudo", "-c"}, {"/usr/sbin/sshd", "-t"}, {"/usr/bin/systemctl", "reload", "ssh"}} {
		if _, err := command(nil, check[0], check[1:]...); err != nil {
			return err
		}
	}
	if err := recoverApplicationClients(); err != nil {
		return err
	}
	if err := health(); err != nil {
		return err
	}
	return private("rolled-back", []byte(time.Now().UTC().Format(time.RFC3339)+"\n"))
}

func run(args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("use administrator sudo; Docker group access is insufficient")
	}
	syscall.Umask(0077)
	if len(args) == 2 && args[0] == "run" && regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(args[1]) {
		return guidedCutover(args[1])
	}
	if len(args) == 2 && args[0] == "resume-preparation" && regexpDigest(args[1]) {
		return resumePreparation(args[1])
	}
	if len(args) == 2 && args[0] == "resume-finalization" && regexpDigest(args[1]) {
		return resumeFinalization(args[1])
	}
	if len(args) == 2 && args[0] == "resume-phases" && regexpDigest(args[1]) {
		return resumePhases(args[1])
	}
	if len(args) == 2 && args[0] == "prepare" {
		return prepare(args[1])
	}
	if len(args) == 2 && args[0] == "cutover" {
		for _, app := range apps {
			if app.Name == args[1] {
				return cutover(app)
			}
		}
	}
	if len(args) == 2 && args[0] == "finalize" && regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(args[1]) {
		return finalize(args[1])
	}
	if len(args) == 1 {
		switch args[0] {
		case "verify":
			return locked(verify, true)
		case "rollback":
			return rollback()
		}
	}
	return errors.New("usage: network-admin run EXPECTED_FINGERPRINT | resume-preparation EXPECTED_FINGERPRINT | resume-finalization EXPECTED_FINGERPRINT | resume-phases EXPECTED_FINGERPRINT | prepare ABSOLUTE_INFRA_BROKER | cutover fleet|chinwag|vita-track|im-listening | finalize EXPECTED_FINGERPRINT | verify | rollback")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Network administrator operation passed. Retain rollback inputs and complete the production verification record.")
}
