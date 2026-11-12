package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Only the original spec or its exact reviewed network-only change is eligible
// for dependency-restart recovery. A new image, mount, credential or permission
// change always requires administrator inspection instead.
func allowedClient(want, current service, resolve func(string) (network, error), app application) bool {
	if sameSpec(want, current) {
		return true
	}
	if err := setNetworksWith(want, desired(app), strings.SplitN(app.Service, "_", 2)[1], resolve); err != nil {
		return false
	}
	want.Spec["Labels"].(map[string]any)["traefik.swarm.network"] = app.Ingress
	return sameSpec(want, current)
}

func cleanCompletion(data []byte, serviceID string) bool {
	var rows []struct {
		ServiceID    string
		DesiredState string
		Status       struct {
			State           string
			ContainerStatus struct{ ExitCode *int }
		}
	}
	if json.Unmarshal(data, &rows) != nil || len(rows) != 1 || rows[0].ServiceID != serviceID ||
		rows[0].Status.ContainerStatus.ExitCode == nil || *rows[0].Status.ContainerStatus.ExitCode != 0 {
		return false
	}
	return rows[0].Status.State == "complete" ||
		(rows[0].Status.State == "shutdown" && rows[0].DesiredState == "shutdown")
}

func restartCompletedClient(original service, inspectFn func(string) (service, error), submit func(service) error, stopped, ready func(service) error) error {
	encoded, err := json.Marshal(original)
	if err != nil {
		return err
	}
	var zero service
	if err := decode(encoded, &zero); err != nil {
		return err
	}
	mode, _ := zero.Spec["Mode"].(map[string]any)
	replicated, _ := mode["Replicated"].(map[string]any)
	if replicated["Replicas"] != json.Number("1") {
		return errors.New("client recovery requires its reviewed single replica")
	}
	replicated["Replicas"] = json.Number("0")
	if err := submit(zero); err != nil {
		return err
	}
	// An accepted update is not an orchestrator acknowledgement. Swarm can
	// coalesce a rapid 0 -> 1 update and leave the completed slot untouched.
	// Wait for the task being replaced, not all retained update history.
	stoppedErr := stopped(zero)
	current, err := inspectFn(original.Spec["Name"].(string))
	if err != nil {
		return err
	}
	if !sameSpec(zero, current) {
		return errors.New("client changed during recovery; refusing to overwrite it")
	}
	original.Version = current.Version
	if err := submit(original); err != nil {
		return err
	}
	if stoppedErr != nil {
		return fmt.Errorf("client scale-down did not converge; restored requested replica count: %w", stoppedErr)
	}
	return ready(original)
}

func retiredClientRemoved(want service, taskID string, run func(...string) ([]byte, error)) (bool, error) {
	if taskID == "" {
		return false, errors.New("client recovery requires the completed task ID")
	}
	tasks, err := run("service", "ps", "-q", "--no-trunc", want.ID)
	if err != nil {
		return false, err
	}
	for _, id := range strings.Fields(string(tasks)) {
		if id == taskID {
			return false, nil
		}
	}
	// Removal of that exact task acknowledges the zero-replica update. Older
	// terminal tasks may remain as history; no desired-running task or running
	// container may remain before restoring the reviewed single replica.
	live, err := run("service", "ps", "-q", "--no-trunc", "--filter", "desired-state=running", want.ID)
	if err != nil || len(strings.Fields(string(live))) != 0 {
		return false, err
	}
	containers, err := run("ps", "-q", "--filter", "label=com.docker.swarm.service.name="+want.Spec["Name"].(string))
	if err != nil {
		return false, err
	}
	return len(strings.Fields(string(containers))) == 0, nil
}

func waitStopped(want service, taskID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return waitForStopped(ctx, want, inspect, func(s service) (bool, error) {
		return retiredClientRemoved(s, taskID, docker)
	})
}

func waitForStopped(ctx context.Context, want service, inspectFn func(string) (service, error), removed func(service) (bool, error)) error {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for retired client tasks: %w", err)
		}
		current, err := inspectFn(want.Spec["Name"].(string))
		if err != nil {
			return err
		}
		if !sameSpec(want, current) {
			return errors.New("client changed while waiting for scale-down")
		}
		gone, err := removed(current)
		if err != nil {
			return err
		}
		if gone {
			return nil
		}
		if attempt%15 == 0 {
			fmt.Println("Waiting for Swarm to retire the completed client task:", want.Spec["Name"])
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func recoverApplicationClients() error {
	for _, app := range apps {
		fmt.Println("Checking application after shared-service restart:", app.Name)
		deadline := time.Now().Add(2 * time.Minute)
		for attempt := 0; attempt < 120; attempt++ {
			want, err := saved(app.Service)
			if err != nil {
				return err
			}
			current, err := inspect(app.Service)
			if err != nil {
				return err
			}
			if !allowedClient(want, current, inspectNetwork, app) {
				return errors.New("application changed outside the reviewed cutover: " + app.Name)
			}
			ids, err := docker("ps", "-q", "--filter", "label=com.docker.swarm.service.name="+app.Service)
			if err != nil {
				return err
			}
			if len(strings.Fields(string(ids))) == 1 {
				response, probeErr := (&http.Client{Timeout: 5 * time.Second}).Get(app.Health)
				if probeErr == nil {
					response.Body.Close()
					if response.StatusCode == http.StatusOK {
						break
					}
				}
			}
			if len(strings.Fields(string(ids))) > 1 {
				return errors.New("unexpected concurrent client tasks: " + app.Name)
			}
			tasks, err := docker("service", "ps", "--no-trunc", "--format", "{{.ID}}", app.Service)
			if err != nil {
				return err
			}
			if rows := strings.Fields(string(tasks)); len(rows) > 0 {
				data, err := docker("inspect", "--type", "task", rows[0])
				if err != nil {
					return err
				}
				if cleanCompletion(data, current.ID) {
					fmt.Println("Restarting cleanly exited client while retaining its service specification:", app.Name)
					if err := restartCompletedClient(current, inspect, submitService, func(zero service) error {
						return waitStopped(zero, rows[0])
					}, wait); err != nil {
						return err
					}
					break
				}
			}
			if attempt == 119 || time.Now().After(deadline) {
				return errors.New("application did not restart after its dependency update: " + app.Name)
			}
			if attempt%15 == 0 {
				fmt.Println("Waiting for application task:", app.Name)
			}
			time.Sleep(time.Second)
		}
	}
	return nil
}

// The final pass must retain the successful rollback drill and begin from the
// exact same baseline. Inspect only; never alter either set of rollback inputs.
func verifyPreparationDrill(archive string, originals func(string) (service, error)) error {
	for _, file := range []string{"rolled-back", "backed-up", "installation.tar.gz"} {
		info, err := os.Lstat(filepath.Join(archive, file))
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("final preparation recovery requires the retained rollback drill: " + file)
		}
	}
	names := append([]string{}, infrastructure...)
	for _, app := range apps {
		names = append(names, app.Service)
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(archive, name+".json"))
		var retained service
		if err != nil || decode(data, &retained) != nil || retained.Spec["Name"] != name || retained.ID == "" {
			return errors.New("rollback drill service inventory is missing or invalid: " + name)
		}
		original, err := originals(name)
		if err != nil {
			return err
		}
		if original.ID != retained.ID || !sameSpec(original, retained) {
			return errors.New("final preparation differs from the retained rollback baseline: " + name)
		}
	}
	return nil
}

// Resume only the fully applied preparation topology, never an arbitrary
// partial migration. This also handles a prior run stopping at its health gate.
func completePreparation() error {
	if err := active(); err != nil {
		return err
	}
	for _, app := range apps {
		if require("cutover-"+app.Name) == nil {
			return errors.New("preparation recovery cannot adopt an application cutover")
		}
		original, err := saved(app.Service)
		if err != nil {
			return err
		}
		current, err := inspect(app.Service)
		if err != nil {
			return err
		}
		if !sameSpec(original, current) {
			return errors.New("application changed since preparation inventory: " + app.Name)
		}
	}
	for _, want := range overlays {
		n, err := inspectNetwork(want.Name)
		if err != nil {
			return err
		}
		if err := checkNetwork(n, want); err != nil {
			return err
		}
	}
	for _, name := range infrastructure {
		original, err := saved(name)
		if err != nil {
			return err
		}
		names, err := networkNames(original)
		if err != nil {
			return err
		}
		for _, app := range apps {
			switch name {
			case "perry-infra_traefik":
				names = append(names, app.Ingress)
			case "perry-infra_postgres":
				if app.Database != "" {
					names = append(names, app.Database)
				}
			case "perry-infra_redis":
				if app.Redis != "" {
					names = append(names, app.Redis)
				}
			}
		}
		if err := setNetworks(original, names, strings.TrimPrefix(name, "perry-infra_")); err != nil {
			return err
		}
		current, err := inspect(name)
		if err != nil {
			return err
		}
		if !sameSpec(original, current) {
			return errors.New("shared service differs from completed preparation: " + name)
		}
		if err := wait(current); err != nil {
			return err
		}
	}
	if err := recoverApplicationClients(); err != nil {
		return err
	}
	if err := health(); err != nil {
		return err
	}
	return private("prepared", []byte("preparation topology and recovered clients verified\n"))
}
