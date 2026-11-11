package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestScaleWaitsForStoppedContainersWithoutAttachedMonitor(t *testing.T) {
	polls := 0
	err := scaleWith(func(args ...string) ([]byte, error) {
		if args[0] == "service" {
			if strings.Join(args, " ") != "service scale --detach=true api=0" {
				t.Fatal("zero-replica scaling must not use Docker's attached monitor")
			}
			return nil, nil
		}
		if args[0] != "ps" {
			t.Fatal("must wait for actual stopped containers")
		}
		polls++
		if polls < 3 {
			return []byte("old-container\n"), nil
		}
		return nil, nil
	}, func(time.Duration) {}, "api", 0)
	if err != nil || polls != 3 {
		t.Fatal("did not wait until all old containers stopped")
	}
}

func TestScaleWaitsForHealthyContainer(t *testing.T) {
	checks := 0
	err := scaleWith(func(args ...string) ([]byte, error) {
		switch args[0] {
		case "service":
			return nil, nil
		case "ps":
			return []byte("container"), nil
		case "inspect":
			checks++
			if checks == 1 {
				return []byte("true starting"), nil
			}
			return []byte("true healthy"), nil
		}
		return nil, errors.New("unexpected command")
	}, func(time.Duration) {}, "api", 1)
	if err != nil || checks != 2 {
		t.Fatal("accepted a container before its healthcheck passed")
	}
}

func TestResumeRejectsChangesToRuntimeSpecification(t *testing.T) {
	saved := []byte(`[{"ID":"service-id","Spec":{"Mode":{"Replicated":{"Replicas":1}},"TaskTemplate":{"ContainerSpec":{"Image":"reviewed"}}}}]`)
	paused := []byte(strings.Replace(string(saved), `"Replicas":1`, `"Replicas":0`, 1))
	if !samePausedService(saved, paused) {
		t.Fatal("replica-only pause must be resumable")
	}
	restarted := strings.Replace(string(paused), `"TaskTemplate":{`, `"TaskTemplate":{"ForceUpdate":1,`, 1)
	if !samePausedService(saved, []byte(restarted)) {
		t.Fatal("replica restart counter must be resumable")
	}
	for _, changed := range []string{
		strings.Replace(string(paused), "reviewed", "changed", 1),
		strings.Replace(string(paused), "service-id", "different-id", 1),
		strings.Replace(string(paused), `"Replicas":0`, `"Replicas":2`, 1),
		`[]`,
	} {
		if samePausedService(saved, []byte(changed)) {
			t.Fatal("accepted a changed or replaced service")
		}
	}
}

func TestPreservationRequiresValuesAndAbsoluteExpiry(t *testing.T) {
	before := []proofRow{{"session", "content", 20_000}, {"persistent", "value", -1}, {"expired", "old", 5_000}}
	after := []proofRow{{"session", "content", 20_000}, {"persistent", "value", -1}}
	if err := preserved(before, after, 10_000); err != nil {
		t.Fatal(err)
	}
	for _, changed := range [][]proofRow{
		{{"session", "different", 20_000}, {"persistent", "value", -1}},
		{{"session", "content", 19_000}, {"persistent", "value", -1}},
		{{"session", "content", 20_000}},
		{{"persistent", "value", -1}},
	} {
		if err := preserved(before, changed, 10_000); err == nil {
			t.Fatal("accepted changed or missing unexpired data")
		}
	}
}

func TestParsePreservationProof(t *testing.T) {
	rows, err := parseProof([]byte(`[["hashed-key","hashed-value",20000]]`))
	if err != nil || len(rows) != 1 || rows[0] != (proofRow{"hashed-key", "hashed-value", 20_000}) {
		t.Fatal("valid proof rejected")
	}
	for _, invalid := range []string{`not-json`, `[["key"]]`, `[["key","value","bad"]]`, `{"error":"NOAUTH"}`} {
		if _, err := parseProof([]byte(invalid)); err == nil {
			t.Fatal("accepted invalid Redis proof")
		}
	}
}
