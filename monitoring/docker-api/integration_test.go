package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Only this disposable local/CI broker sees the test daemon's socket. The
// unprivileged real collector and name helper receive only a filtered socket.
func TestActualNetdataWithoutDockerSocket(t *testing.T) {
	if os.Getenv("DOCKER_API_INTEGRATION") != "1" {
		t.Skip("set DOCKER_API_INTEGRATION=1 with a local/CI Docker daemon")
	}
	image := "netdata/netdata:v2.12.3@sha256:c7eab00e20901eb60595ebae0fbfe88e59464e663924461874719d40de0735d2"
	cliImage := "docker:29.9.3-cli@sha256:5a7af0dcb0e46c803efe9b45371dd21cb500e5b4453250394ac6f06d49b6e6f1"
	prefix := "perry-docker-reader-test-" + time.Now().Format("150405.000000000")
	volume := prefix + "-files"
	seed, broker, collector := prefix+"-seed", prefix+"-broker", prefix+"-collector"
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture Docker %s failed: %s", args[0], out)
		}
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() {
		for _, name := range []string{collector, broker, seed} {
			exec.Command("docker", "rm", "-fv", name).Run()
		}
		exec.Command("docker", "volume", "rm", volume).Run()
	})
	run("pull", image)
	run("volume", "create", volume)
	seedID := run("run", "-d", "--name", seed, "--network", "none", "--mount", "type=volume,source="+volume+",target=/fixtures", "--health-cmd", "exit 0", "--health-interval", "1s", "-e", "READER_FIXTURE_PRIVATE=must-not-leak", "--entrypoint", "sh", image, "-c", "chmod 777 /fixtures; exec sleep 600")
	dir := t.TempDir()
	arch := run("version", "--format", "{{.Server.Arch}}")
	build := func(source, target string) {
		t.Helper()
		cmd := exec.Command("go", "build", "-trimpath", "-o", filepath.Join(dir, target), source)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture build: %s", out)
		}
		run("cp", filepath.Join(dir, target), seed+":/fixtures/"+target)
		run("exec", seed, "chmod", "755", "/fixtures/"+target)
	}
	build(".", "proxy")
	// Model systemd socket activation without adding a test mode to the broker.
	helper := `package main
import("net";"os";"strconv";"syscall")
func main(){l,e:=net.Listen("unix","/fixtures/proxy.sock");if e!=nil{panic(e)};if e=os.Chmod("/fixtures/proxy.sock",0666);e!=nil{panic(e)};f,e:=l.(*net.UnixListener).File();if e!=nil{panic(e)};if e=syscall.Dup3(int(f.Fd()),3,0);e!=nil{panic(e)};os.Setenv("LISTEN_FDS","1");os.Setenv("LISTEN_PID",strconv.Itoa(os.Getpid()));if e=syscall.Exec("/fixtures/proxy",[]string{"proxy","serve"},os.Environ());e!=nil{panic(e)}}`
	if err := os.WriteFile(filepath.Join(dir, "activate.go"), []byte(helper), 0600); err != nil {
		t.Fatal(err)
	}
	build(filepath.Join(dir, "activate.go"), "activate")
	config, err := os.ReadFile("docker.conf")
	if err != nil {
		t.Fatal(err)
	}
	config = []byte(strings.ReplaceAll(strings.ReplaceAll(string(config), socketPath, "/fixtures/proxy.sock"), "update_every: 10", "update_every: 1"))
	if err := os.WriteFile(filepath.Join(dir, "docker.conf"), config, 0600); err != nil {
		t.Fatal(err)
	}
	run("cp", filepath.Join(dir, "docker.conf"), seed+":/fixtures/docker.conf")
	run("exec", seed, "chmod", "644", "/fixtures/docker.conf")
	group := run("run", "--rm", "--network", "none", "--mount", "type=bind,source=/var/run/docker.sock,target=/raw,readonly", "--entrypoint", "stat", image, "-c", "%g", "/raw")
	run("run", "-d", "--name", broker, "--network", "none", "--read-only", "--cap-drop", "ALL", "--user", "10001:10001", "--group-add", group, "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock,readonly", "--mount", "type=volume,source="+volume+",target=/fixtures", "--entrypoint", "/fixtures/activate", image)
	for attempt := 0; attempt < 30; attempt++ {
		if exec.Command("docker", "exec", seed, "test", "-S", "/fixtures/proxy.sock").Run() == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Container inspection must keep names/images and discard credentials/config.
	inspect := run("exec", "--user", "netdata", seed, "curl", "--fail", "--silent", "--unix-socket", "/fixtures/proxy.sock", "http://localhost/containers/"+seedID+"/json")
	if strings.Contains(inspect, "must-not-leak") || strings.Contains(inspect, `"Env"`) || strings.Contains(inspect, `"Mounts"`) || !strings.Contains(inspect, seed) {
		t.Fatal("container inspect projection failed")
	}
	for _, test := range []struct{ method, path string }{{"POST", "/containers/create"}, {"DELETE", "/containers/" + seedID}, {"GET", "/containers/" + seedID + "/logs"}, {"GET", "/containers/" + seedID + "/archive?path=/etc/passwd"}, {"GET", "/secrets"}, {"GET", "/configs"}, {"GET", "/events"}} {
		status := run("exec", "--user", "netdata", seed, "curl", "--silent", "--output", "/dev/null", "--write-out", "%{http_code}", "--unix-socket", "/fixtures/proxy.sock", "-X", test.method, "http://localhost"+test.path)
		if status != "403" {
			t.Fatalf("operation %s %s returned %s", test.method, test.path, status)
		}
	}
	name := run("exec", "--user", "netdata", "-e", "DOCKER_HOST=unix:///fixtures/proxy.sock", seed, "/usr/libexec/netdata/plugins.d/cgroup-name", "/sys/fs/cgroup/docker/"+seedID, "docker_"+seedID)
	if !strings.Contains(name, seed) {
		t.Fatal("actual cgroup-name helper failed")
	}
	// The native host also has Docker's CLI. Exercise the helper's CLI branch,
	// using that host's CLI version without granting it the raw Docker socket.
	run("run", "--rm", "--network", "none", "--mount", "type=volume,source="+volume+",target=/fixtures", "--entrypoint", "cp", cliImage, "/usr/local/bin/docker", "/fixtures/docker")
	name = run("exec", "--user", "netdata", "-e", "PATH=/fixtures:/usr/bin:/bin", "-e", "DOCKER_HOST=unix:///fixtures/proxy.sock", seed, "/usr/libexec/netdata/plugins.d/cgroup-name", "/sys/fs/cgroup/docker/"+seedID, "docker_"+seedID)
	if !strings.Contains(name, seed) {
		t.Fatal("actual cgroup-name helper with Docker CLI failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--tty", "--rm", "--name", collector, "--network", "none", "--read-only", "--cap-drop", "ALL", "--user", "netdata", "-e", "DOCKER_HOST=unix:///var/run/docker.sock", "--mount", "type=volume,source="+volume+",target=/fixtures,readonly", "--mount", "type=volume,source="+volume+",target=/etc/netdata/go.d,readonly", "--entrypoint", "/usr/libexec/netdata/plugins.d/go.d.plugin", image, "-d", "-m", "docker")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = reader
	out, _ := cmd.CombinedOutput()
	reader.Close()
	writer.Close()
	cycles := fixtureCycles(string(out))
	for _, context := range []string{"docker.containers_state", "docker.container_state", "docker.container_health_status"} {
		if cycles[context] < 2 {
			log, _ := os.CreateTemp("", "perry-docker-reader-fixture-*.log")
			path := "unavailable"
			if log != nil {
				log.Write(out)
				path = log.Name()
				log.Close()
			}
			t.Fatalf("actual group-free Netdata lacks repeated %s samples; private fixture log: %s", context, path)
		}
	}
	if !strings.Contains(string(out), seed) {
		t.Fatal("fixture container was not monitored")
	}
	t.Log("Actual unprivileged Netdata Docker collector and cgroup-name passed through filtered Unix API; credentials, writes and unrelated reads denied.")
}

func fixtureCycles(output string) map[string]int {
	contexts := map[string]string{}
	counts := map[string]int{}
	chart := ""
	numeric := false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		switch fields[0] {
		case "CHART":
			if len(fields) > 1 {
				for _, context := range []string{"docker.containers_state", "docker.container_state", "docker.container_health_status"} {
					if strings.Contains(line, context) {
						contexts[strings.Trim(fields[1], "'\"")] = context
					}
				}
			}
		case "BEGIN", "BEGIN2":
			if len(fields) > 1 {
				chart = strings.Trim(fields[1], "'\"")
				numeric = false
			}
		case "SET", "SET2":
			if len(fields) > 2 {
				index := 2
				if fields[index] == "=" && len(fields) > 3 {
					index = 3
				}
				if _, err := strconv.ParseFloat(fields[index], 64); err == nil {
					numeric = true
				}
			}
		case "END", "END2":
			if numeric && contexts[chart] != "" {
				counts[chart]++
			}
			chart = ""
			numeric = false
		}
	}
	result := map[string]int{}
	for chart, count := range counts {
		result[contexts[chart]] = max(result[contexts[chart]], count)
	}
	return result
}
