package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var digest = "sha256:" + strings.Repeat("a", 64)
var oldDigest = "sha256:" + strings.Repeat("b", 64)
var revision = strings.Repeat("c", 40)

func TestSSHBoundary(t *testing.T) {
	good := "release " + digest + " " + revision
	for _, command := range []string{"", "sh", "internal-sftp", "docker system dial-stdio", good + "\n", good + " fleet", good + ";id", strings.Replace(good, " ", "\t", 1), "release ghcr.io/other/app@" + digest + " " + revision, strings.Repeat("x", 65536)} {
		if _, err := sshArgs("chinwag-release", command); err == nil {
			t.Errorf("accepted malformed command of length %d", len(command))
		}
	}
	for _, account := range []string{"deploy", "chinwag", "bulkbets-release", "root"} {
		if _, err := sshArgs(account, good); err == nil {
			t.Errorf("accepted unknown account %s", account)
		}
	}
	want := []string{"-n", "--", installed, "release", "fleet", digest, revision}
	if got, err := sshArgs("fleet-release", good); err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestRelease(t *testing.T) {
	p := apps["chinwag"]
	const id = "abcd123456789012345678901"
	for _, mode := range []string{"success", "delayed-status", "delayed-task", "no-op", "no-op-after-rollback", "drift", "service-replaced", "drift-after-pull", "wrong-revision", "pull-failed", "update-timeout", "rolled-back", "stale-task", "two-tasks"} {
		t.Run(mode, func(t *testing.T) {
			s := service{ID: id}
			container := map[string]any{"Image": p.repository + ":known@" + oldDigest, "Secrets": []string{"chinwag_database_url"}}
			s.Spec = map[string]any{"Name": p.service, "TaskTemplate": map[string]any{"ContainerSpec": container, "Networks": []string{"reviewed"}}}
			expected, _, err := s.fingerprint(p)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "no-op" || mode == "no-op-after-rollback" {
				container["Image"] = p.repository + "@" + digest
			}
			if mode == "no-op-after-rollback" {
				s.UpdateStatus.State = "rollback_completed"
			}
			if mode == "drift" {
				container["Secrets"] = []string{"postgres_password"}
			}
			if mode == "service-replaced" {
				s.ID = strings.Repeat("z", 25)
			}
			image := p.repository + "@" + digest
			mutated := false
			postUpdateInspects, taskChecks := 0, 0
			fake := func(timeout time.Duration, args ...string) ([]byte, error) {
				if timeout <= 0 || timeout > 4*time.Minute {
					t.Fatalf("unexpected timeout %v", timeout)
				}
				switch strings.Join(args[:2], " ") {
				case "service inspect":
					if !slices.Equal(args[2:], []string{p.service}) {
						t.Fatalf("wrong service: %v", args)
					}
					if mutated && mode == "delayed-status" {
						postUpdateInspects++
						if postUpdateInspects == 1 {
							s.UpdateStatus.State = "updating"
						} else {
							s.UpdateStatus.State = "completed"
						}
					}
					return json.Marshal([]service{s})
				case "image pull":
					if !slices.Equal(args[2:], []string{image}) {
						t.Fatalf("wrong repository or mutable image: %v", args)
					}
					if mode == "pull-failed" {
						return nil, errors.New("pull failed")
					}
					if mode == "drift-after-pull" {
						s.Spec["Labels"] = map[string]any{"changed": "yes"}
					}
				case "image inspect":
					if mode == "wrong-revision" {
						return []byte("other\n"), nil
					}
					return []byte(revision + "\n"), nil
				case "service update":
					if !slices.Equal(args[2:], []string{"--detach=false", "--no-resolve-image", "--image", image, id}) {
						t.Fatalf("unexpected Swarm mutation: %v", args)
					}
					mutated = true
					if mode == "update-timeout" {
						return nil, errors.New("timeout")
					}
					if mode == "rolled-back" {
						s.UpdateStatus.State = "rollback_completed"
					} else {
						container["Image"] = image
						s.UpdateStatus.State = "completed"
					}
				case "service ps":
					taskChecks++
					switch mode {
					case "delayed-task":
						if taskChecks == 1 {
							return []byte(image + "|Starting 2 seconds ago\n"), nil
						}
					case "stale-task":
						return []byte(p.repository + "@" + oldDigest + "|Running 2 hours ago\n"), nil
					case "two-tasks":
						return []byte(strings.Repeat(image+"|Running 2 seconds ago\n", 2)), nil
					}
					return []byte(image + "|Running 2 seconds ago\n"), nil
				default:
					t.Fatalf("unexpected command %v", args)
				}
				return nil, nil
			}
			err = release(fake, p, expected, []string{digest, revision}, 3, 0)
			if (err == nil) != slices.Contains([]string{"success", "delayed-status", "delayed-task", "no-op", "no-op-after-rollback"}, mode) {
				t.Fatalf("unexpected error: %v", err)
			}
			if mutated != slices.Contains([]string{"success", "delayed-status", "delayed-task", "update-timeout", "rolled-back", "stale-task", "two-tasks"}, mode) {
				t.Fatalf("unexpected mutation status: %v", mutated)
			}
		})
	}
}

func TestConcurrentRelease(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "release.lock")
	if err := withLock(lock, func() error {
		if err := withLock(lock, func() error { t.Fatal("second release acquired held lock"); return nil }); err == nil {
			t.Fatal("missing contention error")
		}
		return errors.New("simulated command failure")
	}); err == nil {
		t.Fatal("missing action error")
	}
	if err := withLock(lock, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}
