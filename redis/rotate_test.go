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

func TestRotationOverlapsAndRetiresOneUsersPassword(t *testing.T) {
	root := t.TempDir()
	initial, rotated, retired := filepath.Join(root, "initial"), filepath.Join(root, "rotated"), filepath.Join(root, "retired")
	if err := credentials(initial); err != nil {
		t.Fatal(err)
	}
	acl := filepath.Join(initial, "redis_users_v1")
	originalURL := filepath.Join(initial, "im_listening_redis_url_v2")
	if err := rotate("im_listening", acl, originalURL, rotated, false); err != nil {
		t.Fatal(err)
	}
	if err := rotate("im_listening", filepath.Join(rotated, "users.acl"), filepath.Join(rotated, "url"), retired, true); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(acl)
	during, _ := os.ReadFile(filepath.Join(rotated, "users.acl"))
	after, _ := os.ReadFile(filepath.Join(retired, "users.acl"))
	oldURLBytes, _ := os.ReadFile(originalURL)
	newURLBytes, _ := os.ReadFile(filepath.Join(rotated, "url"))
	oldURL, _ := url.Parse(string(oldURLBytes))
	newURL, _ := url.Parse(string(newURLBytes))
	oldPassword, _ := oldURL.User.Password()
	newPassword, _ := newURL.User.Password()
	oldHash := fmt.Sprintf("#%x", sha256.Sum256([]byte(oldPassword)))
	newHash := fmt.Sprintf("#%x", sha256.Sum256([]byte(newPassword)))
	if oldPassword == newPassword || !strings.Contains(string(during), oldHash) || !strings.Contains(string(during), newHash) || strings.Contains(string(after), oldHash) || !strings.Contains(string(after), newHash) {
		t.Fatal("rotation did not overlap and retire credentials")
	}
	for _, line := range strings.Split(string(before), "\n") {
		if !strings.HasPrefix(line, "user im_listening ") && (!strings.Contains(string(during), line) || !strings.Contains(string(after), line)) {
			t.Fatal("rotation changed another user")
		}
	}
}
