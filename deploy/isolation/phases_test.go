package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPhaseCheckpointsRetainEveryMutationAndUseThreeManualGates(t *testing.T) {
	var originalActions, phaseActions []string
	original := fakeCutoverFlow(&originalActions)
	baseline := cutoverSteps(original)
	legacy := cutoverProgress{Version: 1}
	if err := executeCutover(context.Background(), &legacy, baseline, func() error { return nil }, bufio.NewReader(strings.NewReader(approvals(baseline))), io.Discard, original.Rollback); err != nil {
		t.Fatal(err)
	}
	f := fakeCutoverFlow(&phaseActions)
	steps := cutoverStepsForMode(f, phaseCheckMode)
	var gates []string
	for _, step := range steps {
		if step.Check != "" {
			gates = append(gates, step.Name)
		}
	}
	if !slices.Equal(gates, []string{"drill/monitoring", "drill/restored-check", "accept"}) || len(steps) != len(baseline) {
		t.Fatalf("wrong phase gates: %v", gates)
	}
	p := cutoverProgress{Version: 1, CheckMode: phaseCheckMode}
	output := new(bytes.Buffer)
	if err := executeCutover(context.Background(), &p, steps, func() error { return nil }, bufio.NewReader(strings.NewReader(approvals(steps))), output, f.Rollback); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(originalActions, phaseActions) || p.Next != len(steps) || p.Pending {
		t.Fatalf("changed operations: %v", phaseActions)
	}
	deferred := 0
	for _, event := range p.Events {
		if strings.Contains(event.Step, "/check/") || event.Step == "final/monitoring" {
			t.Fatal("deferred walkthrough was recorded as accepted")
		}
		if strings.Contains(event.Step, "/deferred-") {
			deferred++
		}
	}
	if deferred != 9 || strings.Count(output.String(), "Manual checkpoint") != 3 || strings.Contains(output.String(), "pass final/check/") {
		t.Fatalf("did not consolidate walkthroughs: %s", output)
	}
	if !strings.Contains(steps[30].Check, "all five external monitors") || !strings.Contains(steps[30].Check, "Redis") {
		t.Fatal("final acceptance omitted consolidated checks")
	}
}

func TestPhaseFlowDoesNotPublishPoliciesBeforeCombinedAcceptance(t *testing.T) {
	var actions []string
	f := fakeCutoverFlow(&actions)
	steps := cutoverStepsForMode(f, phaseCheckMode)
	p := cutoverProgress{Version: 1, Next: 17, CheckMode: phaseCheckMode}
	err := executeCutover(context.Background(), &p, steps, func() error { return nil }, bufio.NewReader(strings.NewReader("")), io.Discard, f.Rollback)
	if !errors.Is(err, errPaused) || p.Next != 30 || !p.Pending || slices.Contains(actions, "policies") || slices.Contains(actions, "finish") {
		t.Fatalf("accepted before final check: %+v %v %v", p, actions, err)
	}
	want := []string{"prepare", "cutover/fleet", "cutover/chinwag", "cutover/vita-track", "cutover/im-listening", "install", "finalize", "verify"}
	if !slices.Equal(actions, want) {
		t.Fatalf("missing automatic checks: %v", actions)
	}
}

func TestPhaseFlowStopsAtFailedAppWithoutMovingToNextApp(t *testing.T) {
	var actions []string
	f := fakeCutoverFlow(&actions)
	f.Cutover = func(app application) error {
		actions = append(actions, "failed/"+app.Name)
		return errors.New("isolation check failed")
	}
	p := cutoverProgress{Version: 1, CheckMode: phaseCheckMode}
	err := executeCutover(context.Background(), &p, cutoverStepsForMode(f, phaseCheckMode), func() error { return nil }, bufio.NewReader(strings.NewReader("pass accept\nstop\n")), io.Discard, f.Rollback)
	if err == nil || p.Next != 1 || !p.Pending || !slices.Equal(actions, []string{"prepare", "failed/fleet"}) {
		t.Fatalf("advanced after failed app: %v %+v %v", actions, p, err)
	}
}

func TestPhaseAdoptionKeepsCursorAcceptedChecksAndRollbackState(t *testing.T) {
	for _, next := range []int{12, 15} {
		p := cutoverProgress{Version: 1, Dir: "/old", Manifest: "old", Fingerprint: "reviewed", Next: next, Pending: true, Events: []cutoverEvent{{Step: "drill/check/fleet", Time: "old"}, {Step: "drill/verify", Time: "old"}}}
		if !recoverablePhaseCheckpoint(p, "reviewed") {
			t.Fatal("rejected safe manual boundary")
		}
		before, _ := json.Marshal(p)
		b := cutoverBundle{Dir: "/new", Manifest: "new", Fingerprint: "reviewed"}
		updated := phaseProgress(p, b, time.Unix(123, 0))
		if updated.Next != p.Next || updated.Pending != p.Pending || updated.RolledBack != p.RolledBack || updated.Fingerprint != p.Fingerprint || updated.CheckMode != phaseCheckMode || updated.Dir != b.Dir || updated.Manifest != b.Manifest || !reflect.DeepEqual(updated.Events[:len(p.Events)], p.Events) {
			t.Fatalf("lost audit state: %+v", updated)
		}
		after, _ := json.Marshal(p)
		if !bytes.Equal(before, after) {
			t.Fatal("mutated original progress")
		}
		for _, change := range []func(*cutoverProgress){func(p *cutoverProgress) { p.Pending = false }, func(p *cutoverProgress) { p.RolledBack = true }, func(p *cutoverProgress) { p.Version++ }, func(p *cutoverProgress) { p.Fingerprint = "other" }, func(p *cutoverProgress) { p.CheckMode = phaseCheckMode }} {
			bad := p
			change(&bad)
			if recoverablePhaseCheckpoint(bad, "reviewed") {
				t.Fatal("adopted unsafe progress")
			}
		}
	}
	for next := 0; next <= 33; next++ {
		if next == 12 || next == 15 {
			continue
		}
		if recoverablePhaseCheckpoint(cutoverProgress{Version: 1, Next: next, Pending: true, Fingerprint: "reviewed"}, "reviewed") {
			t.Fatalf("adopted non-checkpoint step %d", next)
		}
	}
}

func TestPhaseModeMustBeRecognizedWhenLoadingProgress(t *testing.T) {
	b := cutoverBundle{Dir: "/reviewed", Manifest: "manifest", Fingerprint: "reviewed"}
	path := filepath.Join(t.TempDir(), "progress.json")
	fresh, exists, err := loadCutoverProgress(path, b)
	if err != nil || exists || fresh.CheckMode != phaseCheckMode {
		t.Fatal("new runs did not select phase checkpoints")
	}
	for _, mode := range []string{"", phaseCheckMode, "skip-all-checks"} {
		p := cutoverProgress{Version: 1, Dir: b.Dir, Manifest: b.Manifest, Fingerprint: b.Fingerprint, CheckMode: mode}
		data, _ := json.Marshal(p)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, _, err := loadCutoverProgress(path, b)
		if (err != nil) != (mode == "skip-all-checks") {
			t.Fatalf("incorrect mode handling %q: %v", mode, err)
		}
		if err == nil && loaded.CheckMode != mode {
			t.Fatal("silently changed an existing run's recorded validation mode")
		}
	}
}
