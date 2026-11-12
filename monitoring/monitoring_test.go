package monitoring

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Reuse the test executable as deterministic command stubs. No host service,
// socket inspection or privileged command is used by the runtime tests.
func TestMain(m *testing.M) {
	if os.Getenv("MONITORING_TEST_HELPER") == "1" {
		os.Exit(mockCommand(filepath.Base(os.Args[0]), os.Args[1:]))
	}
	os.Exit(m.Run())
}

func mockCommand(name string, args []string) int {
	switch name {
	case "id":
		fmt.Println("0")
	case "netdatacli":
		if len(args) != 1 {
			return 2
		}
		switch args[0] {
		case "ping":
			counter := os.Getenv("MOCK_PING_COUNTER")
			data, err := os.ReadFile(counter)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return 2
			}
			count := 0
			if err == nil {
				count, err = strconv.Atoi(string(data))
				if err != nil {
					return 2
				}
			}
			failures, err := strconv.Atoi(os.Getenv("MOCK_PING_FAILURES"))
			if err != nil || os.WriteFile(counter, []byte(strconv.Itoa(count+1)), 0o600) != nil {
				return 2
			}
			if count < failures {
				return 1
			}
		case "dumpconfig":
			fmt.Print(os.Getenv("MOCK_RUNTIME"))
		case "aclk-state":
			fmt.Print(os.Getenv("MOCK_CLOUD"))
		default:
			return 2
		}
	case "ss":
		fmt.Print(os.Getenv("MOCK_LISTENER"))
		if os.Getenv("MOCK_SOCKET_ERROR") == "1" {
			return 1
		}
	case "systemctl", "free", "df", "sleep":
		// Successful no-ops; readiness retries must not actually sleep.
	default:
		fmt.Fprintf(os.Stderr, "unexpected mock command %q\n", name)
		return 2
	}
	return 0
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFixture(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runShell(t *testing.T, env []string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", args...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("shell check timed out: %v\n%s", ctx.Err(), output)
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("could not run shell check: %v", err)
	}
	return string(output), err
}

func TestStartupGuard(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		allowed bool
	}{
		{"managed", readFixture(t, "netdata.conf"), true},
		{"formatted", "# header\n [web] \n  mode = none \n[db]\n mode = dbengine\n", true},
		{"empty", "", false},
		{"missing_mode", "[web]\n", false},
		{"public_mode", "[web]\nmode = static-threaded\n", false},
		{"commented_mode", "[web]\n# mode = none\n", false},
		{"wrong_section", "[db]\nmode = none\n", false},
		{"public_override", "[web]\nmode = none\nmode = static-threaded\n", false},
		{"private_override", "[web]\nmode = static-threaded\nmode = none\n", false},
		{"duplicate_section", "[web]\nmode = none\n[web]\nmode = none\n", false},
		{"duplicate_mode", "[web]\nmode = none\nmode = none\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "netdata.conf")
			writeFixture(t, config, tc.config)
			checkConfig(t, config, tc.allowed)
		})
	}
	t.Run("missing_file", func(t *testing.T) {
		checkConfig(t, filepath.Join(t.TempDir(), "missing.conf"), false)
	})
}

func checkConfig(t *testing.T, config string, allowed bool) {
	t.Helper()
	for _, args := range [][]string{
		{"check-config.sh", config},
		{"verify.sh", "--config-only", config},
	} {
		output, err := runShell(t, nil, args...)
		if (err == nil) != allowed {
			t.Errorf("%s: allowed = %v, want %v\n%s", args[0], err == nil, allowed, output)
		}
	}
}

func TestRuntimeVerification(t *testing.T) {
	cases := []struct {
		name         string
		runtime      string
		listener     string
		cloud        string
		socketError  bool
		local        bool
		pingFailures int
		allowed      bool
		message      string
	}{
		{name: "cloud_online", allowed: true, message: "PASS: Cloud claimed and online."},
		{name: "waits_for_control_pipe", pingFailures: 2, allowed: true},
		{name: "control_pipe_timeout", pingFailures: 100, message: "did not become ready"},
		{name: "local_before_claim", cloud: "Claimed: No\n", local: true, allowed: true},
		{name: "stale_public_runtime", runtime: "[web]\nmode = static-threaded\n", message: "Refusing to start"},
		{name: "public_listener", listener: "LISTEN 0 128 0.0.0.0:19999", message: "port 19999 is listening"},
		{name: "socket_inspection_failed", socketError: true},
		{name: "unclaimed", cloud: "Claimed: No\nOnline: Yes\n", message: "agent not claimed/online"},
		{name: "offline", cloud: "Claimed: Yes\nOnline: No\n", message: "agent not claimed/online"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			config := filepath.Join(root, "netdata.conf")
			writeFixture(t, config, "[web]\nmode = none\n")
			writeFixture(t, filepath.Join(root, "check-config.sh"), readFixture(t, "check-config.sh"))
			script := readFixture(t, "verify.sh")
			// Change only host paths and command lookup in a disposable copy.
			// Fail if these anchors drift, before any system command could run.
			for _, replacement := range [][2]string{
				{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "PATH=" + shellQuote(root) + ":$PATH"},
				{"/etc/netdata/netdata.conf", shellQuote(config)},
			} {
				if strings.Count(script, replacement[0]) != 1 {
					t.Fatalf("expected exactly one fixture anchor %q", replacement[0])
				}
				script = strings.Replace(script, replacement[0], replacement[1], 1)
			}
			verify := filepath.Join(root, "verify.sh")
			writeFixture(t, verify, script)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"id", "netdatacli", "ss", "systemctl", "free", "df", "sleep"} {
				if err := os.Symlink(executable, filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.runtime == "" {
				tc.runtime = "[web]\nmode = none\n"
			}
			if tc.cloud == "" {
				tc.cloud = "Claimed: Yes\nOnline: Yes\n"
			}
			counter := filepath.Join(root, "ping-counter")
			socketError := "0"
			if tc.socketError {
				socketError = "1"
			}
			env := []string{
				"MONITORING_TEST_HELPER=1",
				"MOCK_RUNTIME=" + tc.runtime,
				"MOCK_LISTENER=" + tc.listener,
				"MOCK_CLOUD=" + tc.cloud,
				"MOCK_SOCKET_ERROR=" + socketError,
				"MOCK_PING_COUNTER=" + counter,
				"MOCK_PING_FAILURES=" + strconv.Itoa(tc.pingFailures),
				// Reexecuted race-enabled helpers need no exit delay.
				"GORACE=" + os.Getenv("GORACE") + " atexit_sleep_ms=0",
			}
			args := []string{verify}
			if tc.local {
				args = append(args, "--local-only")
			}
			output, err := runShell(t, env, args...)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed = %v, want %v\n%s", err == nil, tc.allowed, output)
			}
			if tc.message != "" && !strings.Contains(output, tc.message) {
				t.Errorf("expected %q in output:\n%s", tc.message, output)
			}
			attempts := min(tc.pingFailures+1, 30)
			if got := readFixture(t, counter); got != strconv.Itoa(attempts) {
				t.Errorf("ping attempts = %s, want %d", got, attempts)
			}
		})
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
