package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const socketPath = "/run/perry-netdata-docker-api.sock"

var versionPrefix = regexp.MustCompile(`^/v[0-9]{1,2}\.[0-9]{1,2}(/.*)$`)
var inspectPath = regexp.MustCompile(`^/containers/([a-f0-9]{12}|[a-f0-9]{64})/json$`)
var apiVersion = regexp.MustCompile(`^[0-9]{1,2}\.[0-9]{1,2}$`)

// Construct requests ourselves. Never forward caller headers, bodies, hosts,
// encoded paths, redirects or arbitrary query options to the privileged socket.
func permitted(r *http.Request) (string, url.Values, bool) {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.URL.RawPath != "" || r.URL.EscapedPath() != r.URL.Path || r.URL.Fragment != "" {
		return "", nil, false
	}
	path := r.URL.Path
	if match := versionPrefix.FindStringSubmatch(path); match != nil {
		path = match[1]
	}
	if r.Method != http.MethodGet && !(r.Method == http.MethodHead && path == "/_ping") {
		return "", nil, false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", nil, false
	}
	for _, values := range query {
		if len(values) != 1 {
			return "", nil, false
		}
	}
	switch {
	case path == "/_ping", path == "/version", path == "/info", path == "/images/json":
		return path, query, len(query) == 0
	case path == "/containers/json":
		for key, values := range query {
			switch key {
			case "all":
				if values[0] != "true" && values[0] != "false" && values[0] != "1" && values[0] != "0" {
					return "", nil, false
				}
			case "size":
				if values[0] != "false" && values[0] != "0" {
					return "", nil, false
				}
			case "filters":
				var filters map[string]json.RawMessage
				if json.Unmarshal([]byte(values[0]), &filters) != nil {
					return "", nil, false
				}
				canonical := map[string][]string{}
				for filter, raw := range filters {
					var options []string
					if json.Unmarshal(raw, &options) != nil {
						var selected map[string]bool
						if json.Unmarshal(raw, &selected) != nil {
							return "", nil, false
						}
						for option, enabled := range selected {
							if !enabled {
								return "", nil, false
							}
							options = append(options, option)
						}
					}
					if filter != "health" || len(options) != 1 {
						return "", nil, false
					}
					switch options[0] {
					case "healthy", "unhealthy", "starting", "none":
					default:
						return "", nil, false
					}
					canonical[filter] = options
				}
				encoded, _ := json.Marshal(canonical)
				query.Set("filters", string(encoded))
			default:
				return "", nil, false
			}
		}
		return path, query, true
	case inspectPath.MatchString(path):
		return path, query, len(query) == 0
	default:
		return "", nil, false
	}
}

type dockerInfo struct {
	Running int64 `json:"ContainersRunning"`
	Paused  int64 `json:"ContainersPaused"`
	Stopped int64 `json:"ContainersStopped"`
}
type dockerVersion struct {
	Version string `json:"Version"`
	API     string `json:"ApiVersion"`
	MinAPI  string `json:"MinAPIVersion"`
	OS      string `json:"Os"`
	Arch    string `json:"Arch"`
}
type dockerImage struct {
	ID   string `json:"Id"`
	Size int64  `json:"Size"`
}
type dockerContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
}
type dockerInspect struct {
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

func project(path string, body []byte) (any, error) {
	var value any
	switch {
	case path == "/version":
		value = &dockerVersion{}
	case path == "/info":
		value = &dockerInfo{}
	case path == "/images/json":
		value = &[]dockerImage{}
	case path == "/containers/json":
		value = &[]dockerContainer{}
	case inspectPath.MatchString(path):
		value = &dockerInspect{}
	default:
		return nil, errors.New("unsupported projection")
	}
	if err := json.Unmarshal(body, value); err != nil {
		return nil, err
	}
	// Labels may contain credentials or routing/authentication configuration.
	// Only this boolean affects the Docker collector's existing ignore behavior.
	if containers, ok := value.(*[]dockerContainer); ok {
		for i := range *containers {
			labels := (*containers)[i].Labels
			(*containers)[i].Labels = map[string]string{}
			if strings.EqualFold(labels["netdata.cloud/ignore"], "true") || strings.EqualFold(labels["netdata.cloud/ignore"], "yes") {
				(*containers)[i].Labels["netdata.cloud/ignore"] = "true"
			}
		}
	}
	if inspect, ok := value.(*dockerInspect); ok {
		inspect.Config.Labels = map[string]string{}
	}
	return value, nil
}

func unixClient(path string) *http.Client {
	return &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{Proxy: nil, DisableCompression: true, MaxConnsPerHost: 4, MaxIdleConnsPerHost: 4, ResponseHeaderTimeout: 3 * time.Second,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", path)
			}},
	}
}

func proxyHandler(client *http.Client) http.Handler {
	slots := make(chan struct{}, 8)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query, ok := permitted(r)
		if !ok {
			http.Error(w, "operation denied", http.StatusForbidden)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		upstream, err := http.NewRequestWithContext(r.Context(), r.Method, "http://docker"+r.URL.Path, nil)
		if err != nil {
			http.Error(w, "operation denied", 403)
			return
		}
		upstream.URL.RawQuery = query.Encode()
		upstream.Header.Set("User-Agent", "perry-netdata-docker-api")
		response, err := client.Do(upstream)
		if err != nil {
			http.Error(w, "upstream unavailable", 502)
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			status := http.StatusBadGateway
			if response.StatusCode == 404 && inspectPath.MatchString(path) {
				status = 404
			}
			http.Error(w, "upstream unavailable", status)
			return
		}
		if path == "/_ping" {
			if version := response.Header.Get("API-Version"); apiVersion.MatchString(version) {
				w.Header().Set("API-Version", version)
			}
			w.Header().Set("OSType", "linux")
			if r.Method == http.MethodGet {
				io.WriteString(w, "OK")
			}
			return
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
		if err != nil || len(body) > 4<<20 {
			http.Error(w, "upstream unavailable", 502)
			return
		}
		value, err := project(path, body)
		if err != nil {
			http.Error(w, "upstream unavailable", 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(value)
	})
}
