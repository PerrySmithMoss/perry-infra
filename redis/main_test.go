package main

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialsAgreeAndRemainPrivate(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "secrets")
	if err := credentials(directory); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("credentials directory must have mode 0700")
	}
	acl, err := os.ReadFile(filepath.Join(directory, "redis_users_v1"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for file, user := range map[string]string{"redis_admin_url_v1": "admin", "redis_probe_url_v1": "infra_probe", "im_listening_redis_url_v2": "im_listening"} {
		path := filepath.Join(directory, file)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(string(data))
		if err != nil || u.User.Username() != user || u.Host != "perry-infra_redis:6379" || u.Path != "/0" {
			t.Fatal("invalid private Redis connection setting")
		}
		password, ok := u.User.Password()
		if !ok || len(password) != 64 || seen[password] {
			t.Fatal("expected separate random 256-bit passwords")
		}
		seen[password] = true
		digest := sha256.Sum256([]byte(password))
		if !strings.Contains(string(acl), fmt.Sprintf("user %s reset on #%x ", user, digest)) || strings.Contains(string(acl), password) {
			t.Fatal("ACL must contain the matching password hash and no plaintext password")
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("credential file must have mode 0600")
		}
	}
	if err := credentials(directory); err == nil {
		t.Fatal("must refuse to replace existing credentials")
	}
	after, _ := os.ReadFile(filepath.Join(directory, "redis_users_v1"))
	if string(after) != string(acl) {
		t.Fatal("retry changed installed credentials")
	}
}
