package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCutoverRetainsCredentialsMountsAndOtherRuntimePermissions(t *testing.T) {
	fixture := []byte(`{"ID":"service-id","Spec":{"Name":"im-listening_api","Labels":{"traefik.swarm.network":"portfolio_network","routing":"retained"},"TaskTemplate":{"ContainerSpec":{"Image":"repo@sha256:abc","Secrets":[{"SecretID":"retained-secret-id"}],"Configs":[{"ConfigID":"retained-config-id"}],"Env":["EXAMPLE=retained"],"Mounts":[{"Source":"retained-volume"}],"CapabilityDrop":["ALL"],"ReadOnly":true},"Networks":[{"Target":"old-shared-network-id"}],"Resources":{"Limits":{"MemoryBytes":402653184}}}}}`)
	for _, app := range apps {
		var s service
		if decode(fixture, &s) != nil {
			t.Fatal("fixture decode")
		}
		before, _ := json.Marshal(containerSpec(s))
		resources, _ := json.Marshal(task(s)["Resources"])
		if err := setNetworksWith(s, desired(app), "api", func(name string) (network, error) { return network{ID: name + "-id"}, nil }); err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(containerSpec(s))
		newResources, _ := json.Marshal(task(s)["Resources"])
		if !bytes.Equal(before, after) || !bytes.Equal(resources, newResources) {
			t.Fatal("cutover changed credentials, mounts or resource limits")
		}
		rows := task(s)["Networks"].([]any)
		if len(rows) != len(desired(app)) {
			t.Fatal("cutover retained an old network")
		}
		for index, name := range desired(app) {
			if rows[index].(map[string]any)["Target"] != name+"-id" {
				t.Fatal("cutover selected another app's network")
			}
		}
	}
}

func TestRejectWeakOrWrongNetworks(t *testing.T) {
	want := overlays[1]
	n := network{Name: want.Name, ID: "network-id", Driver: "overlay", Scope: "swarm", Internal: true, Options: map[string]string{"encrypted": ""}}
	n.IPAM.Config = append(n.IPAM.Config, struct{ Subnet string }{want.Subnet})
	if err := checkNetwork(n, want); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*network){
		func(n *network) { n.Attachable = true }, func(n *network) { n.Internal = false }, func(n *network) { n.Ingress = true },
		func(n *network) { n.Options = nil }, func(n *network) { n.Driver = "bridge" }, func(n *network) { n.Scope = "local" },
		func(n *network) { n.Name = "other-app" }, func(n *network) { n.IPAM.Config = nil },
	} {
		bad := n
		change(&bad)
		if checkNetwork(bad, want) == nil {
			t.Fatal("accepted a network that broadens access or differs from the reviewed plan")
		}
	}
}

func TestHealthyOldTaskCannotSatisfyNetworkConvergence(t *testing.T) {
	var s service
	if decode([]byte(`{"ID":"service-id","Spec":{"TaskTemplate":{"ContainerSpec":{"Image":"repo@sha256:abc"},"Networks":[{"Target":"new-private-overlay"}]}}}`), &s) != nil {
		t.Fatal("decode")
	}
	task := taskRecord{ServiceID: s.ID, Spec: map[string]any{"ContainerSpec": map[string]any{"Image": "repo@sha256:abc"}, "Networks": []any{map[string]any{"Target": "old-shared-overlay"}}}}
	task.Status.State = "running"
	if taskMatches(s, task) {
		t.Fatal("healthy old same-image task satisfied a network update")
	}
	task.Spec["Networks"] = []any{map[string]any{"Target": "new-private-overlay"}}
	if !taskMatches(s, task) {
		t.Fatal("expected current healthy task")
	}
	task.Status.State = "starting"
	if taskMatches(s, task) {
		t.Fatal("starting task was treated as converged")
	}
}

func TestUnexpectedServiceMembershipFails(t *testing.T) {
	makeService := func(name string) service {
		return service{Spec: map[string]any{"Name": name, "TaskTemplate": map[string]any{"Networks": []any{map[string]any{"Target": "private-id"}}}}}
	}
	allowed := []service{makeService("app"), makeService("traefik")}
	ids := map[string]string{"private": "private-id"}
	expected := map[string][]string{"private": {"app", "traefik"}}
	if err := members(allowed, ids, expected); err != nil {
		t.Fatal(err)
	}
	if members(append(allowed, makeService("unapproved-app")), ids, expected) == nil {
		t.Fatal("accepted an unapproved peer")
	}
	if members(allowed[:1], ids, expected) == nil {
		t.Fatal("accepted a missing required peer")
	}
	if members(allowed, ids, map[string][]string{"private": nil}) == nil {
		t.Fatal("accepted an occupied retired overlay")
	}
}

func TestRetainedSpecificationsDetectUnrelatedChanges(t *testing.T) {
	data := []byte(`{"ID":"service-id","Spec":{"Name":"app","TaskTemplate":{"ForceUpdate":9007199254740993,"ContainerSpec":{"Image":"repo@sha256:abc","Env":["EXAMPLE=private"]}}}}`)
	var before, after service
	if decode(data, &before) != nil || decode(data, &after) != nil {
		t.Fatal("fixture decode failed")
	}
	if !sameSpec(before, after) {
		t.Fatal("identical retained specs differ")
	}
	encoded, _ := before.Spec["TaskTemplate"].(map[string]any)["ForceUpdate"].(interface{ String() string })
	if encoded.String() != "9007199254740993" {
		t.Fatal("lost Docker integer precision")
	}
	containerSpec(after)["Env"] = []any{"EXAMPLE=changed"}
	if sameSpec(before, after) {
		t.Fatal("ignored unrelated environment change")
	}
	if decode(data, &after) != nil {
		t.Fatal("decode")
	}
	after.ID = "replacement-service"
	if sameSpec(before, after) {
		t.Fatal("accepted replaced service")
	}
}

func TestFinalManifestMatchesReviewedTopology(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.stack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "portfolio_network") || strings.Contains(text, "--providers.swarm.network=") {
		t.Fatal("final snapshot retains the shared application overlay/default")
	}
	if strings.Contains(text, "[redis_clients,") || strings.Contains(text, "  redis_clients:") {
		t.Fatal("final snapshot retains the shared Redis client overlay")
	}
	for _, network := range overlays {
		if !strings.Contains(text, "  "+network.Name+":\n    external: true") {
			t.Fatalf("missing reviewed external overlay %s", network.Name)
		}
	}
	if !strings.Contains(text, "source: postgres_hba_isolation_v3") {
		t.Fatal("HBA Docker config was not versioned")
	}
	if !strings.Contains(text, "networks: [im_listening_redis_clients, redis_metrics]") {
		t.Fatal("shared Redis has the wrong client/monitoring overlays")
	}
	// These files already belong to the infrastructure fingerprint and installer.
	broker, err := os.ReadFile("../infrastructure/main.go")
	if err != nil {
		t.Fatal(err)
	}
	installer, err := os.ReadFile("../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docker-compose.stack.yaml", "postgres/pg_hba.conf"} {
		if !strings.Contains(string(broker), `"`+name+`"`) || !strings.Contains(string(installer), name) {
			t.Fatalf("runtime file is not fingerprinted/installed: %s", name)
		}
	}
}

func TestPrivateServicesCannotPublishPorts(t *testing.T) {
	s := service{Spec: map[string]any{"EndpointSpec": map[string]any{"Ports": []any{map[string]any{"PublishedPort": 5432}}}}}
	if privatePorts(s) == nil {
		t.Fatal("accepted published database port")
	}
	s.Spec["EndpointSpec"] = map[string]any{}
	if err := privatePorts(s); err != nil {
		t.Fatal(err)
	}
}
