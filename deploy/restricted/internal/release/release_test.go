package release

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestInfrastructureArguments(t *testing.T) {
	digest := strings.Repeat("a", 64)
	valid := []string{digest}
	if !validArgs(valid) || !reflect.DeepEqual(SSHArgs("infrastructure "+digest), valid) {
		t.Fatal("rejected the valid fingerprint")
	}
	for _, args := range [][]string{
		nil, {digest, "extra"}, {"../../tmp"}, {digest + ";id"}, {digest + "\n"},
		{strings.ToUpper(digest)}, {"sha256:" + digest},
	} {
		if validArgs(args) || SSHArgs("infrastructure "+strings.Join(args, " ")) != nil {
			t.Errorf("accepted invalid arguments %q", args)
		}
	}
	for _, command := range []string{"", "id", "docker system dial-stdio", "internal-sftp"} {
		if SSHArgs(command) != nil {
			t.Errorf("accepted SSH command %q", command)
		}
	}
}

func TestFingerprintMatchesPython(t *testing.T) {
	root := testSnapshot(t)
	// Golden value from the Python broker for these paths and raw file bytes.
	const want = "235d28c360e2fd5403adc3fe20fb4fcf9480321aafd281c1d60c7b57440ea8f3"
	if got, err := Fingerprint(root); err != nil || got != want {
		t.Fatalf("fingerprint = %q, %v, want %s", got, err, want)
	}
	for _, name := range snapshotFiles {
		path := filepath.Join(root, name)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, "modified", 0o600)
		if got, err := Fingerprint(root); err != nil || got == want {
			t.Fatalf("changed %s did not change fingerprint: %s, %v", name, got, err)
		}
		writeFile(t, path, string(original), 0o600)
	}
}

func TestACMEEnv(t *testing.T) {
	const expected = "ACME_EMAIL=admin+proxy@example.test"
	input := `["UNRELATED=value","` + expected + `","ACME_EMAIL=second@example.test"]`
	if got, err := ACMEEnv(strings.NewReader(input)); err != nil || got != expected {
		t.Fatalf("ACMEEnv = %q, %v", got, err)
	}
	for _, input := range []string{
		`null`, `{"ACME_EMAIL":"admin@example.test"}`, `["UNRELATED=value"]`,
		`["ACME_EMAIL="]`, `["ACME_EMAIL=$(id)"]`, `["ACME_EMAIL=admin@example.test;id"]`,
		`["ACME_EMAIL=admin@example.test\nPATH=/tmp"]`, `["ACME_EMAIL=admin@example.test"][]`,
	} {
		if _, err := ACMEEnv(strings.NewReader(input)); err == nil {
			t.Errorf("accepted invalid Docker environment %q", input)
		}
	}
}

func TestDeployRequiresFingerprintAndExclusiveLock(t *testing.T) {
	root := testSnapshot(t)
	lockPath := filepath.Join(root, "release.lock")
	writeFile(t, filepath.Join(root, "deployment.env"), "touch sourced; exit 0\n", 0o600)

	if err := deploy(root, lockPath, strings.Repeat("a", 64), time.Second, io.Discard, io.Discard); err == nil {
		t.Fatal("accepted mismatched fingerprint")
	}
	if _, err := os.Stat(filepath.Join(root, "sourced")); !os.IsNotExist(err) {
		t.Fatalf("sourced deployment.env with mismatched fingerprint: %v", err)
	}

	digest, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(lockPath, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if err := deploy(root, lockPath, digest, time.Second, io.Discard, io.Discard); err != syscall.EWOULDBLOCK {
		t.Fatalf("wanted an exclusive lock error, got %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := deploy(root, lockPath, digest, time.Second, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "sourced")); err != nil {
		t.Fatal(err)
	}
}

func TestDeployTimesOutAndReleasesLock(t *testing.T) {
	root := testSnapshot(t)
	lockPath := filepath.Join(root, "release.lock")
	digest, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(root, "deployment.env")
	writeFile(t, env, "touch started; (sleep 0.5; touch survived) & wait; exit 0\n", 0o600)
	if err := deploy(root, lockPath, digest, 200*time.Millisecond, io.Discard, io.Discard); err == nil {
		t.Fatal("deployment should time out")
	}
	if _, err := os.Stat(filepath.Join(root, "started")); err != nil {
		t.Fatal("deployment fixture did not start")
	}
	writeFile(t, env, "exit 0\n", 0o600)
	if err := deploy(root, lockPath, digest, time.Second, io.Discard, io.Discard); err != nil {
		t.Fatalf("could not retry after timeout: %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "survived")); !os.IsNotExist(err) {
		t.Fatalf("timed-out deployment left a child running: %v", err)
	}
}

func testSnapshot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range map[string]string{
		"docker-compose.stack.yaml": "compose\r\n",
		"postgres/pg_hba.conf":      "hba\x00\xff",
		"docker-proxy/haproxy.cfg":  "proxy\n",
	} {
		path := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, content, 0o600)
	}
	return root
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
