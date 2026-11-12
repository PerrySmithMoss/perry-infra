package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func recoveryFixture(t *testing.T) service {
	t.Helper()
	var s service
	if err := decode([]byte(`{"ID":"client-id","Version":{"Index":42},"Spec":{"Name":"im-listening_api","Mode":{"Replicated":{"Replicas":1}},"Labels":{"traefik.swarm.network":"portfolio_network"},"TaskTemplate":{"ContainerSpec":{"Image":"repo@sha256:retained","Secrets":[{"SecretID":"retained-id"}],"Mounts":[{"Source":"retained-volume"}],"ReadOnly":true},"Networks":[{"Target":"original-id"}]}}}`), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestClientRecoveryRestoresExactSpecAndUsesFreshVersion(t *testing.T) {
	original := recoveryFixture(t)
	before, _ := json.Marshal(original)
	var current service
	var replicas []string
	ready := false
	err := restartCompletedClient(original, func(name string) (service, error) {
		if name != "im-listening_api" {
			t.Fatal("wrong service inspected")
		}
		current.Version.Index = 43
		return current, nil
	}, func(s service) error {
		value := s.Spec["Mode"].(map[string]any)["Replicated"].(map[string]any)["Replicas"]
		replicas = append(replicas, fmt.Sprint(value))
		current = s
		if len(replicas) == 2 {
			if !sameSpec(original, s) || s.Version.Index != 43 {
				t.Fatal("recovery changed the final spec or reused a stale version")
			}
		}
		return nil
	}, func(s service) error {
		if fmt.Sprint(replicas) != "[0]" || !sameSpec(current, s) {
			t.Fatal("restored replicas before observing scale-down")
		}
		return nil
	}, func(s service) error {
		ready = true
		if !sameSpec(original, s) {
			t.Fatal("waited for an unreviewed spec")
		}
		return nil
	})
	if err != nil || !ready || fmt.Sprint(replicas) != "[0 1]" {
		t.Fatalf("recovery=%v ready=%v replicas=%v", err, ready, replicas)
	}
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("mutated the retained original specification")
	}
}

func TestClientRecoveryDoesNotOverwriteConcurrentChanges(t *testing.T) {
	original := recoveryFixture(t)
	posts := 0
	err := restartCompletedClient(original, func(string) (service, error) {
		changed := recoveryFixture(t)
		containerSpec(changed)["Image"] = "unreviewed-image"
		return changed, nil
	}, func(service) error { posts++; return nil }, func(service) error { return nil }, func(service) error { t.Fatal("accepted a changed service"); return nil })
	if err == nil || posts != 1 {
		t.Fatal("overwrote an unrelated change during recovery")
	}
	err = restartCompletedClient(original, func(string) (service, error) { t.Fatal("inspected after rejected update"); return service{}, nil }, func(service) error { return errors.New("version conflict") }, func(service) error { return nil }, func(service) error { t.Fatal("accepted rejected update"); return nil })
	if err == nil {
		t.Fatal("ignored a rejected zero-replica update")
	}
}

func TestClientRecoveryWaitsForTaskRemovalAndRestoresReplicasOnTimeout(t *testing.T) {
	original := recoveryFixture(t)
	var current service
	posts := 0
	waitErr := errors.New("retired task still exists")
	err := restartCompletedClient(original, func(string) (service, error) {
		current.Version.Index = 43
		return current, nil
	}, func(s service) error { current = s; posts++; return nil }, func(service) error {
		if posts != 1 {
			t.Fatal("one replica restored before retired tasks were checked")
		}
		return waitErr
	}, func(service) error { t.Fatal("accepted failed scale-down"); return nil })
	if !errors.Is(err, waitErr) || posts != 2 || !sameSpec(original, current) {
		t.Fatalf("failed wait left a changed replica count: posts=%d error=%v", posts, err)
	}
}

func TestStoppedWaitRequiresTheCompletedTaskToDisappear(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		want := recoveryFixture(t)
		want.Spec["Mode"].(map[string]any)["Replicated"].(map[string]any)["Replicas"] = json.Number("0")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		checks := 0
		start := time.Now()
		err := waitForStopped(ctx, want, func(string) (service, error) { return want, nil }, func(service) (bool, error) {
			checks++
			return checks == 4, nil // Zero running containers alone is not a scale-down acknowledgement.
		})
		if err != nil || checks != 4 || time.Since(start) != 3*time.Second {
			t.Fatalf("stopped early: checks=%d elapsed=%s error=%v", checks, time.Since(start), err)
		}
		err = waitForStopped(ctx, want, func(string) (service, error) { return want, nil }, func(service) (bool, error) { return false, nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ignored stop deadline: %v", err)
		}
	})
}

func TestRetiredClientAllowsHistoryOnlyAfterItsOwnTaskIsRemoved(t *testing.T) {
	for _, tc := range []struct {
		name, tasks, live, containers string
		want                          bool
	}{
		{"completed task retained", "completed\nhistorical\n", "", "", false},
		{"historical task only", "historical\n", "", "", true},
		{"no history", "", "", "", true},
		{"unexpected desired-running task", "historical\nreplacement\n", "replacement\n", "", false},
		{"container still running", "historical\n", "", "container\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := retiredClientRemoved(recoveryFixture(t), "completed", func(args ...string) ([]byte, error) {
				cmd := strings.Join(args, " ")
				switch cmd {
				case "service ps -q --no-trunc client-id":
					return []byte(tc.tasks), nil
				case "service ps -q --no-trunc --filter desired-state=running client-id":
					return []byte(tc.live), nil
				case "ps -q --filter label=com.docker.swarm.service.name=im-listening_api":
					return []byte(tc.containers), nil
				default:
					t.Fatalf("unexpected diagnostic: %s", cmd)
					return nil, nil
				}
			})
			if err != nil || got != tc.want {
				t.Fatalf("removed=%v error=%v", got, err)
			}
		})
	}
	if _, err := retiredClientRemoved(recoveryFixture(t), "", func(...string) ([]byte, error) {
		t.Fatal("ran with no completed task ID")
		return nil, nil
	}); err == nil {
		t.Fatal("accepted missing task ID")
	}
	for failAt := 1; failAt <= 3; failAt++ {
		calls := 0
		wantErr := errors.New("diagnostic unavailable")
		got, err := retiredClientRemoved(recoveryFixture(t), "completed", func(...string) ([]byte, error) {
			calls++
			if calls == failAt {
				return nil, wantErr
			}
			return nil, nil
		})
		if got || !errors.Is(err, wantErr) {
			t.Fatal("accepted a failed scale-down diagnostic")
		}
	}
}

func TestStoppedWaitRejectsConcurrentDrift(t *testing.T) {
	want := recoveryFixture(t)
	err := waitForStopped(context.Background(), want, func(string) (service, error) {
		changed := recoveryFixture(t)
		containerSpec(changed)["Image"] = "changed"
		return changed, nil
	}, func(service) (bool, error) { t.Fatal("checked tasks after drift"); return true, nil })
	if err == nil {
		t.Fatal("accepted service drift")
	}
}

func TestOnlyExplicitCleanCompletionIsRecoverable(t *testing.T) {
	if !cleanCompletion([]byte(`[{"ServiceID":"client-id","DesiredState":"shutdown","Status":{"State":"shutdown","ContainerStatus":{"ExitCode":0}}}]`), "client-id") {
		t.Fatal("rejected a clean task retired by an interrupted scale cycle")
	}
	for _, tc := range []struct {
		id, state, exit string
		want            bool
	}{
		{"client-id", "complete", `,"ContainerStatus":{"ExitCode":0}`, true},
		{"client-id", "shutdown", `,"ContainerStatus":{"ExitCode":0}`, false},
		{"other-id", "complete", `,"ContainerStatus":{"ExitCode":0}`, false},
		{"client-id", "failed", `,"ContainerStatus":{"ExitCode":1}`, false},
		{"client-id", "complete", `,"ContainerStatus":{"ExitCode":1}`, false},
		{"client-id", "complete", "", false},
		{"client-id", "running", `,"ContainerStatus":{"ExitCode":0}`, false},
	} {
		data := fmt.Sprintf(`[{"ServiceID":%q,"Status":{"State":%q%s}}]`, tc.id, tc.state, tc.exit)
		if got := cleanCompletion([]byte(data), "client-id"); got != tc.want {
			t.Fatalf("cleanCompletion(%s)=%v", data, got)
		}
	}
}

func TestClientRecoveryAllowsOnlyTheReviewedNetworkChange(t *testing.T) {
	app := apps[3]
	resolve := func(name string) (network, error) { return network{ID: name + "-id"}, nil }
	original := recoveryFixture(t)
	current := recoveryFixture(t)
	if !allowedClient(original, current, resolve, app) {
		t.Fatal("rejected unchanged original service")
	}
	if err := setNetworksWith(current, desired(app), "api", resolve); err != nil {
		t.Fatal(err)
	}
	current.Spec["Labels"].(map[string]any)["traefik.swarm.network"] = app.Ingress
	if !allowedClient(recoveryFixture(t), current, resolve, app) {
		t.Fatal("rejected reviewed network-only change")
	}
	containerSpec(current)["Image"] = "another-image"
	if allowedClient(recoveryFixture(t), current, resolve, app) {
		t.Fatal("adopted unrelated image change")
	}
}

func TestRecoveryHandoffRequiresUntouchedFirstPreparation(t *testing.T) {
	p := cutoverProgress{Version: 1, Next: 0, Pending: true, Fingerprint: "reviewed"}
	if !recoverablePreparation(p, "reviewed") {
		t.Fatal("rejected paused first preparation")
	}
	for _, change := range []func(*cutoverProgress){
		func(p *cutoverProgress) { p.Next = 1 },
		func(p *cutoverProgress) { p.Pending = false },
		func(p *cutoverProgress) { p.RolledBack = true },
		func(p *cutoverProgress) { p.Version = 2 },
		func(p *cutoverProgress) { p.Fingerprint = "different" },
		func(p *cutoverProgress) { p.Events = []cutoverEvent{{Step: "accepted"}} },
	} {
		bad := p
		change(&bad)
		if recoverablePreparation(bad, "reviewed") {
			t.Fatal("recovery skipped accepted work or changed reviewed inputs")
		}
	}
}

func TestFinalPreparationHandoffRequiresAcceptedArchivedDrill(t *testing.T) {
	p := cutoverProgress{Version: 1, Next: 17, Pending: true, Fingerprint: "reviewed", CheckMode: phaseCheckMode,
		Events: []cutoverEvent{{Step: "drill/restored-check"}, {Step: "drill/archive"}}}
	if !recoverablePreparation(p, "reviewed") {
		t.Fatal("rejected the paused final preparation after accepted rollback drill")
	}
	for _, change := range []func(*cutoverProgress){
		func(p *cutoverProgress) { p.Next = 16 },
		func(p *cutoverProgress) { p.Next = 18 },
		func(p *cutoverProgress) { p.Pending = false },
		func(p *cutoverProgress) { p.RolledBack = true },
		func(p *cutoverProgress) { p.Version++ },
		func(p *cutoverProgress) { p.Fingerprint = "other" },
		func(p *cutoverProgress) { p.CheckMode = "" },
		func(p *cutoverProgress) { p.CheckMode = "unsupported" },
		func(p *cutoverProgress) { p.Events = nil },
		func(p *cutoverProgress) {
			p.Events = []cutoverEvent{{Step: "drill/monitoring"}, {Step: "drill/archive"}}
		},
		func(p *cutoverProgress) {
			p.Events = []cutoverEvent{{Step: "drill/restored-check"}, {Step: "drill/baseline"}}
		},
	} {
		bad := p
		change(&bad)
		if recoverablePreparation(bad, "reviewed") {
			t.Fatalf("accepted incomplete or unrelated recovery: %+v", bad)
		}
	}
	b := cutoverBundle{Dir: "/new-bundle", Manifest: "new-manifest", Fingerprint: "reviewed"}
	updated := preparationProgress(p, b, time.Unix(1, 0))
	if updated.Next != p.Next || updated.Pending != p.Pending || updated.CheckMode != p.CheckMode || updated.Fingerprint != p.Fingerprint ||
		updated.Dir != b.Dir || updated.Manifest != b.Manifest || len(updated.Events) != 3 || len(p.Events) != 2 ||
		updated.Events[0] != p.Events[0] || updated.Events[1] != p.Events[1] || updated.Events[2].Step != "preparation/recovery" {
		t.Fatal("handoff changed accepted evidence, cursor or validation mode")
	}
}

func TestFinalPreparationRequiresUnchangedDrillRollbackInputs(t *testing.T) {
	archive := t.TempDir()
	for _, name := range []string{"rolled-back", "backed-up", "installation.tar.gz"} {
		if err := os.WriteFile(filepath.Join(archive, name), []byte("retained fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	names := append([]string{}, infrastructure...)
	for _, app := range apps {
		names = append(names, app.Service)
	}
	originals := func(name string) (service, error) {
		s := recoveryFixture(t)
		s.Spec["Name"] = name
		return s, nil
	}
	for _, name := range names {
		s, _ := originals(name)
		data, _ := json.Marshal(s)
		if err := os.WriteFile(filepath.Join(archive, name+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyPreparationDrill(archive, originals); err != nil {
		t.Fatal(err)
	}
	if err := verifyPreparationDrill(archive, func(name string) (service, error) {
		s, _ := originals(name)
		containerSpec(s)["Image"] = "unreviewed"
		return s, nil
	}); err == nil {
		t.Fatal("accepted a final inventory differing from the drill baseline")
	}
	if err := os.Remove(filepath.Join(archive, "rolled-back")); err != nil {
		t.Fatal(err)
	}
	if err := verifyPreparationDrill(archive, originals); err == nil {
		t.Fatal("accepted missing drill rollback evidence")
	}
}
