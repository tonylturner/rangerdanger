package server

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func awaitTraffic(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("traffic worker did not reach barrier")
	}
}

func trafficGenerating(t *testing.T, s *Server) bool {
	t.Helper()
	rec, body := rangeRequest(s, http.MethodGet, "/api/traffic/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return body["generating"].(bool)
}

func waitTrafficExit(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for trafficGenerating(t, s) {
		if time.Now().After(deadline) {
			t.Fatal("traffic still generating")
		}
		time.Sleep(time.Millisecond)
	}
}

func stopTraffic(t *testing.T, s *Server, id, status string) {
	t.Helper()
	rec, body := rangeRequest(s, http.MethodPost, "/api/traffic/stop", `{"run_id":"`+id+`"}`)
	if rec.Code != http.StatusOK || body["status"] != status {
		t.Fatalf("stop = %d: %s, want %s", rec.Code, rec.Body, status)
	}
}

func TestTrafficStopIdle(t *testing.T) {
	s := routedServer(t)
	for i := 0; i < 2; i++ {
		stopTraffic(t, s, "", "stopped")
		stopTraffic(t, s, "completed-run", "stopped")
	}
	if trafficGenerating(t, s) {
		t.Fatal("idle stop started traffic")
	}
	rec, _ := rangeRequest(s, http.MethodPost, "/api/traffic/stop", "{")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed stop = %d", rec.Code)
	}
}

func TestTrafficCancelDuringRunWaitsForWorkerExit(t *testing.T) {
	s := routedServer(t)
	gen := usGeneration(nil)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		if err := gen.Stop(3 * time.Second); err != nil {
			t.Error(err)
		}
	})
	run, err := s.startTrafficRun(gen, []resolvedTraffic{{desc: "blocked exec"}}, 30, func(ctx context.Context, _ resolvedTraffic) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		select {
		case <-release:
		case <-time.After(2 * time.Second):
			t.Error("test did not release draining exec")
		}
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	awaitTraffic(t, entered)
	stopTraffic(t, s, run.id, "stopping")
	awaitTraffic(t, cancelled)
	stopTraffic(t, s, run.id, "stopping")
	if !trafficGenerating(t, s) {
		t.Fatal("stop published idle before exec exited")
	}
	if _, err := s.startTrafficRun(gen, nil, 30, nil); !errors.Is(err, errTrafficBusy) {
		t.Fatalf("new start during drain = %v", err)
	}
	close(release)
	waitTrafficExit(t, s)
	stopTraffic(t, s, run.id, "stopped")
}

func TestTrafficStaleStopCannotCancelNewRun(t *testing.T) {
	s := routedServer(t)
	gen := usGeneration(nil)
	t.Cleanup(func() {
		if err := gen.Stop(3 * time.Second); err != nil {
			t.Error(err)
		}
	})
	// Empty targets exercise the cancellable between-cycle sleep.
	first, err := s.startTrafficRun(gen, nil, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	stopTraffic(t, s, first.id, "stopping")
	waitTrafficExit(t, s)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	second, err := s.startTrafficRun(gen, []resolvedTraffic{{}}, 30, func(ctx context.Context, _ resolvedTraffic) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	awaitTraffic(t, entered)
	if first.id == second.id {
		t.Fatal("run ID reused")
	}
	stopTraffic(t, s, first.id, "stopped")
	stopTraffic(t, s, "", "stopped")
	first.cancel() // Also model an old handler paused after capturing its run.
	select {
	case <-cancelled:
		t.Fatal("stale stop cancelled new run")
	case <-time.After(20 * time.Millisecond):
	}
	_, body := rangeRequest(s, http.MethodGet, "/api/traffic/status", "")
	if body["run_id"] != second.id || body["generating"] != true {
		t.Fatalf("new run status = %v", body)
	}
	stopTraffic(t, s, second.id, "stopping")
	awaitTraffic(t, cancelled)
	waitTrafficExit(t, s)
}

func TestTrafficGenerationStopAndRejectedStart(t *testing.T) {
	s := routedServer(t)
	gen := usGeneration(nil)
	entered := make(chan struct{})
	if _, err := s.startTrafficRun(gen, []resolvedTraffic{{}}, 30, func(ctx context.Context, _ resolvedTraffic) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	awaitTraffic(t, entered)
	if err := gen.Stop(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	if trafficGenerating(t, s) {
		t.Fatal("range stop did not finish traffic")
	}
	if _, err := s.startTrafficRun(gen, nil, 30, nil); !errors.Is(err, errTrafficRangeStopped) {
		t.Fatalf("start on stopped range = %v", err)
	}
	if trafficGenerating(t, s) {
		t.Fatal("rejected worker left generating set")
	}
}

func TestTrafficDurationCancelsInFlightExec(t *testing.T) {
	s := routedServer(t)
	gen := usGeneration(nil)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		if err := gen.Stop(3 * time.Second); err != nil {
			t.Error(err)
		}
	})
	if _, err := s.startTrafficRun(gen, []resolvedTraffic{{}}, 1, func(ctx context.Context, _ resolvedTraffic) error {
		close(entered)
		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Errorf("duration ended with %v", ctx.Err())
		}
		close(cancelled)
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	awaitTraffic(t, entered)
	awaitTraffic(t, cancelled)
	waitTrafficExit(t, s)
}
