package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestUpstreamFailureStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"deadline", fmt.Errorf("control centre unreachable: %w", context.DeadlineExceeded), http.StatusGatewayTimeout},
		{"net timeout", fmt.Errorf("wrapped: %w", timeoutError{}), http.StatusGatewayTimeout},
		{"refused", errors.New("connection refused"), http.StatusBadGateway},
		{"bad status", &upstreamError{service: "control centre", status: 500, body: []byte("boom")}, http.StatusBadGateway},
	}
	for _, test := range tests {
		if got := upstreamFailureStatus(test.err); got != test.want {
			t.Errorf("%s: status = %d, want %d", test.name, got, test.want)
		}
	}
}

// TestCallUpstreamTimesOut checks that a slow service ends in a deadline
// error, which the routes answer with 504.
func TestCallUpstreamTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	_, _, err := callUpstream(context.Background(), http.MethodGet, srv.URL, nil, 50*time.Millisecond)
	if err == nil || upstreamFailureStatus(err) != http.StatusGatewayTimeout {
		t.Fatalf("callUpstream error = %v, want a timeout", err)
	}
}

func TestFetchUpstreamObjectRejectsNonObjects(t *testing.T) {
	for name, body := range map[string]string{"array": `[1]`, "empty": ``, "broken": `{"a":`} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			if _, err := fetchUpstreamObject(context.Background(), "control centre", srv.URL); err == nil {
				t.Errorf("fetchUpstreamObject(%q) = nil error", body)
			}
		})
	}
}
