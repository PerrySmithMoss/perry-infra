package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type healthTransport func(*http.Request) (*http.Response, error)

func (f healthTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type healthBody struct{ closed bool }

func (*healthBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *healthBody) Close() error           { b.closed = true; return nil }

func TestHealthWaitsForEveryRouteToRecover(t *testing.T) {
	for _, statuses := range [][]int{{http.StatusOK}, {http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusNoContent, 0, http.StatusOK}} {
		name := "already ready"
		if len(statuses) > 1 {
			name = "routing and connection failures recover"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := map[string]int{}
				var bodies []*healthBody
				client := &http.Client{Transport: healthTransport(func(req *http.Request) (*http.Response, error) {
					calls[req.URL.Path]++
					status := http.StatusOK
					if req.URL.Path == "/recovering" {
						status = statuses[calls[req.URL.Path]-1]
					}
					if status == 0 {
						return nil, errors.New("connection refused")
					}
					body := &healthBody{}
					bodies = append(bodies, body)
					return &http.Response{StatusCode: status, Body: body}, nil
				})}
				ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
				defer cancel()
				started := time.Now()
				urls := []string{"https://health.test/ready", "https://health.test/recovering", "https://health.test/last"}
				if err := waitForHealth(ctx, client, urls, healthRetryInterval); err != nil {
					t.Fatal(err)
				}
				if calls["/ready"] != len(statuses) || calls["/recovering"] != len(statuses) || calls["/last"] != 1 {
					t.Fatalf("did not require a successful sweep of every route: %v", calls)
				}
				if elapsed := time.Since(started); elapsed != time.Duration(len(statuses)-1)*healthRetryInterval {
					t.Fatalf("readiness retry timing = %s", elapsed)
				}
				for _, body := range bodies {
					if !body.closed {
						t.Fatal("health response body was not closed")
					}
				}
			})
		})
	}
}

func TestHealthFailureStopsAtSharedDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
		defer cancel()
		calls := 0
		client := &http.Client{Transport: healthTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		started := time.Now()
		err := waitForHealth(ctx, client, []string{"https://health.test/unavailable"}, healthRetryInterval)
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "HTTP 503: https://health.test/unavailable") {
			t.Fatalf("missing deadline or failed route diagnostic: %v", err)
		}
		if calls < 2 || time.Since(started) != healthTimeout {
			t.Fatalf("health failure did not retry within its deadline: calls=%d elapsed=%s", calls, time.Since(started))
		}
	})
}

func TestHealthRequestTimeoutsRetryWithinSharedDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
		defer cancel()
		calls := 0
		client := &http.Client{Timeout: 15 * time.Second, Transport: healthTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			<-req.Context().Done()
			return nil, req.Context().Err()
		})}
		started := time.Now()
		err := waitForHealth(ctx, client, []string{"https://health.test/stalled"}, healthRetryInterval)
		if !errors.Is(err, context.DeadlineExceeded) || calls < 2 || time.Since(started) != healthTimeout {
			t.Fatalf("stalled requests exceeded the readiness budget: calls=%d elapsed=%s err=%v", calls, time.Since(started), err)
		}
	})
}

func TestCanceledHealthCheckDoesNotStartRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: healthTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("started a request after cancellation")
		return nil, nil
	})}
	if err := waitForHealth(ctx, client, []string{"https://health.test/ready"}, healthRetryInterval); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}
