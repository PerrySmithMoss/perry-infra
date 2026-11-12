package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const phaseCheckMode = "phase"

func checkpointPosition(steps []cutoverStep, current int) (position, total int) {
	for i, step := range steps {
		if step.Check != "" {
			total++
			if i <= current {
				position++
			}
		}
	}
	return position, total
}

func combinedAppCheck() string {
	var lines []string
	lines = append(lines, "Use the retained controlled accounts and test records. Check the four apps together after this phase's last service change:")
	for _, app := range apps {
		lines = append(lines, app.Name+": "+appChecks[app.Name])
	}
	return strings.Join(lines, "\n") + "\nAutomated health, PostgreSQL/migration, Redis and network isolation checks must have passed before this checkpoint. Confirm all five external monitors are Up and Netdata Cloud charts and alert evaluations have recovered. Record unresolved provider/quota issues explicitly; they are not successful functional checks."
}

// Keep action order and durable cursor positions compatible with the original
// flow. A deferred walkthrough is recorded as deferred, never as a passed check.
func cutoverStepsForMode(f cutoverFlow, mode string) []cutoverStep {
	steps := cutoverSteps(f)
	if mode != phaseCheckMode {
		return steps
	}
	for i := range steps {
		step := &steps[i]
		if strings.Contains(step.Name, "/check/") {
			pass, app, _ := strings.Cut(step.Name, "/check/")
			gate := "drill/monitoring"
			if pass == "final" {
				gate = "accept"
			}
			step.Name = pass + "/deferred-check/" + app
			step.Check = ""
			step.Deferred = app + ": automatic cutover checks passed; functional walkthrough is deferred to " + gate + "."
		}
		switch step.Name {
		case "drill/monitoring":
			step.Check = combinedAppCheck() + "\nAccept this drill result before the automatic rollback drill."
		case "drill/restored-check":
			step.Check = "The retained specifications and policies have been restored and automatically verified. Check existing sessions, core app reads/reconnect and monitoring once on this restored baseline, using the retained test accounts. No new signup or provider-linking flow is needed."
		case "final/monitoring":
			step.Name = "final/deferred-monitoring"
			step.Check = ""
			step.Deferred = "Final monitoring and application walkthroughs are consolidated into the accept checkpoint."
		case "accept":
			step.Check = combinedAppCheck() + "\nAccept the final rollout and allow reviewed image releases to resume only after this combined check. Protected CI release/rollback evidence and old-overlay retirement remain separate requirements before closing the task."
		}
	}
	return steps
}

func recoverablePhaseCheckpoint(p cutoverProgress, expected string) bool {
	return p.Version == 1 && p.CheckMode == "" && p.Fingerprint == expected && p.Pending && !p.RolledBack &&
		(p.Next == 12 || p.Next == 15)
}

func phaseProgress(p cutoverProgress, b cutoverBundle, now time.Time) cutoverProgress {
	p.Dir, p.Manifest, p.CheckMode = b.Dir, b.Manifest, phaseCheckMode
	p.Events = append(append([]cutoverEvent(nil), p.Events...), cutoverEvent{"phase-checkpoints/enabled", now.UTC().Format(time.RFC3339)})
	return p
}

func rebindPhases(b cutoverBundle) error {
	path := filepath.Join(guidedState, "progress.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var p cutoverProgress
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	if p.Dir == b.Dir && p.Manifest == b.Manifest && p.Fingerprint == b.Fingerprint && p.CheckMode == phaseCheckMode {
		return nil
	}
	if !recoverablePhaseCheckpoint(p, b.Fingerprint) {
		return errors.New("phase checkpoints can adopt only the paused drill monitoring or restored-baseline check with the same reviewed fingerprint")
	}
	old, err := checkBundle(p.Dir, p.Fingerprint)
	if err != nil {
		return err
	}
	if _, _, err := loadCutoverProgress(path, old); err != nil {
		return err
	}
	if p.Next == 12 {
		if err := active(); err != nil {
			return err
		}
		if err := require("finalized"); err != nil {
			return err
		}
		actual, err := command(nil, b.file("perry-infra-isolation-release"), "fingerprint", snapshot)
		if err != nil || strings.TrimSpace(string(actual)) != b.Fingerprint {
			return errors.New("installed snapshot differs from the reviewed phase-checkpoint inputs")
		}
		for _, app := range apps {
			if err := require("cutover-" + app.Name); err != nil {
				return err
			}
			if _, err := checkedCutoverService(app); err != nil {
				return err
			}
		}
	} else if err := verifyBaseline(); err != nil {
		return err
	}
	backup := filepath.Join(guidedState, fmt.Sprintf("progress-before-phase-checkpoints-%d-%s.json", p.Next, p.Manifest))
	if existing, err := os.ReadFile(backup); err == nil {
		if string(existing) != string(data) {
			return errors.New("different phase-checkpoint backup already exists; nothing overwritten")
		}
	} else if os.IsNotExist(err) {
		if err := atomicFile(backup, data, 0600); err != nil {
			return err
		}
	} else {
		return err
	}
	p = phaseProgress(p, b, time.Now())
	updated, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicFile(path, append(updated, '\n'), 0600); err != nil {
		return err
	}
	fmt.Println("Phase checkpoints enabled; completed checks, the original bundle, current prompt and rollback inputs are retained. No deferred check is recorded as passed.")
	return nil
}
