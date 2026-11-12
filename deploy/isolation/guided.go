package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const guidedState = "/var/lib/perry-network-cutover"

var errPaused = errors.New("cutover paused; rerun the same command to continue")
var errRollbackRequested = errors.New("administrator requested rollback")

type cutoverBundle struct {
	Dir, Fingerprint, Manifest string
}

// The expected runtime fingerprint comes from the trusted builder/operator,
// never from a digest read off the VPS. Checksums also detect stale bundle files.
func checkBundle(dir, expected string) (cutoverBundle, error) {
	b := cutoverBundle{Dir: dir, Fingerprint: expected}
	if !filepath.IsAbs(dir) {
		return b, errors.New("the cutover bundle must have an absolute path")
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return b, err
	}
	if err := checkBundleFiles(dir, string(manifest)); err != nil {
		return b, err
	}
	actual, err := command(nil, b.file("perry-infra-isolation-release"), "fingerprint", b.file("checkout"))
	if err != nil || strings.TrimSpace(string(actual)) != expected {
		return b, errors.New("bundle does not match the expected reviewed infrastructure fingerprint")
	}
	digest := sha256.Sum256(manifest)
	b.Manifest = hex.EncodeToString(digest[:])
	return b, nil
}

func (b cutoverBundle) file(name string) string { return filepath.Join(b.Dir, name) }

func checkBundleFiles(dir, manifest string) error {
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(manifest), "\n") {
		digest, name, ok := strings.Cut(line, "  ")
		if !ok || !regexpDigest(digest) || name == "." || filepath.IsAbs(name) || filepath.Clean(name) != name || strings.HasPrefix(name, "../") || seen[name] {
			return errors.New("invalid or duplicate cutover bundle checksum entry")
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(canonical, name))
		if err != nil || resolved != filepath.Join(canonical, name) {
			return errors.New("bundle inputs must be regular files without symlinks: " + name)
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("invalid bundle input: " + name)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return err
		}
		actual := sha256.Sum256(data)
		if hex.EncodeToString(actual[:]) != digest {
			return errors.New("cutover bundle checksum mismatch: " + name)
		}
		seen[name] = true
	}
	for _, name := range []string{"perry-network-admin", "perry-infra-isolation-release", "perry-isolation-redis-admin", "perry-isolation-metrics-admin", "checkout/deploy/install.sh", "source.tar.gz"} {
		if !seen[name] {
			return errors.New("cutover bundle is missing a checksum for " + name)
		}
	}
	return nil
}

func regexpDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type cutoverEvent struct {
	Step, Time string
}

type cutoverProgress struct {
	Version                    int
	Fingerprint, Manifest, Dir string
	CheckMode                  string `json:",omitempty"`
	Next                       int
	Pending, RolledBack        bool
	Events                     []cutoverEvent
}

type cutoverStep struct {
	Name, Check string
	Action      func() error
	Deferred    string
}

func loadCutoverProgress(path string, b cutoverBundle) (cutoverProgress, bool, error) {
	p := cutoverProgress{Version: 1, Fingerprint: b.Fingerprint, Manifest: b.Manifest, Dir: b.Dir}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		p.CheckMode = phaseCheckMode // New runs use combined phase checkpoints.
		return p, false, nil
	}
	if err != nil {
		return p, false, err
	}
	if json.Unmarshal(data, &p) != nil || p.Version != 1 || p.Fingerprint != b.Fingerprint || p.Manifest != b.Manifest || p.Dir != b.Dir || (p.CheckMode != "" && p.CheckMode != phaseCheckMode) {
		return p, true, errors.New("guided progress belongs to different inputs or has an unsupported format; retain it and inspect before continuing")
	}
	return p, true, nil
}

type cutoverFlow struct {
	Prepare, Install, Finalize, Verify, Rollback, Baseline, Archive, Policies, Finish func() error
	Cutover                                                                           func(application) error
}

var appChecks = map[string]string{
	"fleet":        "Check live vehicles, SSE/stream reconnect, route UI and authentication protection.",
	"chinwag":      "Check controlled login, messages and socket reconnect.",
	"vita-track":   "Check controlled login, activity/nutrition reads and GraphQL protection.",
	"im-listening": "Check an existing session, password login/rejection/logout, applicable Google/Spotify paths, and rate limits/Retry-After. Remove disposable test records.",
}

func cutoverSteps(f cutoverFlow) []cutoverStep {
	var steps []cutoverStep
	for _, pass := range []string{"drill", "final"} {
		steps = append(steps, cutoverStep{Name: pass + "/prepare", Action: f.Prepare})
		for _, app := range apps {
			steps = append(steps,
				cutoverStep{Name: pass + "/cutover/" + app.Name, Action: func() error { return f.Cutover(app) }},
				cutoverStep{Name: pass + "/check/" + app.Name, Check: appChecks[app.Name]})
		}
		steps = append(steps,
			cutoverStep{Name: pass + "/install", Action: f.Install},
			cutoverStep{Name: pass + "/finalize", Action: f.Finalize},
			cutoverStep{Name: pass + "/verify", Action: f.Verify},
			cutoverStep{Name: pass + "/monitoring", Check: "Confirm all five external health monitors and Netdata Cloud charts/alerts have recovered. Check application flows again after the shared service restarts."})
		if pass == "drill" {
			steps = append(steps,
				cutoverStep{Name: "drill/rollback", Action: f.Rollback},
				cutoverStep{Name: "drill/baseline", Action: f.Baseline},
				cutoverStep{Name: "drill/restored-check", Check: "The original specifications and policies are restored. Check all four apps, the existing Im Listening session, and monitoring before the final cutover."},
				cutoverStep{Name: "drill/archive", Action: f.Archive})
		}
	}
	return append(steps,
		cutoverStep{Name: "accept", Check: "Confirm the final applications and monitoring work. Accept the cutover and allow reviewed image releases to resume. Protected CI release/rollback evidence and old-overlay retirement remain separate pending tasks."},
		cutoverStep{Name: "policies", Action: f.Policies},
		cutoverStep{Name: "finish", Action: f.Finish})
}

func confirmCutover(reader *bufio.Reader, output io.Writer, step cutoverStep, failed bool) error {
	fmt.Fprintln(output, step.Check)
	for {
		if failed {
			fmt.Fprintln(output, "Type rollback to restore the retained specifications, or stop to retain maintenance for inspection:")
		} else {
			fmt.Fprintf(output, "Type pass %s after checking, rollback to restore, or stop to pause:\n", step.Name)
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return errPaused // EOF, a disconnected terminal and partial input never approve a step.
		}
		switch strings.TrimSpace(line) {
		case "rollback":
			return errRollbackRequested
		case "stop":
			return errPaused
		case "pass " + step.Name:
			if !failed {
				return nil
			}
		}
		fmt.Fprintln(output, "No action taken; enter the displayed response.")
	}
}

// Persist intent before every action. Retrying a stage invokes its live checks;
// a stopped prompt is never silently accepted or advanced on the next run.
func executeCutover(ctx context.Context, p *cutoverProgress, steps []cutoverStep, save func() error, reader *bufio.Reader, output io.Writer, restore func() error) error {
	if p.Next < 0 || p.Next > len(steps) || p.Version != 1 || p.RolledBack {
		return errors.New("invalid or rolled-back guided cutover progress; retain the audit directory")
	}
	for p.Next < len(steps) {
		if ctx.Err() != nil {
			return errPaused
		}
		step := steps[p.Next]
		if p.CheckMode == phaseCheckMode {
			if step.Check != "" {
				checkpoint, total := checkpointPosition(steps, p.Next)
				fmt.Fprintf(output, "\n[Manual checkpoint %d/%d] %s\n", checkpoint, total, step.Name)
			} else if step.Deferred == "" {
				fmt.Fprintf(output, "\nRunning %s\n", step.Name)
			}
		} else {
			fmt.Fprintf(output, "\n[%d/%d] %s\n", p.Next+1, len(steps), step.Name)
		}
		p.Pending = true
		if err := save(); err != nil {
			return err
		}
		var err error
		if step.Deferred != "" {
			fmt.Fprintln(output, step.Deferred)
		} else if step.Check != "" {
			err = confirmCutover(reader, output, step, false)
		} else {
			err = step.Action()
			if err != nil && ctx.Err() == nil {
				fmt.Fprintf(output, "Step failed: %v\n", err)
				choice := confirmCutover(reader, output, cutoverStep{}, true)
				if errors.Is(choice, errRollbackRequested) {
					err = choice
				}
			}
		}
		if errors.Is(err, errRollbackRequested) {
			if recoveryErr := restore(); recoveryErr != nil {
				return fmt.Errorf("rollback failed; retain all recovery inputs: %w", recoveryErr)
			}
			p.RolledBack = true
			p.Events = append(p.Events, cutoverEvent{"administrator/rollback", time.Now().UTC().Format(time.RFC3339)})
			if err := save(); err != nil {
				return err
			}
			return errors.New("rollback passed; retained guided progress and recovery inputs for inspection")
		}
		if err != nil {
			return err
		}
		p.Events = append(p.Events, cutoverEvent{step.Name, time.Now().UTC().Format(time.RFC3339)})
		p.Next++
		p.Pending = false
		if err := save(); err != nil {
			return err
		}
	}
	return nil
}

func guidedCutover(expected string) error {
	return guidedCutoverMode(expected, "")
}

func resumePreparation(expected string) error {
	return guidedCutoverMode(expected, "prepare")
}

func resumeFinalization(expected string) error {
	return guidedCutoverMode(expected, "finalize")
}

func resumePhases(expected string) error {
	return guidedCutoverMode(expected, "phases")
}

func guidedCutoverMode(expected, recovery string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	b, err := checkBundle(filepath.Dir(executable), expected)
	if err != nil {
		return err
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return errors.New("run from an interactive administrator terminal (ssh -t); piped input cannot approve a cutover")
	}
	defer tty.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	go func() { <-ctx.Done(); tty.Close() }()
	return locked(func() error {
		if recovery == "prepare" {
			if err := rebindPreparation(b); err != nil {
				return err
			}
		}
		if recovery == "finalize" {
			if err := rebindFinalization(b); err != nil {
				return err
			}
		}
		if recovery == "phases" {
			if err := rebindPhases(b); err != nil {
				return err
			}
		}
		return runGuidedBundle(ctx, b, bufio.NewReader(tty), tty)
	}, false)
}

// A reviewed recovery build may take over only a paused preparation, before
// any application cutover. Final preparation also requires the accepted drill
// archive. Neither recovery advances the cursor or accepts a checkpoint.
func rebindPreparation(b cutoverBundle) error {
	path := filepath.Join(guidedState, "progress.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var p cutoverProgress
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	if p.Dir == b.Dir && p.Manifest == b.Manifest && p.Fingerprint == b.Fingerprint {
		return nil // Subsequent pauses resume this same reviewed recovery build.
	}
	if !recoverablePreparation(p, b.Fingerprint) {
		return errors.New("recovery accepts only an untouched paused preparation with the same reviewed fingerprint and retained drill evidence for the final pass")
	}
	old, err := checkBundle(p.Dir, p.Fingerprint)
	if err != nil {
		return err
	}
	if _, _, err := loadCutoverProgress(path, old); err != nil {
		return err
	}
	if p.Next == 17 {
		if err := verifyPreparationDrill(filepath.Join(guidedState, "rollback-drill"), saved); err != nil {
			return err
		}
	}
	backup := filepath.Join(guidedState, fmt.Sprintf("progress-before-preparation-recovery-%d-%s.json", p.Next, p.Manifest))
	if existing, err := os.ReadFile(backup); err == nil {
		if string(existing) != string(data) {
			return errors.New("different recovery progress backup already exists; nothing overwritten")
		}
	} else if os.IsNotExist(err) {
		if err := atomicFile(backup, data, 0600); err != nil {
			return err
		}
	} else {
		return err
	}
	fmt.Println("Verifying and recovering the paused preparation; original progress and rollback inputs are retained.")
	if err := infraLocked(completePreparation); err != nil {
		return err
	}
	p = preparationProgress(p, b, time.Now())
	updated, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return atomicFile(path, append(updated, '\n'), 0600)
}

func recoverablePreparation(p cutoverProgress, expected string) bool {
	if p.Version != 1 || !p.Pending || p.RolledBack || p.Fingerprint != expected || (p.CheckMode != "" && p.CheckMode != phaseCheckMode) {
		return false
	}
	if p.Next == 0 {
		return len(p.Events) == 0
	}
	if p.Next != 17 || p.CheckMode != phaseCheckMode || len(p.Events) < 2 || p.Events[len(p.Events)-1].Step != "drill/archive" {
		return false
	}
	for _, event := range p.Events[:len(p.Events)-1] {
		if event.Step == "drill/restored-check" {
			return true
		}
	}
	return false
}

func preparationProgress(p cutoverProgress, b cutoverBundle, now time.Time) cutoverProgress {
	p.Dir, p.Manifest = b.Dir, b.Manifest
	p.Events = append(append([]cutoverEvent(nil), p.Events...), cutoverEvent{"preparation/recovery", now.UTC().Format(time.RFC3339)})
	return p
}

func runGuidedBundle(ctx context.Context, b cutoverBundle, reader *bufio.Reader, output io.Writer) error {
	if err := os.Mkdir(guidedState, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(guidedState)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
		return errors.New("guided progress must be a root-owned private directory")
	}
	path := filepath.Join(guidedState, "progress.json")
	p, exists, err := loadCutoverProgress(path, b)
	if err != nil {
		return err
	}
	if !exists {
		if _, err := os.Lstat(state); !os.IsNotExist(err) {
			return errors.New("an existing manual migration requires inspection; guided cutover will not adopt or overwrite it")
		}
	}
	save := func() error {
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return err
		}
		return atomicFile(path, append(data, '\n'), 0600)
	}
	fmt.Fprintln(output, "Guided network cutover: release locks are held. This runs a full rollback drill, then the final rollout, with app checks in both passes. Expect brief shared-service outages. Type stop at a prompt to pause; rerun the same command to continue. Recovery inputs and private subprocess logs stay under /var/lib.")
	f := b.flow()
	steps := cutoverStepsForMode(f, p.CheckMode)
	if p.Next < 0 || p.Next > len(steps) {
		return errors.New("invalid guided cutover cursor; retain the audit directory")
	}
	if p.CheckMode == phaseCheckMode {
		_, remaining := checkpointPosition(steps[p.Next:], -1)
		fmt.Fprintf(output, "Phase checkpoints enabled: %d manual checks remain. Automatic service, health, database, Redis, isolation and monitoring checks still run; a failure pauses the flow. Deferred app walkthroughs are checked together after each complete phase.\n", remaining)
	}
	if err := executeCutover(ctx, &p, steps, save, reader, output, f.Rollback); err != nil {
		fmt.Fprintf(output, "Recovery: sudo %s rollback\n", b.file("perry-network-admin"))
		return err
	}
	fmt.Fprintln(output, "Production cutover applied and application checks accepted. Old overlays and both audit sets are retained. Record protected CI image/infrastructure release and rollback checks before closing docs/tasks.md; retire old overlays only after the rollback window.")
	return nil
}

// Application locks remain held across prompts and all stages. External tools
// take only the infrastructure lock; no restricted account gains a new command.
func (b cutoverBundle) flow() cutoverFlow {
	verifyData := func() error {
		for _, tool := range []string{"perry-isolation-redis-admin", "perry-isolation-metrics-admin"} {
			if err := b.logged(tool, b.file(tool), "verify", b.file("checkout")); err != nil {
				return err
			}
		}
		return nil
	}
	return cutoverFlow{
		Prepare: func() error {
			return infraLocked(func() error {
				if require("prepared") == nil {
					if err := active(); err != nil {
						return err
					}
					return health()
				}
				return prepareHeld(b.file("perry-infra-isolation-release"))
			})
		},
		Cutover: ensureCutover,
		Install: func() error {
			return b.logged("snapshot-install", "/bin/sh", b.file("checkout/deploy/install.sh"), b.file("perry-infra-isolation-release"), state+"/release.pub")
		},
		Finalize: func() error { return finalizeHeld(b.Fingerprint) },
		Verify: func() error {
			if err := verify(); err != nil {
				return err
			}
			return verifyData()
		},
		Rollback: func() error { return infraLocked(rollbackHeld) },
		Baseline: func() error {
			if err := verifyBaseline(); err != nil {
				return err
			}
			return verifyData()
		},
		Archive: func() error {
			destination := filepath.Join(guidedState, "rollback-drill")
			if _, err := os.Lstat(destination); err == nil {
				if _, err := os.Lstat(state); os.IsNotExist(err) {
					return nil
				}
				return errors.New("rollback drill archive already exists; nothing overwritten")
			} else if !os.IsNotExist(err) {
				return err
			}
			if err := verifyBaseline(); err != nil {
				return err
			}
			return os.Rename(state, destination)
		},
		Policies: func() error {
			for _, app := range apps {
				current, err := checkedCutoverService(app)
				if err != nil {
					return err
				}
				policy, err := releasePolicy(current)
				if err != nil {
					return err
				}
				if err := atomicFile("/opt/portfolio-release/"+app.Name+".sha256", []byte(policy+"\n"), 0600); err != nil {
					return err
				}
			}
			return nil
		},
		Finish: func() error {
			if err := verify(); err != nil {
				return err
			}
			return infraLocked(func() error {
				actual, err := command(nil, broker, "fingerprint", snapshot)
				if err != nil || strings.TrimSpace(string(actual)) != b.Fingerprint {
					return errors.New("installed snapshot changed before acceptance")
				}
				if err := private("accepted", []byte(time.Now().UTC().Format(time.RFC3339)+"\n")); err != nil {
					return err
				}
				if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
					return err
				}
				return nil
			})
		},
	}
}

func checkedCutoverService(app application) (service, error) {
	want, err := saved(app.Service)
	if err != nil {
		return service{}, err
	}
	if err := setNetworks(want, desired(app), strings.SplitN(app.Service, "_", 2)[1]); err != nil {
		return service{}, err
	}
	want.Spec["Labels"].(map[string]any)["traefik.swarm.network"] = app.Ingress
	current, err := inspect(app.Service)
	if err != nil {
		return service{}, err
	}
	if !sameSpec(want, current) {
		return service{}, errors.New("service differs from the retained, approved network-only cutover: " + app.Name)
	}
	return current, nil
}

func ensureCutover(app application) error {
	if err := active(); err != nil {
		return err
	}
	if _, err := checkedCutoverService(app); err == nil {
		if err := verifyClient(app, false); err != nil {
			return err
		}
		if err := health(); err != nil {
			return err
		}
		return private("cutover-"+app.Name, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"))
	}
	return infraLocked(func() error { return cutoverHeld(app) })
}

// This is the administrator policy wire format used by deploy/portfolio. The
// caller first checks the entire live spec against the retained network change.
func releasePolicy(s service) (string, error) {
	copyJSON, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	var copy service
	if err := decode(copyJSON, &copy); err != nil {
		return "", err
	}
	delete(containerSpec(copy), "Image")
	data, err := json.Marshal(copy.Spec)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(append([]byte(copy.ID+"\n"), data...))
	return hex.EncodeToString(digest[:]), nil
}

func verifyBaseline() error {
	if err := require("rolled-back"); err != nil {
		return err
	}
	names := append([]string{}, infrastructure...)
	for _, app := range apps {
		names = append(names, app.Service)
	}
	for _, name := range names {
		original, err := saved(name)
		if err != nil {
			return err
		}
		current, err := inspect(name)
		if err != nil {
			return err
		}
		if !sameSpec(original, current) {
			return errors.New("rollback did not restore the original service: " + name)
		}
	}
	return health()
}

func (b cutoverBundle) logged(label, program string, args ...string) error {
	log, err := os.OpenFile(filepath.Join(guidedState, label+"-"+time.Now().UTC().Format("20060102T150405.000000000")+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "DOCKER_HOST=unix:///var/run/docker.sock"}
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	if err := cmd.Run(); err != nil {
		return errors.New(label + " failed; inspect root-only log " + log.Name())
	}
	return nil
}
