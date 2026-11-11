package main

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// test.sh supplies a disposable server and credentials. This checks the exact
// production proof Lua and AOF handover, rather than a separate approximation.
func TestAOFReplayWithPreservationProof(t *testing.T) {
	container := os.Getenv("REDIS_TEST_CONTAINER")
	if container == "" {
		t.Skip("run through redis/test.sh with disposable fixtures")
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("REDIS_TEST_SECRETS"), "redis_admin_url_v1"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(string(data))
	if err != nil {
		t.Fatal("invalid test administrator URL")
	}
	password, _ := u.User.Password()
	eval := func(script string) ([]byte, error) {
		cmd := exec.Command("docker", "exec", "-i", container, "sh", "-c",
			`IFS= read -r REDISCLI_AUTH; export REDISCLI_AUTH; exec redis-cli --user admin --json EVAL "$1" 0`, "--", script)
		cmd.Stdin = strings.NewReader(password + "\n")
		return cmd.Output()
	}
	if out, err := eval(`return redis.call('SET', 'im-listening:sess:persistence-test', 'session-contents', 'EX', 300)`); err != nil || !bytes.Contains(out, []byte("OK")) {
		t.Fatal("could not seed disposable session")
	}
	beforeJSON, err := eval(proofScript)
	if err != nil {
		t.Fatal("could not record preservation proof")
	}
	before, err := parseProof(beforeJSON)
	if err != nil || len(before) == 0 {
		t.Fatal("could not parse nonempty preservation proof")
	}
	if err := exec.Command("docker", "restart", container).Run(); err != nil {
		t.Fatal("could not restart disposable Redis")
	}
	var after []proofRow
	for attempt := 0; attempt < 30; attempt++ {
		if data, err := eval(proofScript); err == nil {
			if rows, err := parseProof(data); err == nil {
				after = rows
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := preserved(before, after, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
}
