package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func fakeCutoverFlow(actions *[]string) cutoverFlow {
	action := func(name string) func() error {
		return func() error { *actions = append(*actions, name); return nil }
	}
	return cutoverFlow{
		Prepare: action("prepare"), Install: action("install"), Finalize: action("finalize"), Verify: action("verify"),
		Rollback: action("rollback"), Baseline: action("baseline"), Archive: action("archive"), Policies: action("policies"), Finish: action("finish"),
		Cutover: func(app application) error { *actions = append(*actions, "cutover/"+app.Name); return nil },
	}
}

func approvals(steps []cutoverStep) string {
	var answers strings.Builder
	for _, step := range steps {
		if step.Check != "" {
			fmt.Fprintln(&answers, "pass "+step.Name)
		}
	}
	return answers.String()
}

func TestGuidedCutoverChecksAppsBeforeContinuingAndExercisesRollback(t *testing.T) {
	var actions []string
	f := fakeCutoverFlow(&actions)
	steps := cutoverSteps(f)
	p := cutoverProgress{Version: 1}
	var saved cutoverProgress
	save := func() error {
		saved = p
		return nil
	}
	output := new(bytes.Buffer)
	if err := executeCutover(context.Background(), &p, steps, save, bufio.NewReader(strings.NewReader(approvals(steps))), output, f.Rollback); err != nil {
		t.Fatal(err)
	}
	want := []string{"prepare", "cutover/fleet", "cutover/chinwag", "cutover/vita-track", "cutover/im-listening", "install", "finalize", "verify", "rollback", "baseline", "archive", "prepare", "cutover/fleet", "cutover/chinwag", "cutover/vita-track", "cutover/im-listening", "install", "finalize", "verify", "policies", "finish"}
	if !slices.Equal(actions, want) || saved.Pending || saved.Next != len(steps) || len(saved.Events) != len(steps) {
		t.Fatalf("incomplete or out-of-order cutover: %v progress=%+v", actions, saved)
	}
	for _, pass := range []string{"drill", "final"} {
		for _, app := range apps {
			if !strings.Contains(output.String(), "pass "+pass+"/check/"+app.Name) {
				t.Fatal("did not request the named application check")
			}
		}
	}
}

func TestGuidedPromptStopAndEOFNeverAdvanceAndResumeDoesNotRepeatCutover(t *testing.T) {
	for _, answer := range []string{"", "pass drill/check/fleet", "stop\n", "pass wrong-app\n", "\n"} {
		t.Run(fmt.Sprintf("input=%q", answer), func(t *testing.T) {
			var actions []string
			f := fakeCutoverFlow(&actions)
			steps := cutoverSteps(f)
			p := cutoverProgress{Version: 1}
			var data []byte
			save := func() error { var err error; data, err = json.Marshal(p); return err }
			err := executeCutover(context.Background(), &p, steps, save, bufio.NewReader(strings.NewReader(answer)), io.Discard, f.Rollback)
			if !errors.Is(err, errPaused) || !slices.Equal(actions, []string{"prepare", "cutover/fleet"}) {
				t.Fatalf("continued without operator acceptance: actions=%v err=%v", actions, err)
			}
			if err := json.Unmarshal(data, &p); err != nil || p.Next != 2 || !p.Pending {
				t.Fatalf("did not persist the pending Fleet check: %+v %v", p, err)
			}
			err = executeCutover(context.Background(), &p, steps, save, bufio.NewReader(strings.NewReader("pass drill/check/fleet\nstop\n")), io.Discard, f.Rollback)
			if !errors.Is(err, errPaused) || !slices.Equal(actions, []string{"prepare", "cutover/fleet", "cutover/chinwag"}) {
				t.Fatalf("resume repeated a mutation or skipped the next app's check: %v %v", actions, err)
			}
		})
	}
}

func TestGuidedFailureCannotBeOverriddenByPass(t *testing.T) {
	for _, choice := range []string{"stop\n", "rollback\n"} {
		t.Run(choice, func(t *testing.T) {
			var actions []string
			f := fakeCutoverFlow(&actions)
			f.Cutover = func(app application) error {
				actions = append(actions, "failed/"+app.Name)
				return errors.New("probe failed")
			}
			steps := cutoverSteps(f)
			p := cutoverProgress{Version: 1}
			err := executeCutover(context.Background(), &p, steps, func() error { return nil }, bufio.NewReader(strings.NewReader("pass drill/cutover/fleet\n"+choice)), io.Discard, f.Rollback)
			if err == nil || p.Next != 1 || !p.Pending || slices.Contains(actions, "policies") || slices.Contains(actions, "install") {
				t.Fatalf("failed probe was bypassed: actions=%v progress=%+v err=%v", actions, p, err)
			}
			if p.RolledBack != (choice == "rollback\n") || slices.Contains(actions, "rollback") != p.RolledBack {
				t.Fatalf("rollback selection did not control restoration: %v %+v", actions, p)
			}
		})
	}
}

func TestGuidedWriteFailurePreventsMutationAndCancellationPauses(t *testing.T) {
	var actions []string
	f := fakeCutoverFlow(&actions)
	p := cutoverProgress{Version: 1}
	err := executeCutover(context.Background(), &p, cutoverSteps(f), func() error { return errors.New("disk full") }, bufio.NewReader(strings.NewReader("")), io.Discard, f.Rollback)
	if err == nil || len(actions) != 0 {
		t.Fatal("mutated services without retained intent")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = executeCutover(ctx, &p, cutoverSteps(f), func() error { return nil }, bufio.NewReader(strings.NewReader("")), io.Discard, f.Rollback)
	if !errors.Is(err, errPaused) || len(actions) != 0 {
		t.Fatal("continued after cancellation")
	}
}

func TestGuidedProgressBindsResumeToReviewedInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	b := cutoverBundle{Dir: "/reviewed", Fingerprint: strings.Repeat("a", 64), Manifest: strings.Repeat("b", 64)}
	p, exists, err := loadCutoverProgress(path, b)
	if err != nil || exists || p.Version != 1 {
		t.Fatal("fresh progress was not initialized")
	}
	data, _ := json.Marshal(p)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := loadCutoverProgress(path, b); err != nil || !exists {
		t.Fatal("matching progress was not readable")
	}
	for _, changed := range []cutoverBundle{
		{Dir: "/other", Fingerprint: b.Fingerprint, Manifest: b.Manifest},
		{Dir: b.Dir, Fingerprint: strings.Repeat("c", 64), Manifest: b.Manifest},
		{Dir: b.Dir, Fingerprint: b.Fingerprint, Manifest: strings.Repeat("c", 64)},
	} {
		if _, _, err := loadCutoverProgress(path, changed); err == nil {
			t.Fatal("adopted progress for different inputs")
		}
	}
}

func TestBundleChecksumsRejectMissingTamperedAndEscapingInputs(t *testing.T) {
	dir := t.TempDir()
	files := []string{"perry-network-admin", "perry-infra-isolation-release", "perry-isolation-redis-admin", "perry-isolation-metrics-admin", "checkout/deploy/install.sh", "source.tar.gz"}
	var manifest strings.Builder
	for _, name := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(name))
		fmt.Fprintf(&manifest, "%x  %s\n", digest, name)
	}
	good := manifest.String()
	if err := checkBundleFiles(dir, good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.SplitN(good, "\n", 2)[1], good + strings.Split(good, "\n")[0] + "\n", strings.Replace(good, "source.tar.gz", "../source.tar.gz", 1), strings.Replace(good, "source.tar.gz", "/source.tar.gz", 1)} {
		if checkBundleFiles(dir, bad) == nil {
			t.Fatal("accepted missing, duplicate or escaping bundle input")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, files[0]), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if checkBundleFiles(dir, good) == nil {
		t.Fatal("accepted a stale binary")
	}
	if err := os.Remove(filepath.Join(dir, files[0])); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(files[1], filepath.Join(dir, files[0])); err != nil {
		t.Fatal(err)
	}
	if checkBundleFiles(dir, good) == nil {
		t.Fatal("accepted a symlink input")
	}
}

func TestBundleRequiresTheBuildersExpectedFingerprint(t *testing.T) {
	dir := t.TempDir()
	expected := strings.Repeat("a", 64)
	files := map[string]string{
		"perry-network-admin":           "fixture",
		"perry-infra-isolation-release": "#!/bin/sh\nprintf '%s\\n' '" + expected + "'\n",
		"perry-isolation-redis-admin":   "fixture",
		"perry-isolation-metrics-admin": "fixture",
		"checkout/deploy/install.sh":    "fixture",
		"source.tar.gz":                 "fixture",
	}
	var manifest strings.Builder
	for name, contents := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0700); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(contents))
		fmt.Fprintf(&manifest, "%x  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(manifest.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkBundle(dir, expected); err != nil {
		t.Fatal(err)
	}
	if _, err := checkBundle(dir, strings.Repeat("b", 64)); err == nil {
		t.Fatal("trusted the VPS's digest instead of the operator's expected fingerprint")
	}
}

func TestGuidedFailedRollbackDoesNotClaimRestoration(t *testing.T) {
	var actions []string
	f := fakeCutoverFlow(&actions)
	p := cutoverProgress{Version: 1}
	err := executeCutover(context.Background(), &p, cutoverSteps(f), func() error { return nil }, bufio.NewReader(strings.NewReader("rollback\n")), io.Discard, func() error { return errors.New("retired network") })
	if err == nil || p.RolledBack || slices.Contains(actions, "policies") || slices.Contains(actions, "finish") {
		t.Fatal("failed recovery was reported as restored or accepted")
	}
}

func TestReleasePolicyMatchesPortfolioWireFormatAndPreservesSpec(t *testing.T) {
	var s service
	if err := decode([]byte(`{"ID":"service-id","Spec":{"Name":"app","Labels":{"routing":"reviewed"},"TaskTemplate":{"ForceUpdate":9007199254740993,"ContainerSpec":{"Image":"repo@sha256:abc","Secrets":[{"SecretID":"retained-id"}]},"Networks":[{"Target":"private-overlay"}]}}}`), &s); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(s)
	got, err := releasePolicy(s)
	if err != nil {
		t.Fatal(err)
	}
	// Canonical JSON is also what deploy/portfolio's administrator recorder hashes.
	wire := "service-id\n" + `{"Labels":{"routing":"reviewed"},"Name":"app","TaskTemplate":{"ContainerSpec":{"Secrets":[{"SecretID":"retained-id"}]},"ForceUpdate":9007199254740993,"Networks":[{"Target":"private-overlay"}]}}`
	digest := sha256.Sum256([]byte(wire))
	if got != hex.EncodeToString(digest[:]) {
		t.Fatalf("incompatible policy: %s", got)
	}
	after, _ := json.Marshal(s)
	if !bytes.Equal(before, after) {
		t.Fatal("policy recording mutated the retained service")
	}
	containerSpec(s)["Image"] = "other-image"
	if changed, _ := releasePolicy(s); changed != got {
		t.Fatal("image change altered image-only policy")
	}
	task(s)["Networks"] = []any{map[string]any{"Target": "unreviewed"}}
	if changed, _ := releasePolicy(s); changed == got {
		t.Fatal("network change did not alter policy")
	}
}

func TestReleaseLocksRemainHeldWhileOperatorChecksApp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.lock")
	if err := lockFiles([]string{path}, func() error {
		other, err := os.OpenFile(path, os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		defer other.Close()
		if err := syscall.Flock(int(other.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			t.Fatal("release lock was available during the operator check")
		}
		return confirmCutover(bufio.NewReader(strings.NewReader("pass check/fleet\n")), io.Discard, cutoverStep{Name: "check/fleet", Check: "Check Fleet"}, false)
	}); err != nil {
		t.Fatal(err)
	}
	if err := lockFiles([]string{path}, func() error { return nil }); err != nil {
		t.Fatal("release lock was not released after the flow")
	}
}
