package containd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetHealthSuccess decodes the documented management health body.
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

func TestHealthDiagnosticsPreservedWithoutGatingManagement(t *testing.T) {
	for _, state := range []string{"inactive", "active", "pending", "failed", "unknown"} {
		t.Run(state, func(t *testing.T) {
			diagnostics := map[string]any{
				"managementRecovery": map[string]any{"state": state, "reason": "stored configuration refused"},
				"enforcement":        map[string]any{"state": "unavailable", "reason": "recovery holds"},
				"icsEnforcement":     map[string]any{"state": "partial", "enforcing": float64(1), "partial": float64(2), "notEnforcing": float64(3), "monitoring": float64(4)},
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := map[string]any{"status": "ok", "build": MinimumVersion}
				for key, value := range diagnostics {
					body[key] = value
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer srv.Close()
			client := newTestClient(srv.URL)
			health, err := client.GetHealth(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			// The server embeds HealthStatus; its JSON must retain all diagnostic
			// fields and coverage counts without translating status=ok to failure.
			data, err := json.Marshal(health)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			for key, want := range diagnostics {
				if !reflect.DeepEqual(got[key], want) {
					t.Fatalf("%s = %#v, want %#v", key, got[key], want)
				}
			}
			if !client.IsAvailable(t.Context()) {
				t.Fatal("operational diagnostic blocked management availability")
			}
			if err := client.WaitReady(t.Context(), time.Second); err != nil {
				t.Fatalf("operational diagnostic blocked a repairing seed: %v", err)
			}
		})
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
		{"0.1.32", false},
		{"0.1.34", false},
		{"0.1.35", false},
		{"v0.1.35+source", false},
		{"0.1.35-rc.1", false},
		{"0.1.37", true},
		{"v0.1.37+source", true},
		{"0.1.38-rc.1", true},
		{"0.1.36", false},
		{"v0.1.36+source", false},
		{"0.1.36-rc.1", false},
		{"0.1.37-rc.1", false},
		{"1.0.0-dev", true},
		{"dev", false},
		{"", false},
		// Malformed versions must fail parsing, not merely fall below the floor.
		{"0.01.35", false},
		{"0.1.36-01", false},
		{"0.1.36-alpha.0", false},
		{"0.1.37-alpha.0", false},
		{"0.1.9999999999999999999999999", true},
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
