package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const installedBinary = "/usr/local/libexec/perry-netdata-docker-api"
const installRecord = "/var/lib/perry-netdata-docker-api/installed.json"
const infraSnapshot = "/opt/perry-infra-release"

var hostFiles = map[string]string{
	"perry-netdata-docker-api.socket":  "/etc/systemd/system/perry-netdata-docker-api.socket",
	"perry-netdata-docker-api.service": "/etc/systemd/system/perry-netdata-docker-api.service",
	"docker.conf":                      "/etc/netdata/go.d/docker.conf",
	"docker-sd.conf":                   "/etc/netdata/go.d/sd/docker.conf",
	"netdata-docker.conf":              "/etc/systemd/system/netdata.service.d/restricted-docker.conf",
}
var runtimeFiles = []string{"go.mod", "main.go", "proxy.go", "admin.go", "perry-netdata-docker-api.socket", "perry-netdata-docker-api.service", "docker.conf", "docker-sd.conf", "netdata-docker.conf"}
var relayUnits = []string{"perry-metrics@15432.service", "perry-metrics@16379.service", "perry-metrics@18082.service"}

type record struct {
	Backup         string
	Root           string
	Metrics        string
	OldFingerprint string
	Fingerprint    string
	BinaryHash     string
	BrokerHash     string
	OldBrokerHash  string
	HadDocker      bool
	Absent         []string
	ServiceHashes  map[string]string
}
type operator struct {
	log string
	ctx context.Context
}

// Command output (including Docker metadata and collector diagnostics) remains
// root-only. Never expose raw errors, environment/configuration or response bodies.
func (o operator) command(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(o.ctx, 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if o.log != "" {
		if file, e := os.OpenFile(o.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600); e == nil {
			file.Write(out)
			file.Close()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%s failed; inspect private administrator log", filepath.Base(name))
	}
	return out, nil
}
func (o operator) must(name string, args ...string) error {
	_, err := o.command(name, args...)
	return err
}
func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}
func (o operator) binary(source, target string) error {
	file, err := os.CreateTemp(filepath.Dir(target), ".perry-docker-install-")
	if err != nil {
		return err
	}
	path := file.Name()
	file.Close()
	defer os.Remove(path)
	if err := o.must("install", "-o", "root", "-g", "root", "-m", "755", source, path); err != nil {
		return err
	}
	return os.Rename(path, target)
}
func saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
func dockerMember(o operator) (bool, error) {
	group, err := user.LookupGroup("docker")
	if err != nil {
		return false, err
	}
	out, err := o.command("id", "-G", "netdata")
	if err != nil {
		return false, err
	}
	for _, g := range strings.Fields(string(out)) {
		if g == group.Gid {
			return true, nil
		}
	}
	return false, nil
}

func guard() error {
	member, err := dockerMember(operator{ctx: context.Background()})
	if err != nil {
		return err
	}
	if member {
		return errors.New("netdata must not belong to the Docker group")
	}
	group, err := user.LookupGroup("netdata")
	if err != nil {
		return err
	}
	info, err := os.Lstat(socketPath)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0660 || stat.Uid != 0 || strconv.Itoa(int(stat.Gid)) != group.Gid {
		return errors.New("filtered socket must be root:netdata mode 0660")
	}
	return nil
}

func apiProbe() error {
	client := unixClient(socketPath)
	for _, path := range []string{"/_ping", "/version", "/info", "/images/json", "/containers/json?all=true"} {
		response, err := client.Get("http://docker" + path)
		if err != nil {
			return errors.New("filtered Docker read failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		response.Body.Close()
		if readErr != nil || response.StatusCode != 200 {
			return errors.New("filtered Docker read failed")
		}
		if path != "/_ping" {
			plain, _, _ := strings.Cut(path, "?")
			value, err := project(plain, body)
			if err != nil {
				return err
			}
			canonical, _ := json.Marshal(value)
			var original any
			if json.Unmarshal(body, &original) != nil {
				return errors.New("invalid filtered JSON")
			}
			normalized, _ := json.Marshal(original)
			var projected any
			json.Unmarshal(canonical, &projected)
			canonical, _ = json.Marshal(projected)
			if !bytes.Equal(normalized, canonical) {
				return errors.New("filtered Docker response contains unexpected fields")
			}
		}
	}
	for _, test := range []struct{ method, path string }{{"GET", "/secrets"}, {"GET", "/configs"}, {"GET", "/containers/example/archive?path=/etc/passwd"}, {"GET", "/containers/example/logs"}, {"GET", "/events"}, {"POST", "/containers/create"}, {"POST", "/exec/example/start"}, {"DELETE", "/containers/example"}} {
		request, _ := http.NewRequest(test.method, "http://docker"+test.path, nil)
		response, err := client.Do(request)
		if err != nil {
			return errors.New("cannot check Docker denials")
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			return errors.New("filtered Docker operation was not denied")
		}
	}
	connection, err := net.DialTimeout("unix", "/var/run/docker.sock", time.Second)
	if err == nil {
		connection.Close()
		return errors.New("netdata still reaches the raw Docker socket")
	}
	if !errors.Is(err, syscall.EACCES) {
		return errors.New("raw Docker denial was not a permissions failure")
	}
	fmt.Println("PASS: filtered Docker reads, response projection and operation denials; raw socket permission denied.")
	return nil
}

func (o operator) asNetdata(name string, args ...string) ([]byte, error) {
	account, err := user.Lookup("netdata")
	if err != nil {
		return nil, err
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return nil, err
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(o.ctx, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/var/lib/netdata", "DOCKER_HOST=unix://" + socketPath}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}, Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	out := stdout.Bytes()
	if o.log != "" {
		f, e := os.OpenFile(o.log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e == nil {
			f.Write(stderr.Bytes())
			f.Write(out)
			f.Close()
		}
	}
	if err != nil {
		return nil, errors.New("isolated netdata verification failed; inspect private administrator log")
	}
	return out, nil
}

func verifyProcessGroups(o operator) error {
	account, err := user.Lookup("netdata")
	if err != nil {
		return err
	}
	group, err := user.LookupGroup("docker")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	seen := 0
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return err
		}
		ours := false
		var groups []string
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			if fields[0] == "Uid:" {
				for _, id := range fields[1:] {
					if id == account.Uid {
						ours = true
					}
				}
			}
			if fields[0] == "Groups:" {
				groups = fields[1:]
			}
		}
		if ours {
			seen++
			for _, id := range groups {
				if id == group.Gid {
					return errors.New("a netdata process retains the Docker group; restart it through its owner service")
				}
			}
		}
	}
	if seen < 4 {
		return errors.New("expected Netdata and the three private relay processes")
	}
	fmt.Println("PASS: all current netdata processes lack the Docker group.")
	return nil
}

func serviceHashes(o operator) (map[string]string, error) {
	result := map[string]string{}
	for _, service := range []string{"perry-infra_traefik", "perry-infra_docker-socket-proxy"} {
		out, err := o.command("docker", "service", "inspect", "--format", "{{json .Spec}}", service)
		if err != nil {
			return nil, err
		}
		result[service] = digest(out)
	}
	return result, nil
}
func verify(o operator, r record) error {
	if err := guard(); err != nil {
		return err
	}
	expected, err := fileDigest(installedBinary)
	if err != nil || expected != r.BinaryHash {
		return errors.New("installed Docker reader binary drifted")
	}
	for source, target := range hostFiles {
		a, err := os.ReadFile(filepath.Join(r.Root, "monitoring/docker-api", source))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(a, b) {
			return errors.New("managed Docker reader configuration drifted")
		}
	}
	actual, err := o.command("/usr/local/libexec/perry-infra-release", "fingerprint", infraSnapshot)
	if err != nil || strings.TrimSpace(string(actual)) != r.Fingerprint {
		return errors.New("installed snapshot fingerprint mismatch")
	}
	if _, err := o.asNetdata(installedBinary, "probe"); err != nil {
		return err
	}
	fmt.Println("PASS: group-free Netdata reaches only the filtered Docker API.")
	if err := verifyProcessGroups(o); err != nil {
		return err
	}
	// Resolve current cgroup names through the actual installed helper and Docker
	// CLI/API, with no supplementary groups. Retain no environment or inspect data.
	out, err := o.command("docker", "ps", "--no-trunc", "--format", "{{.ID}} {{.Names}}")
	if err != nil {
		return err
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return errors.New("unexpected container inventory")
		}
		name, err := o.asNetdata("/usr/libexec/netdata/plugins.d/cgroup-name", "/sys/fs/cgroup/system.slice/docker-"+fields[0]+".scope", "docker_"+fields[0])
		if err != nil {
			return err
		}
		if len(strings.Fields(string(name))) == 0 || strings.Fields(string(name))[0] != fields[1] {
			return errors.New("current container name resolution did not verify")
		}
		count++
	}
	if count != 10 {
		return errors.New("expected all ten running containers")
	}
	fmt.Println("PASS: actual cgroup-name helper resolves all ten current container names without Docker-group access.")
	if err := o.must(r.Metrics, "verify", r.Root); err != nil {
		return err
	}
	if err := o.must(r.Metrics, "verify-docker"); err != nil {
		return err
	}
	if err := guard(); err != nil {
		return err
	}
	if err := verifyProcessGroups(o); err != nil {
		return err
	}
	fmt.Println("PASS: Cloud-only agent, private collectors, Docker state/health samples and public application checks.")
	hashes, err := serviceHashes(o)
	if err != nil {
		return err
	}
	for name, want := range r.ServiceHashes {
		if hashes[name] != want {
			return errors.New("Traefik or its existing socket proxy service specification changed")
		}
	}
	fmt.Println("PASS: Traefik and its existing socket proxy specifications are unchanged.")
	return nil
}

func restore(o operator, r record) error {
	return releaseLock(func() error { return restoreLocked(o, r) })
}
func restoreLocked(o operator, r record) error {
	actual, err := o.command("/usr/local/libexec/perry-infra-release", "fingerprint", infraSnapshot)
	if err != nil || (strings.TrimSpace(string(actual)) != r.Fingerprint && strings.TrimSpace(string(actual)) != r.OldFingerprint) {
		return errors.New("snapshot changed since this migration; review rollback manually")
	}
	brokerHash, err := fileDigest("/usr/local/libexec/perry-infra-release")
	if err != nil || (brokerHash != r.BrokerHash && brokerHash != r.OldBrokerHash) {
		return errors.New("release broker changed since this migration; review rollback manually")
	}
	if err := removeMarker(r); err != nil {
		return err
	}
	var failures []error
	check := func(err error) {
		if err != nil {
			failures = append(failures, err)
		}
	}
	// Restore only the original membership, never the entire group databases.
	if r.HadDocker {
		check(o.must("gpasswd", "-a", "netdata", "docker"))
	}
	check(o.must("systemctl", "stop", "netdata.service"))
	for _, unit := range []string{"perry-netdata-docker-api.socket", "perry-netdata-docker-api.service"} {
		load, err := o.command("systemctl", "show", unit, "--property=LoadState", "--value")
		if err != nil {
			check(err)
			continue
		}
		if strings.TrimSpace(string(load)) == "not-found" {
			continue
		}
		if strings.HasSuffix(unit, ".socket") {
			check(o.must("systemctl", "disable", "--now", unit))
		} else {
			check(o.must("systemctl", "stop", unit))
		}
	}
	for _, path := range r.Absent {
		if !restorable(path) {
			check(errors.New("unrecognized path in rollback record"))
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			check(err)
		}
	}
	// This subtree was required to be absent at first installation; remove only
	// the snapshot files this migration introduced before restoring the old tree.
	check(os.RemoveAll(infraSnapshot + "/monitoring/docker-api"))
	check(o.must("tar", "-xzpf", filepath.Join(r.Backup, "before.tar.gz"), "-C", "/"))
	check(o.must("systemctl", "daemon-reload"))
	check(o.must("systemctl", append([]string{"restart"}, relayUnits...)...))
	check(o.must("systemctl", "restart", "netdata.service"))
	actual, err = o.command("/usr/local/libexec/perry-infra-release", "fingerprint", infraSnapshot)
	if err != nil || strings.TrimSpace(string(actual)) != r.OldFingerprint {
		check(errors.New("rollback fingerprint mismatch"))
	}
	check(o.must(r.Metrics, "verify", r.Root))
	check(o.must(r.Metrics, "verify-docker"))
	return errors.Join(failures...)
}
func restorable(path string) bool {
	if path == installedBinary || path == installRecord {
		return true
	}
	for _, target := range hostFiles {
		if target == path {
			return true
		}
	}
	return false
}

func releaseLock(action func() error) error {
	f, err := os.OpenFile("/run/lock/perry-infra-release.lock", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("an infrastructure release is running")
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return action()
}
func removeMarker(r record) error {
	path := infraSnapshot + "/network-maintenance"
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if string(data) != "Netdata Docker API maintenance: "+r.Backup+"\n" {
		return errors.New("another administrator owns the infrastructure maintenance marker")
	}
	return os.Remove(path)
}

var rollbackDrill = errors.New("controlled rollback drill")

func install(o operator, root, metrics, broker, old, want string, drill bool) (result error) {
	fmt.Println("Checking baseline private monitoring (about 90 seconds); application services stay running.")
	for _, path := range []string{root, metrics, broker} {
		if !filepath.IsAbs(path) {
			return errors.New("require absolute reviewed paths")
		}
	}
	if len(old) != 64 || len(want) != 64 {
		return errors.New("require reviewed old and new fingerprints")
	}
	actual, err := o.command("/usr/local/libexec/perry-infra-release", "fingerprint", infraSnapshot)
	if err != nil || strings.TrimSpace(string(actual)) != old {
		return errors.New("installed infrastructure snapshot differs from reviewed baseline")
	}
	actual, err = o.command(broker, "fingerprint", root)
	if err != nil || strings.TrimSpace(string(actual)) != want {
		return errors.New("reviewed checkout differs from the expected new snapshot")
	}
	actual, err = o.command("/usr/local/libexec/perry-infra-release", "fingerprint", root)
	if err != nil || strings.TrimSpace(string(actual)) != old {
		return errors.New("this migration must preserve all existing infrastructure runtime files")
	}
	if _, err := os.Stat(infraSnapshot + "/network-maintenance"); !errors.Is(err, os.ErrNotExist) {
		return errors.New("infrastructure migration is in progress")
	}
	paths := append([]string{installedBinary, installRecord}, mapTargets()...)
	for _, path := range paths {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("first installation refuses existing managed path: %s", path)
		}
	}
	if _, err := os.Lstat(infraSnapshot + "/monitoring/docker-api"); !errors.Is(err, os.ErrNotExist) {
		return errors.New("first installation refuses an existing Docker API snapshot subtree")
	}
	a, err := os.ReadFile(filepath.Join(root, "docker-proxy/haproxy.cfg"))
	if err != nil {
		return err
	}
	b, err := os.ReadFile(infraSnapshot + "/docker-proxy/haproxy.cfg")
	if err != nil || !bytes.Equal(a, b) {
		return errors.New("existing Traefik proxy restrictions must remain unchanged")
	}
	if err := o.must(metrics, "verify", root); err != nil {
		return err
	}
	member, err := dockerMember(o)
	if err != nil {
		return err
	}
	if !member {
		return errors.New("baseline expected the reviewed existing Docker membership")
	}
	hashes, err := serviceHashes(o)
	if err != nil {
		return err
	}
	if err := os.MkdirAll("/var/backups/perry-maintenance", 0700); err != nil {
		return err
	}
	backup, err := os.MkdirTemp("/var/backups/perry-maintenance", "docker-api.")
	if err != nil {
		return err
	}
	fmt.Println("Private rollback and diagnostics:", backup)
	o.log = filepath.Join(backup, "operations.log")
	r := record{Backup: backup, Root: root, Metrics: metrics, OldFingerprint: old, Fingerprint: want, HadDocker: member, Absent: paths, ServiceHashes: hashes}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	r.BinaryHash, err = fileDigest(self)
	if err != nil {
		return err
	}
	r.BrokerHash, err = fileDigest(broker)
	if err != nil {
		return err
	}
	r.OldBrokerHash, err = fileDigest("/usr/local/libexec/perry-infra-release")
	if err != nil {
		return err
	}
	if err := saveJSON(filepath.Join(backup, "record.json"), r); err != nil {
		return err
	}
	args := []string{"-czpf", filepath.Join(backup, "before.tar.gz"), "-C", "/", "--"}
	args = append(args, "usr/local/libexec/perry-infra-release")
	if err := o.must("tar", args...); err != nil {
		return err
	}
	changed := false
	defer func() {
		if result != nil && changed {
			fmt.Println("Verification failed; restoring the retained monitoring configuration and membership.")
			recovery := operator{ctx: context.Background(), log: o.log}
			if err := restore(recovery, r); err != nil {
				result = fmt.Errorf("migration and automatic rollback failed; inspect %s", backup)
			} else {
				fmt.Println("Rollback passed; original monitoring configuration and Docker membership restored.")
				if errors.Is(result, rollbackDrill) {
					result = nil
					fmt.Println("PASS: controlled production monitoring rollback drill.")
				}
			}
		}
	}()
	if err := releaseLock(func() error {
		current, err := o.command("/usr/local/libexec/perry-infra-release", "fingerprint", infraSnapshot)
		if err != nil || strings.TrimSpace(string(current)) != old {
			return errors.New("infrastructure snapshot changed during baseline verification")
		}
		file, err := os.OpenFile(infraSnapshot+"/network-maintenance", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("cannot pause infrastructure releases for this migration")
		}
		_, err = file.WriteString("Netdata Docker API maintenance: " + r.Backup + "\n")
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			os.Remove(infraSnapshot + "/network-maintenance")
			return errors.New("cannot save infrastructure maintenance marker")
		}
		return nil
	}); err != nil {
		return err
	}
	changed = true
	fmt.Println("Installing and testing the isolated filtered Unix broker.")
	if err := o.binary(self, installedBinary); err != nil {
		return err
	}
	// Install and test the isolated broker before touching the running agent.
	for _, source := range []string{"perry-netdata-docker-api.socket", "perry-netdata-docker-api.service"} {
		if err := o.must("install", "-D", "-o", "root", "-g", "root", "-m", "644", filepath.Join(root, "monitoring/docker-api", source), hostFiles[source]); err != nil {
			return err
		}
	}
	if err := o.must("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := o.must("systemctl", "enable", "--now", "perry-netdata-docker-api.socket"); err != nil {
		return err
	}
	if _, err := o.asNetdata(installedBinary, "probe"); err != nil {
		return err
	}
	fmt.Println("PASS: filtered broker works in an isolated Netdata process without the Docker group.")
	fmt.Println("Switching Netdata and restarting its private relays to clear inherited Docker groups.")
	for source, target := range hostFiles {
		if strings.HasSuffix(source, ".service") || strings.HasSuffix(source, ".socket") {
			continue
		}
		if err := o.must("install", "-D", "-o", "root", "-g", "root", "-m", "644", filepath.Join(root, "monitoring/docker-api", source), target); err != nil {
			return err
		}
	}
	if err := o.must("gpasswd", "-d", "netdata", "docker"); err != nil {
		return err
	}
	if err := o.must("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := o.must("systemctl", append([]string{"restart"}, relayUnits...)...); err != nil {
		return err
	}
	if err := o.must("systemctl", "restart", "netdata.service"); err != nil {
		return err
	}
	// Extend only the new snapshot subtree; all existing runtime files were
	// verified identical. No application deploy, SSH policy or key is changed.
	for _, name := range runtimeFiles {
		if err := o.must("install", "-D", "-o", "root", "-g", "root", "-m", "644", filepath.Join(root, "monitoring/docker-api", name), filepath.Join(infraSnapshot, "monitoring/docker-api", name)); err != nil {
			return err
		}
	}
	if err := o.binary(broker, "/usr/local/libexec/perry-infra-release"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(installRecord), 0700); err != nil {
		return err
	}
	if err := saveJSON(installRecord, r); err != nil {
		return err
	}
	fmt.Println("Verifying live Docker, container names, private collectors and Cloud (about two minutes).")
	if err := verify(o, r); err != nil {
		return err
	}
	if drill {
		return rollbackDrill
	}
	if err := releaseLock(func() error { return removeMarker(r) }); err != nil {
		return err
	}
	fmt.Println("PASS: Netdata Docker group removed; restricted Docker monitoring verified.")
	return nil
}
func mapTargets() []string {
	var result []string
	for _, path := range hostFiles {
		result = append(result, path)
	}
	return result
}

func admin(args []string) error {
	if len(args) == 1 && args[0] == "probe" {
		return apiProbe()
	}
	if os.Geteuid() != 0 {
		return errors.New("administrator commands require sudo on the VPS")
	}
	if len(args) == 1 && args[0] == "guard" {
		return guard()
	}
	syscall.Umask(0077)
	if err := os.MkdirAll("/run/perry-netdata", 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile("/run/perry-netdata/docker-api.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another Docker monitoring operation is running")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	o := operator{ctx: ctx}
	switch {
	case len(args) == 6 && args[0] == "install":
		return install(o, args[1], args[2], args[3], args[4], args[5], false)
	case len(args) == 6 && args[0] == "drill-and-install":
		if err := install(o, args[1], args[2], args[3], args[4], args[5], true); err != nil {
			return err
		}
		return install(o, args[1], args[2], args[3], args[4], args[5], false)
	case len(args) == 1 && args[0] == "verify":
		data, err := os.ReadFile(installRecord)
		if err != nil {
			return err
		}
		var r record
		if err := json.Unmarshal(data, &r); err != nil {
			return err
		}
		o.log = filepath.Join(r.Backup, "operations.log")
		return verify(o, r)
	case len(args) == 2 && args[0] == "rollback":
		path := filepath.Clean(args[1])
		if filepath.Dir(path) != "/var/backups/perry-maintenance" || !strings.HasPrefix(filepath.Base(path), "docker-api.") {
			return errors.New("require the exact private Docker API rollback directory")
		}
		data, err := os.ReadFile(filepath.Join(path, "record.json"))
		if err != nil {
			return err
		}
		var r record
		if err := json.Unmarshal(data, &r); err != nil {
			return err
		}
		if r.Backup != path {
			return errors.New("rollback directory mismatch")
		}
		o.log = filepath.Join(path, "operations.log")
		return restore(o, r)
	default:
		return errors.New("usage: serve | guard | probe | install|drill-and-install CHECKOUT METRICS_BINARY BROKER_BINARY OLD_FINGERPRINT NEW_FINGERPRINT | verify | rollback PRIVATE_BACKUP_DIRECTORY")
	}
}
