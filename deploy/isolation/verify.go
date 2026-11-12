package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
)

func preflightNetworks() error {
	services, err := allServices()
	if err != nil {
		return err
	}
	ids := map[string]string{}
	expected := map[string][]string{
		"portfolio_network": {"perry-infra_traefik", "perry-infra_postgres", "fleet_fleet-api", "chinwag_chinwag-api", "vita-track_vita-track-api", "im-listening_api"},
		"redis_clients":     {"perry-infra_redis", "im-listening_api"},
	}
	for name := range expected {
		n, err := inspectNetwork(name)
		if err != nil {
			return err
		}
		ids[name] = n.ID
	}
	out, err := docker("network", "ls", "-q")
	if err != nil {
		return err
	}
	data, err := docker(append([]string{"network", "inspect"}, strings.Fields(string(out))...)...)
	if err != nil {
		return err
	}
	var networks []network
	if json.Unmarshal(data, &networks) != nil {
		return errors.New("cannot inventory existing network subnets")
	}
	for _, want := range overlays {
		planned, _ := netip.ParsePrefix(want.Subnet)
		for _, existing := range networks {
			if existing.Name == want.Name {
				if err := checkNetwork(existing, want); err != nil {
					return err
				}
				ids[want.Name] = existing.ID
				expected[want.Name] = nil
				continue
			}
			for _, subnet := range existing.IPAM.Config {
				prefix, err := netip.ParsePrefix(subnet.Subnet)
				if err != nil {
					continue
				}
				if planned.Overlaps(prefix) {
					return errors.New("planned isolation subnet overlaps existing network " + existing.Name)
				}
			}
		}
	}
	return members(services, ids, expected)
}

type probe struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}
type clientOptions struct {
	App      string  `json:"app"`
	Database bool    `json:"database"`
	Redis    bool    `json:"redis"`
	Final    bool    `json:"final"`
	Denied   []probe `json:"denied"`
	Metrics  []probe `json:"metrics"`
}

func privatePorts(s service) error {
	endpoint, _ := s.Spec["EndpointSpec"].(map[string]any)
	ports, _ := endpoint["Ports"].([]any)
	if len(ports) != 0 {
		return errors.New("unexpected published application/database/Redis port")
	}
	return nil
}

func members(services []service, networks map[string]string, expected map[string][]string) error {
	for name, want := range expected {
		var found []string
		for _, s := range services {
			rows, _ := task(s)["Networks"].([]any)
			for _, row := range rows {
				if row.(map[string]any)["Target"] == networks[name] {
					found = append(found, s.Spec["Name"].(string))
				}
			}
		}
		slices.Sort(found)
		sorted := append([]string{}, want...)
		slices.Sort(sorted)
		if !slices.Equal(found, sorted) {
			return fmt.Errorf("unexpected membership on %s: %v", name, found)
		}
	}
	return nil
}

func allServices() ([]service, error) {
	out, err := docker("service", "ls", "-q")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return nil, errors.New("no Swarm services found")
	}
	data, err := docker(append([]string{"service", "inspect"}, ids...)...)
	var services []service
	if err != nil || decode(data, &services) != nil {
		return nil, errors.New("cannot inspect service membership")
	}
	return services, nil
}

func verifyTopology() error {
	services, err := allServices()
	if err != nil {
		return err
	}
	ids := map[string]string{}
	expected := map[string][]string{}
	for _, want := range overlays {
		n, err := inspectNetwork(want.Name)
		if err != nil {
			return err
		}
		if err := checkNetwork(n, want); err != nil {
			return err
		}
		ids[want.Name] = n.ID
	}
	for _, app := range apps {
		expected[app.Ingress] = []string{app.Service, "perry-infra_traefik"}
		if app.Database != "" {
			expected[app.Database] = []string{app.Service, "perry-infra_postgres"}
		}
		if app.Redis != "" {
			expected[app.Redis] = []string{app.Service, "perry-infra_redis"}
		}
		s, err := inspect(app.Service)
		if err != nil {
			return err
		}
		nets, err := networkNames(s)
		if err != nil {
			return err
		}
		want := desired(app)
		slices.Sort(want)
		if !slices.Equal(nets, want) {
			return errors.New("unexpected application network attachment: " + app.Name)
		}
		labels, _ := s.Spec["Labels"].(map[string]any)
		if labels["traefik.swarm.network"] != app.Ingress {
			return errors.New("Traefik network is not explicitly selected: " + app.Name)
		}
		if err := privatePorts(s); err != nil {
			return err
		}
	}
	for _, name := range []string{"perry-infra_postgres", "perry-infra_redis"} {
		s, err := inspect(name)
		if err != nil {
			return err
		}
		if err := privatePorts(s); err != nil {
			return err
		}
	}
	for _, old := range []string{"portfolio_network", "redis_clients"} {
		if n, err := inspectNetwork(old); err == nil {
			ids[old] = n.ID
			expected[old] = nil
		}
	}
	if err := members(services, ids, expected); err != nil {
		return err
	}
	// Shared services must not retain an unrelated client or control overlay.
	infra := map[string][]string{
		"perry-infra_traefik":  {"bulkbets_ingress", "perry-infra_docker_control", "perry-infra_traefik_metrics", "chinwag_ingress", "fleet_ingress", "im_listening_ingress", "vita_track_ingress"},
		"perry-infra_postgres": {"bulkbets_database", "perry-infra_postgres_metrics", "chinwag_database", "im_listening_database", "vita_track_database"},
		"perry-infra_redis":    {"perry-infra_redis_metrics", "im_listening_redis_clients"},
	}
	for name, want := range infra {
		s, err := inspect(name)
		if err != nil {
			return err
		}
		nets, err := networkNames(s)
		if err != nil {
			return err
		}
		slices.Sort(want)
		if !slices.Equal(nets, want) {
			return errors.New("unexpected shared service network attachment: " + name)
		}
	}
	fmt.Println("Encrypted overlays, exact service memberships, selected Traefik networks and unpublished data ports passed.")
	return nil
}

// Probe VIPs and task addresses. DNS failure alone is not proof of isolation.
func addresses(s service) ([]string, error) {
	return addressesExcept(s, "")
}

func addressesExcept(s service, excludedNetwork string) ([]string, error) {
	var addresses []string
	for _, vip := range s.Endpoint.VirtualIPs {
		if excludedNetwork != "" && vip.NetworkID == excludedNetwork {
			continue
		}
		ip, _, err := net.ParseCIDR(vip.Addr)
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, ip.String())
	}
	tasks, err := docker("service", "ps", "-q", "--no-trunc", "--filter", "desired-state=running", s.ID)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(tasks))
	if len(ids) != 1 {
		return nil, errors.New("expected exactly one running task for a network probe")
	}
	data, err := docker("inspect", "--type", "task", ids[0])
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Status              struct{ State string }
		NetworksAttachments []struct {
			Network   struct{ ID string }
			Addresses []string
		}
	}
	if json.Unmarshal(data, &rows) != nil || len(rows) != 1 || rows[0].Status.State != "running" {
		return nil, errors.New("cannot inspect running task addresses")
	}
	for _, network := range rows[0].NetworksAttachments {
		if network.Network.ID == "" {
			return nil, errors.New("network probe task is missing its overlay identity")
		}
		if excludedNetwork != "" && network.Network.ID == excludedNetwork {
			continue
		}
		for _, addr := range network.Addresses {
			ip, _, err := net.ParseCIDR(addr)
			if err != nil {
				return nil, err
			}
			addresses = append(addresses, ip.String())
		}
	}
	slices.Sort(addresses)
	addresses = slices.Compact(addresses)
	if len(addresses) == 0 {
		return nil, errors.New("network probe has no concrete target address")
	}
	return addresses, nil
}

func verifyClient(app application, final bool) error {
	options := clientOptions{App: app.Name, Database: app.Database != "", Redis: app.Redis != "", Final: final}
	for _, other := range apps {
		if other.Name == app.Name {
			continue
		}
		s, err := inspect(other.Service)
		if err != nil {
			return err
		}
		ips, err := addresses(s)
		if err != nil {
			return err
		}
		for _, ip := range ips {
			options.Denied = append(options.Denied, probe{ip, other.Port})
		}
	}
	// The Docker API and metrics listeners are outside every application's
	// authorized networks. Test their known listeners, rather than closed ports.
	for name, port := range map[string]int{"perry-infra_docker-socket-proxy": 2375, "perry-infra_traefik": 8082} {
		s, err := inspect(name)
		if err != nil {
			return err
		}
		ips, err := addresses(s)
		if err != nil {
			return err
		}
		for _, ip := range ips {
			if name == "perry-infra_traefik" {
				options.Metrics = append(options.Metrics, probe{ip, port})
			} else {
				options.Denied = append(options.Denied, probe{ip, port})
			}
		}
	}
	{
		s, err := inspect("perry-infra_postgres")
		if err != nil {
			return err
		}
		authorized := ""
		if app.Database != "" {
			n, err := inspectNetwork(app.Database)
			if err != nil {
				return err
			}
			authorized = n.ID
		}
		ips, err := addressesExcept(s, authorized)
		if err != nil {
			return err
		}
		for _, ip := range ips {
			options.Denied = append(options.Denied, probe{ip, 5432})
		}
	}
	{
		s, err := inspect("perry-infra_redis")
		if err != nil {
			return err
		}
		authorized := ""
		if app.Redis != "" {
			n, err := inspectNetwork(app.Redis)
			if err != nil {
				return err
			}
			authorized = n.ID
		}
		ips, err := addressesExcept(s, authorized)
		if err != nil {
			return err
		}
		for _, ip := range ips {
			options.Denied = append(options.Denied, probe{ip, 6379})
		}
	}
	s, err := inspect(app.Service)
	if err != nil {
		return err
	}
	out, err := docker("ps", "-q", "--filter", "label=com.docker.swarm.service.name="+app.Service)
	if err != nil {
		return err
	}
	ids := strings.Fields(string(out))
	if len(ids) != 1 {
		return errors.New("expected one client container")
	}
	if err := wait(s); err != nil {
		return err
	}
	config, _ := json.Marshal(options)
	result, err := command(strings.NewReader(clientScript), "/usr/bin/docker", "exec", "-i", ids[0], "node", "--input-type=module", "-", string(config))
	if err != nil {
		return errors.New("application network/database/Redis probe failed for " + app.Name + "; inspect privately, retain rollback inputs")
	}
	fmt.Print(string(result)) // Embedded verifier emits fixed labels, never values.
	return nil
}

func verify() error {
	if err := verifyTopology(); err != nil {
		return err
	}
	if err := health(); err != nil {
		return err
	}
	for _, app := range apps {
		if err := verifyClient(app, true); err != nil {
			return err
		}
	}
	return nil
}
