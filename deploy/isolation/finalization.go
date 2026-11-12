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

// Adoption changes only the reviewed executable binding, never the cursor or
// retained rollback inputs. Every accepted app check remains in the audit.
func recoverableFinalization(p cutoverProgress, expected string) bool {
	if p.Version != 1 || p.Fingerprint != expected || !p.Pending || p.RolledBack {
		return false
	}
	steps := cutoverSteps(cutoverFlow{})
	return p.Next >= 0 && p.Next < len(steps) &&
		(steps[p.Next].Name == "drill/finalize" || steps[p.Next].Name == "final/finalize")
}

func rebindFinalization(b cutoverBundle) error {
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
		return nil // Later pauses keep using the same immutable recovery bundle.
	}
	if !recoverableFinalization(p, b.Fingerprint) {
		return errors.New("finalization recovery accepts only a paused finalization with the same reviewed fingerprint")
	}
	old, err := checkBundle(p.Dir, p.Fingerprint)
	if err != nil {
		return err
	}
	if _, _, err := loadCutoverProgress(path, old); err != nil {
		return err
	}
	if err := active(); err != nil {
		return err
	}
	actual, err := command(nil, b.file("perry-infra-isolation-release"), "fingerprint", snapshot)
	if err != nil || strings.TrimSpace(string(actual)) != b.Fingerprint {
		return errors.New("installed snapshot differs from the reviewed finalization inputs")
	}
	for _, app := range apps {
		if err := require("cutover-" + app.Name); err != nil {
			return err
		}
		if _, err := checkedCutoverService(app); err != nil {
			return err
		}
	}
	backup := filepath.Join(guidedState, fmt.Sprintf("progress-before-finalization-recovery-%d-%s.json", p.Next, p.Manifest))
	if existing, err := os.ReadFile(backup); err == nil {
		if string(existing) != string(data) {
			return errors.New("different finalization recovery backup already exists; nothing overwritten")
		}
	} else if os.IsNotExist(err) {
		if err := atomicFile(backup, data, 0600); err != nil {
			return err
		}
	} else {
		return err
	}
	p.Dir, p.Manifest = b.Dir, b.Manifest
	p.Events = append(p.Events, cutoverEvent{"finalization/recovery", time.Now().UTC().Format(time.RFC3339)})
	updated, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicFile(path, append(updated, '\n'), 0600); err != nil {
		return err
	}
	fmt.Println("Resuming the paused finalization with task-removal-aware client recovery; accepted checks, original bundle and rollback inputs are retained.")
	return nil
}
