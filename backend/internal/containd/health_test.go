package containd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetHealthSuccess verifies parsing a healthy response from containd.
func TestGetHealthSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(HealthStatus{
			Status:    "healthy",
			Version:   "1.0.0",
			Uptime:    3600,
			Zones:     4,
			Sessions:  12,
			EventRate: 5,
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	health, err := client.GetHealth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("expected healthy, got %s", health.Status)
	}
	if health.Zones != 4 {
		t.Errorf("expected 4 zones, got %d", health.Zones)
	}
}

// TestGetHealthUnreachable verifies error handling when containd is down.
func TestGetHealthUnreachable(t *testing.T) {
	client := newTestClient("http://127.0.0.1:1") // nothing listening
	_, err := client.GetHealth()
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
	_, err := client.GetHealth()
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// TestIsAvailable verifies the availability check.
func TestIsAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(HealthStatus{Status: "healthy"})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	if !client.IsAvailable() {
		t.Error("expected available with healthy response")
	}

	// Unreachable server
	client2 := newTestClient("http://127.0.0.1:1")
	if client2.IsAvailable() {
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
		json.NewEncoder(w).Encode(HealthStatus{Status: "healthy"})
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
