package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDeniedRequestsNeverReachDocker(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write([]byte(`{}`)) }))
	defer upstream.Close()
	client := upstream.Client()
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(upstream.URL, "http://")
		return http.DefaultTransport.RoundTrip(r)
	})
	handler := proxyHandler(client)
	for _, test := range []struct{ method, path, body string }{
		{"POST", "/containers/create", `{"Image":"evil"}`}, {"DELETE", "/containers/abc", ""}, {"PUT", "/plugins/create", ""}, {"PATCH", "/info", ""},
		{"GET", "/secrets", ""}, {"GET", "/configs", ""}, {"GET", "/services", ""}, {"GET", "/events", ""}, {"GET", "/containers/abc/logs", ""}, {"GET", "/containers/abc/archive?path=/etc/passwd", ""},
		{"GET", "/v1.52/containers/create", ""}, {"GET", "/v1.52/../info", ""}, {"GET", "//info", ""}, {"GET", "/%69nfo", ""}, {"GET", "/v1.52/%63ontainers/json", ""},
		{"GET", "/info?x=1", ""}, {"GET", "/images/json?all=true", ""}, {"GET", "/containers/json?size=true", ""}, {"GET", "/containers/json?size=1", ""},
		{"GET", "/containers/json?all=1&all=0", ""}, {"GET", "/containers/json?filters=%7B%22label%22%3A%5B%22secret%22%5D%7D", ""},
		{"GET", "/containers/json?filters=%7B%22health%22%3A%5B%22bad%22%5D%7D", ""}, {"GET", "/containers/json?filters=%7B", ""},
		{"GET", "/info", "body"}, {"HEAD", "/containers/json", ""}, {"CONNECT", "/info", ""},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 403 {
				t.Fatalf("status=%d", response.Code)
			}
		})
	}
	request := httptest.NewRequest("GET", "/info", nil)
	request.TransferEncoding = []string{"chunked"}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("accepted a streamed request body")
	}
	if calls.Load() != 0 {
		t.Fatal("a denied operation reached Docker")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAllowedResponsesAreProjectedAndCallerHeadersDiscarded(t *testing.T) {
	id := strings.Repeat("a", 64)
	fixtures := map[string]string{
		"/version":                    `{"Version":"29.6.2","ApiVersion":"1.52","MinAPIVersion":"1.44","Os":"linux","Arch":"amd64","extra":"secret-marker"}`,
		"/info":                       `{"ContainersRunning":10,"ContainersPaused":0,"ContainersStopped":12,"DockerRootDir":"secret-marker","RegistryConfig":{"secret":"secret-marker"}}`,
		"/images/json":                `[{"Id":"sha256:fixture","Size":123,"Labels":{"password":"secret-marker"},"RepoTags":["secret-marker"]}]`,
		"/containers/json":            `[{"Id":"fixture","Names":["/app"],"Image":"image","ImageID":"sha256:fixture","State":"running","Status":"Up (healthy)","Created":1,"Command":"secret-marker","Mounts":["secret-marker"],"NetworkSettings":{"secret":"secret-marker"},"Labels":{"auth":"secret-marker","netdata.cloud/ignore":"yes"}}]`,
		"/containers/" + id + "/json": `{"Name":"/app","Config":{"Image":"image","Env":["PASSWORD=secret-marker"],"Labels":{"auth":"secret-marker"},"Cmd":["secret-marker"]},"HostConfig":{"Binds":["secret-marker"]},"Mounts":["secret-marker"]}`,
	}
	for path, fixture := range fixtures {
		t.Run(path, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("X-Registry-Auth") != "" || r.Header.Get("Connection") != "" || r.URL.Host != "docker" {
					t.Fatal("forwarded caller metadata")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Secret": []string{"secret-marker"}}, Body: io.NopCloser(strings.NewReader(fixture))}, nil
			})}
			request := httptest.NewRequest("GET", "/v1.52"+path, nil)
			request.Header.Set("Authorization", "secret-marker")
			request.Header.Set("X-Registry-Auth", "secret-marker")
			request.Header.Set("Connection", "upgrade")
			response := httptest.NewRecorder()
			proxyHandler(client).ServeHTTP(response, request)
			if response.Code != 200 || strings.Contains(response.Body.String(), "secret-marker") || response.Header().Get("X-Secret") != "" {
				t.Fatalf("unfiltered response: %d %s", response.Code, response.Body.String())
			}
			var doc any
			if json.Unmarshal(response.Body.Bytes(), &doc) != nil {
				t.Fatal("invalid projected JSON")
			}
			if path == "/containers/json" && !strings.Contains(response.Body.String(), `"netdata.cloud/ignore":"true"`) {
				t.Fatal("lost safe ignore behavior")
			}
		})
	}
}

func TestPingNegotiationAndSafeQueries(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Api-Version": []string{"1.52"}}, Body: io.NopCloser(strings.NewReader("OK"))}, nil
		})}
		response := httptest.NewRecorder()
		proxyHandler(client).ServeHTTP(response, httptest.NewRequest(method, "/_ping", nil))
		if response.Code != 200 || response.Header().Get("API-Version") != "1.52" {
			t.Fatal("lost Docker version negotiation")
		}
		if method == "HEAD" && response.Body.Len() != 0 {
			t.Fatal("HEAD returned a body")
		}
	}
	for _, path := range []string{"/containers/json?all=1&filters=%7B%22health%22%3A%5B%22none%22%5D%7D", "/containers/json?all=1&filters=%7B%22health%22%3A%7B%22healthy%22%3Atrue%7D%7D", "/containers/json?all=true&size=false", "/containers/" + strings.Repeat("b", 12) + "/json"} {
		if _, _, ok := permitted(httptest.NewRequest("GET", path, nil)); !ok {
			t.Fatal("denied a required safe read")
		}
	}
}

func TestDockerFailuresAndMalformedResponsesDoNotLeak(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
	}{{500, "secret-marker"}, {302, "secret-marker"}, {200, "invalid-secret-marker"}, {200, strings.Repeat("x", (4<<20)+1)}} {
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: test.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body))}, nil
		})}
		response := httptest.NewRecorder()
		proxyHandler(client).ServeHTTP(response, httptest.NewRequest("GET", "/info", nil))
		if response.Code != 502 || strings.Contains(response.Body.String(), "secret-marker") {
			t.Fatal("leaked an upstream failure")
		}
	}
}

func TestRollbackRejectsUnmanagedRemovalPaths(t *testing.T) {
	for _, path := range []string{"/etc/group", "/etc/gshadow", "/var/lib/docker", "/etc/netdata/claim.conf", "/etc/netdata/health.d/perry-private-metrics.conf", infraSnapshot} {
		if restorable(path) {
			t.Fatal("rollback can remove unrelated path", path)
		}
	}
	for _, path := range append(mapTargets(), installedBinary, installRecord) {
		if !restorable(path) {
			t.Fatal("missing managed rollback path")
		}
	}
}
