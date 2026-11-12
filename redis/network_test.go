package main

import (
	"errors"
	"testing"
)

func TestVerificationFollowsOnlyTheActualRedisClientNetwork(t *testing.T) {
	var api, redis serviceSnapshot
	api.Spec.TaskTemplate.Networks = append(api.Spec.TaskTemplate.Networks, struct{ Target string }{"new"})
	redis.Spec.TaskTemplate.Networks = append(redis.Spec.TaskTemplate.Networks, struct{ Target string }{"new"}, struct{ Target string }{"old"})
	run := func(args ...string) ([]byte, error) {
		id := "new"
		if args[2] == "redis_clients" {
			id = "old"
		}
		return []byte(`[{"ID":"` + id + `","Driver":"overlay","Internal":true,"Attachable":false,"Options":{"encrypted":""}}]`), nil
	}
	if got, err := verificationNetwork(run, api, redis); err != nil || got != "im_listening_redis_clients" {
		t.Fatalf("network = %q, %v", got, err)
	}
	api.Spec.TaskTemplate.Networks = append(api.Spec.TaskTemplate.Networks, struct{ Target string }{"old"})
	if _, err := verificationNetwork(run, api, redis); err == nil {
		t.Fatal("accepted ambiguous shared-client attachments")
	}
	if _, err := verificationNetwork(func(...string) ([]byte, error) { return nil, errors.New("absent") }, api, redis); err == nil {
		t.Fatal("accepted missing overlays")
	}
	if _, err := verificationNetwork(func(...string) ([]byte, error) {
		return []byte(`[{"ID":"new","Driver":"overlay","Internal":true,"Attachable":true,"Options":{"encrypted":""}}]`), nil
	}, api, redis); err == nil {
		t.Fatal("accepted an attachable Redis overlay")
	}
}
