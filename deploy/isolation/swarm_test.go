package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// This daemon runs inside a disposable privileged local Docker container. It
// never mounts the host socket, publishes a port or changes the host's Swarm.
func TestDisposableSwarmCompletedClientRecovery(t *testing.T) {
	if os.Getenv("ISOLATION_SWARM_TEST") != "1" {
		t.Skip("set ISOLATION_SWARM_TEST=1 with Docker")
	}
	name := fmt.Sprintf("perry-recovery-swarm-%d", time.Now().UnixNano())
	run := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	must := func(args ...string) string {
		t.Helper()
		out, err := run(args...)
		if err != nil {
			t.Fatalf("fixture command %s: %v: %s", args[0], err, out)
		}
		return out
	}
	must("run", "-d", "--privileged", "--name", name, "-e", "DOCKER_TLS_CERTDIR=", "docker:29.6.2-dind", "--host=unix:///var/run/docker.sock")
	t.Cleanup(func() { _, _ = run("rm", "-fv", name) })
	inner := func(args ...string) (string, error) { return run(append([]string{"exec", name, "docker"}, args...)...) }
	innerMust := func(args ...string) string {
		t.Helper()
		out, err := inner(args...)
		if err != nil {
			t.Fatalf("inner fixture %s: %v: %s", args[0], err, out)
		}
		return out
	}
	eventually := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(time.Minute)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("fixture timed out: " + label)
	}
	eventually("daemon startup", func() bool { _, err := inner("info"); return err == nil })
	innerMust("swarm", "init")
	images := strings.Fields(must("image", "ls", "alpine", "--no-trunc", "--format", "{{.ID}}"))
	if len(images) == 0 {
		t.Fatal("fixture requires an already installed Alpine image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	save := exec.CommandContext(ctx, "docker", "save", images[0])
	pipe, err := save.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	load := exec.CommandContext(ctx, "docker", "exec", "-i", name, "docker", "load")
	load.Stdin = pipe
	if err := save.Start(); err != nil {
		t.Fatal(err)
	}
	if out, err := load.CombinedOutput(); err != nil {
		t.Fatalf("load fixture image: %v %s", err, out)
	}
	if err := save.Wait(); err != nil {
		t.Fatal(err)
	}
	innerMust("service", "create", "--detach=true", "--no-resolve-image", "--name", "completed-client", "--restart-condition", "on-failure", "--stop-grace-period", "2s", "--mount", "type=volume,source=fixture-state,target=/state", "--entrypoint", "/bin/sh", images[0], "-c", "if [ -e /state/next-completes ]; then rm /state/next-completes; exit 0; fi; trap 'exit 0' TERM; while :; do sleep 1 & wait $!; done")
	var historicalTask, containerID string
	eventually("initial task running", func() bool {
		ids, err := inner("service", "ps", "-q", "--no-trunc", "--filter", "desired-state=running", "completed-client")
		if err != nil || len(strings.Fields(ids)) != 1 {
			return false
		}
		historicalTask = ids
		out, err := inner("inspect", "--type", "task", "--format", "{{.Status.State}} {{.Status.ContainerStatus.ContainerID}}", historicalTask)
		fields := strings.Fields(out)
		if err != nil || len(fields) != 2 || fields[0] != "running" {
			return false
		}
		containerID = fields[1]
		return true
	})
	innerMust("exec", containerID, "touch", "/state/next-completes")
	innerMust("service", "update", "--detach=true", "--force", "completed-client")
	var oldTask string
	eventually("clean completion", func() bool {
		ids, err := inner("service", "ps", "-q", "--no-trunc", "completed-client")
		rows := strings.Fields(ids)
		if err != nil || len(rows) != 2 || rows[0] == historicalTask {
			return false
		}
		oldTask = rows[0]
		out, err := inner("inspect", "--type", "task", oldTask)
		return err == nil && strings.Contains(out, `"State": "complete"`)
	})
	inspectFn := func(serviceName string) (service, error) {
		out, err := inner("service", "inspect", serviceName)
		var rows []service
		if err != nil {
			return service{}, err
		}
		if err := decode([]byte(out), &rows); err != nil || len(rows) != 1 {
			return service{}, fmt.Errorf("invalid fixture service")
		}
		return rows[0], nil
	}
	original, err := inspectFn("completed-client")
	if err != nil {
		t.Fatal(err)
	}
	historyRetained := false
	err = restartCompletedClient(original, inspectFn, func(s service) error {
		replicas := s.Spec["Mode"].(map[string]any)["Replicated"].(map[string]any)["Replicas"]
		_, err := inner("service", "scale", "--detach=true", "completed-client="+fmt.Sprint(replicas))
		return err
	}, func(zero service) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return waitForStopped(ctx, zero, inspectFn, func(s service) (bool, error) {
			gone, err := retiredClientRemoved(s, oldTask, func(args ...string) ([]byte, error) {
				out, err := inner(args...)
				return []byte(out), err
			})
			if gone && err == nil {
				tasks := innerMust("service", "ps", "-q", "--no-trunc", "completed-client")
				for _, id := range strings.Fields(tasks) {
					if id == historicalTask {
						historyRetained = true
					}
				}
			}
			return gone, err
		})
	}, func(want service) error {
		eventually("replacement running", func() bool {
			ids, err := inner("service", "ps", "-q", "--no-trunc", "--filter", "desired-state=running", "completed-client")
			if err != nil || len(strings.Fields(ids)) != 1 || ids == oldTask {
				return false
			}
			out, err := inner("inspect", "--type", "task", "--format", "{{.Status.State}}", ids)
			return err == nil && out == "running"
		})
		current, err := inspectFn("completed-client")
		if err == nil && !sameSpec(original, current) {
			return fmt.Errorf("recovery changed final fixture spec")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !historyRetained {
		t.Fatal("fixture did not retain the historical task that blocked the previous all-history wait")
	}
	t.Log("replacement is running with its exact original specification while older task history remains")
}
