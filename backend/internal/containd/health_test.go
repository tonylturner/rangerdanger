package containd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetHealthSuccess decodes the body containd's healthHandler
// (api/http/server.go) writes.
func TestGetHealthSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		io.WriteString(w, `{"build":"v0.1.40","component":"mgmt","status":"ok","time":"2026-10-09T12:34:56.789Z"}`)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	health, err := client.GetHealth(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := HealthStatus{
		Status:    "ok",
		Component: "mgmt",
		Build:     "v0.1.40",
		Time:      time.Date(2026, 10, 9, 12, 34, 56, 789000000, time.UTC),
	}
	if !health.Time.Equal(want.Time) {
		t.Errorf("time: got %v, want %v", health.Time, want.Time)
	}
	health.Time, want.Time = time.Time{}, time.Time{}
	if *health != want {
		t.Errorf("health: got %+v, want %+v", *health, want)
	}
}

// TestGetHealthUnreachable verifies error handling when containd is down.
func TestGetHealthUnreachable(t *testing.T) {
	client := newTestClient("http://127.0.0.1:1") // nothing listening
	_, err := client.GetHealth(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

// TestGetHealthNon200 verifies error handling for non-200 responses.
func TestGetHealthNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GetHealth(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// TestIsAvailable verifies the availability check.
func TestIsAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(HealthStatus{Status: "ok", Build: MinimumVersion})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	if !client.IsAvailable(context.Background()) {
		t.Error("expected available with healthy response")
	}

	// Unreachable server
	client2 := newTestClient("http://127.0.0.1:1")
	if client2.IsAvailable(context.Background()) {
		t.Error("expected unavailable for unreachable server")
	}
}

// TestWaitReadySuccess verifies the polling loop succeeds when containd comes up.
func TestWaitReadySuccess(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(HealthStatus{Status: "ok", Build: MinimumVersion})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.WaitReady(context.Background(), 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&callCount) < 3 {
		t.Error("expected at least 3 health check attempts")
	}
}

// TestWaitReadyTimeout verifies WaitReady returns error on timeout.
func TestWaitReadyTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.WaitReady(context.Background(), 3*time.Second)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitReadyVersionGate(t *testing.T) {
	for _, tc := range []struct {
		build   string
		allowed bool
	}{
		{"0.1.32", false}, {"0.1.33", true}, {"v0.1.33+source", true}, {"0.1.33-rc.1", false}, {"0.1.34-rc.1", true}, {"1.0.0-dev", true}, {"dev", false}, {"", false}, {"0.01.33", false}, {"0.1.34-01", false}, {"0.1.34-alpha.0", true}, {"0.1.9999999999999999999999999", true},
	} {
		t.Run(tc.build, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"status": "ok", "build": tc.build, "ipv4Forwarding": "disabled", "managementBinding": map[string]any{"state": "unbound-appliance"}})
			}))
			defer srv.Close()
			err := newTestClient(srv.URL).WaitReady(t.Context(), time.Second)
			if (err == nil) != tc.allowed {
				t.Fatalf("build %q: %v", tc.build, err)
			}
			if err != nil && (!strings.Contains(err.Error(), tc.build) || !strings.Contains(err.Error(), MinimumVersion)) {
				t.Fatalf("unhelpful version error: %v", err)
			}
		})
	}
}
