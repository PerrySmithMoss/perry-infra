package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Evaluate the actual configuration in Netdata, including rule selection and
// hysteresis. Only the fixture's lookup/evaluation intervals are shortened.
func TestChinwagRollbackAlert(t *testing.T) {
	if os.Getenv("METRICS_DOCKER_TEST") != "1" {
		t.Skip("set METRICS_DOCKER_TEST=1 with a Docker daemon")
	}
	name := "perry-alert-test-" + time.Now().Format("150405.000000000")
	docker := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture docker %s failed: %s", args[0], output)
		}
		return output
	}
	docker("create", "--name", name, "--user", "0", "--network", "none", "--memory", "192m", "--cpus", ".25", "--pids-limit", "64", "--entrypoint", "/usr/sbin/netdata",
		"netdata/netdata:v2.12.3@sha256:c7eab00e20901eb60595ebae0fbfe88e59464e663924461874719d40de0735d2", "-D", "-u", "root")
	t.Cleanup(func() { exec.Command("docker", "rm", "-fv", name).Run() })
	directory := t.TempDir()
	copyFile := func(filename, target, contents string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(directory, filename)
		if err := os.WriteFile(path, []byte(contents), mode); err != nil {
			t.Fatal(err)
		}
		docker("cp", path, name+":"+target)
	}
	health, err := os.ReadFile("health.conf")
	if err != nil {
		t.Fatal(err)
	}
	shortHealth := strings.Replace(string(health), "lookup: average -5m unaligned of rollback", "lookup: average -10s unaligned of rollback", 1)
	shortHealth = strings.Replace(shortHealth, "every: 1m", "every: 1s", 1)
	copyFile("health.conf", "/etc/netdata/health.d/perry-private-metrics.conf", shortHealth, 0o644)
	copyFile("netdata.conf", "/etc/netdata/netdata.conf", `[global]
    update every = 1
[db]
    mode = ram
    retention = 600
[web]
    bind to = 127.0.0.1
[ml]
    enabled = no
[health]
    enabled = yes
    run at least every seconds = 1
    postpone alarms during hibernation for seconds = 0
[plugins]
    enable running new plugins = no
    fixture = yes
    proc = no
    diskspace = no
    cgroups = no
    go.d = no
    apps = no
    ebpf = no
    tc = no
    timex = no
    idlejitter = no
`, 0o644)
	copyFile("fixture.plugin", "/usr/libexec/netdata/plugins.d/fixture.plugin", `#!/bin/sh
for db in chinwag vita_track; do
    echo "CHART postgres_private.db_${db}_transactions_ratio '' 'Fixture' 'percentage' 'transactions' 'postgres.db_transactions_ratio' line 1 1"
    echo "CLABEL database '${db}' 1"
    echo 'CLABEL_COMMIT'
    echo "DIMENSION committed '' absolute 1 100"
    echo "DIMENSION rollback '' absolute 1 100"
done
while :; do
    value=$(cat /tmp/rollback)
    for db in chinwag vita_track; do
        echo "BEGIN postgres_private.db_${db}_transactions_ratio"
        echo 'SET committed = 9778'
        echo "SET rollback = $value"
        echo 'END'
    done
    sleep 1
done
`, 0o755)
	copyFile("rollback", "/tmp/rollback", "222\n", 0o644)
	docker("start", name)
	type alarm struct {
		Status string   `json:"status"`
		Source string   `json:"source"`
		Value  *float64 `json:"value"`
		Up     int      `json:"delay_up_duration"`
		Down   int      `json:"delay_down_duration"`
	}
	for _, step := range []struct {
		input, status string
		value         float64
	}{{"222", "CLEAR", 2.22}, {"1500", "WARNING", 15}, {"700", "WARNING", 7}, {"500", "CLEAR", 5}} {
		copyFile("rollback", "/tmp/rollback", step.input+"\n", 0o644)
		matched := false
		deadline := time.Now().Add(40 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(2 * time.Second)
			output, err := exec.Command("docker", "exec", name, "curl", "-fsS", "http://127.0.0.1:19999/api/v1/alarms?all").Output()
			if err != nil {
				continue
			}
			var response struct {
				Alarms map[string]alarm `json:"alarms"`
			}
			if err := json.Unmarshal(output, &response); err != nil {
				t.Fatal(err)
			}
			current := response.Alarms["postgres_private.db_chinwag_transactions_ratio.postgres_db_transactions_rollback_ratio"]
			stock := response.Alarms["postgres_private.db_vita_track_transactions_ratio.postgres_db_transactions_rollback_ratio"]
			if current.Status == step.status && current.Value != nil && math.Abs(*current.Value-step.value) < .01 {
				if !strings.Contains(current.Source, "perry-private-metrics.conf") || current.Up != 300 || current.Down != 300 || !strings.Contains(stock.Source, "conf.d/health.d/postgres.conf") {
					t.Fatal("wrong rule source, delays or scope")
				}
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("rollback %.2f did not evaluate to %s", step.value, step.status)
		}
		t.Logf("rollback %.2f%% evaluates to %s; other database retains stock rule", step.value, step.status)
	}
}
