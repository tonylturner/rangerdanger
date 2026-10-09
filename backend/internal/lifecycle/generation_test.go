package lifecycle

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

func testGeneration(id uint64) *Generation {
	return NewGeneration(id, labs.Package{ID: "pkg-a"}, &manifest.Manifest{}, nil)
}

func TestStaleCompletionDropped(t *testing.T) {
	gen := testGeneration(3)
	published := 0
	if !gen.Commit(func() { published++ }) || published != 1 {
		t.Fatalf("live generation dropped a completion")
	}

	// A worker finishes after its generation was stopped: its result
	// must not be published.
	finished := make(chan bool, 1)
	gen.Go("pcap-poll", func(ctx context.Context) {
		<-ctx.Done()
		finished <- gen.Commit(func() { published++ })
	})
	if err := gen.Stop(time.Second); err != nil {
		t.Fatal(err)
	}
	if <-finished {
		t.Error("Commit after Stop reported success")
	}
	if published != 1 {
		t.Errorf("published = %d, want the stale completion dropped", published)
	}
}

func TestStopRefusesNewEntriesAndCancels(t *testing.T) {
	gen := testGeneration(1)
	if err := gen.Stop(time.Second); err != nil {
		t.Fatal(err)
	}
	if gen.Context().Err() == nil {
		t.Error("stopped generation's context is live")
	}
	if _, ok := gen.Enter("GET /late"); ok {
		t.Error("stopped generation admitted a request")
	}
	if err := gen.Stop(time.Second); err != nil {
		t.Errorf("second Stop = %v", err)
	}
}

func TestStopWaitsForRegisteredWorkAndNamesStragglers(t *testing.T) {
	gen := testGeneration(9)
	release, _ := gen.Enter("GET /api/fast")
	stuck, _ := gen.Enter("GET /api/stuck")
	gen.Go("traffic", func(ctx context.Context) { <-ctx.Done() })
	go func() {
		<-gen.Context().Done()
		release()
		release() // releasing twice is harmless
	}()

	err := gen.Stop(50 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "generation 9") || !strings.Contains(err.Error(), "GET /api/stuck") {
		t.Fatalf("Stop = %v, want a timeout naming the straggler", err)
	}
	if strings.Contains(err.Error(), "GET /api/fast") || strings.Contains(err.Error(), "traffic") {
		t.Errorf("Stop = %v names work that drained", err)
	}
	stuck()
	if err := gen.Stop(time.Second); err != nil {
		t.Errorf("Stop after the straggler left = %v", err)
	}
}
