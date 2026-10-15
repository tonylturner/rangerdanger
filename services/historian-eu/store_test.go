package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func testClock() time.Time {
	return time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
}

func TestEpochSegmentsAndGaps(t *testing.T) {
	now := testClock()
	start := now
	s := newStore(func() time.Time { return now })
	state := fixtureState(t)
	for i, offset := range []time.Duration{0, 3 * time.Second, 6*time.Second + time.Nanosecond, 7 * time.Second} {
		now = start.Add(offset)
		state.SnapshotSeq = uint64(i + 1)
		if i == 2 {
			state.CacheEpoch = "new-epoch"
			state.SnapshotSeq = 1
		}
		if i == 3 {
			// Even an epoch name reappearing must be a new segment.
			state.CacheEpoch = "0123456789abcdef"
		}
		s.append(state)
	}
	h, found := s.history("oltc-1-tap", start, now)
	if !found || len(h.Segments) != 3 || len(h.Segments[0].Samples) != 2 || len(h.Gaps) != 1 {
		t.Fatalf("segments/gaps: %+v", h)
	}
	if h.Segments[1].CacheEpoch != "new-epoch" || h.Segments[1].Samples[0].SnapshotSeq != 1 {
		t.Fatalf("epoch did not reset sequence: %+v", h.Segments[1])
	}
	want := gap{From: start.Add(3 * time.Second), To: start.Add(6*time.Second + time.Nanosecond)}
	if h.Gaps[0] != want {
		t.Fatalf("gap: %+v want %+v", h.Gaps[0], want)
	}
	insideGap, _ := s.history("oltc-1-tap", start.Add(4*time.Second), start.Add(5*time.Second))
	if len(insideGap.Segments) != 0 || len(insideGap.Gaps) != 1 || insideGap.Gaps[0] != want {
		t.Fatalf("query inside silence must show its endpoints: %+v", insideGap)
	}
	// A point absent during an intervening epoch must still be split.
	other := fixtureState(t)
	other.CacheEpoch, other.Points = "empty-epoch", nil
	now = now.Add(time.Second)
	s.append(other)
	now = now.Add(time.Second)
	s.append(state)
	h, _ = s.history("oltc-1-tap", start, now)
	if len(h.Segments) != 4 {
		t.Fatalf("missing point merged epoch boundaries: %+v", h.Segments)
	}
}

func TestObservationFidelityAndStaleStaysStale(t *testing.T) {
	now := testClock()
	s := newStore(func() time.Time { return now })
	state := fixtureState(t)
	s.append(state)
	now = now.Add(time.Second)
	m := fixtureMap(t)
	m["snapshot_seq"] = 2
	for _, v := range m["points"].([]any) {
		p := v.(map[string]any)
		q := p["quality"].(map[string]any)
		q["iv"], q["nt"], q["sb"], q["bl"] = true, true, true, true
		p["stale_reason"] = "link_lost"
		// Keep source_time and received_at old; new ingestion is NOT new data.
	}
	pointMap(m, 2)["value"] = nil
	pointMap(m, 2)["quality"].(map[string]any)["ov"] = true
	stale, err := decodeState([]byte(encode(t, m)))
	if err != nil {
		t.Fatal(err)
	}
	s.append(stale)
	for _, p := range stale.Points {
		h, found := s.history(p.ID, now, now)
		if !found || len(h.Segments) != 1 || len(h.Segments[0].Samples) != 1 {
			t.Fatalf("inclusive ingestion-time filter (not source time): %+v", h)
		}
		got := h.Segments[0].Samples[0]
		if !reflect.DeepEqual(got.Quality, p.Quality) || !reflect.DeepEqual(got.StaleReason, p.StaleReason) ||
			!reflect.DeepEqual(got.SourceTime, p.SourceTime) || !reflect.DeepEqual(got.SourceTimeInvalid, p.SourceTimeInvalid) ||
			!reflect.DeepEqual(got.CCReceivedAt, p.ReceivedAt) || got.SnapshotSeq != 2 || got.IngestedAt != now {
			t.Fatalf("observation was reinterpreted: %+v", got)
		}
		if string(got.Value) != string(p.Value) {
			t.Fatalf("value changed: %s want %s", got.Value, p.Value)
		}
	}
	h, _ := s.history("oltc-1-tap", now, now)
	var step struct {
		Value     int  `json:"value"`
		Transient bool `json:"transient"`
	}
	if err := json.Unmarshal(h.Segments[0].Samples[0].Value, &step); err != nil || step.Value != -3 || !step.Transient {
		t.Fatalf("step value lost: %+v, %v", step, err)
	}
	now = now.Add(time.Hour)
	if h := s.health(); h.Status != "degraded" || h.LastIngestAt == nil || h.LastIngestAt.Equal(now) {
		t.Fatalf("silence masquerades as health: %+v", h)
	}
	for _, p := range s.points() {
		if !p.Last.Quality.IV || !p.Last.Quality.NT || *p.Last.StaleReason != "link_lost" {
			t.Fatalf("last stale observation healed: %+v", p)
		}
	}
}

func TestRetentionBound(t *testing.T) {
	now := testClock()
	start := now
	s := newStore(func() time.Time { return now })
	state := fixtureState(t)
	// Exercise the frozen maximum point count, not a one-point shortcut.
	prototype := state.Points[0]
	state.Points = make([]point, maxPoints)
	for i := range state.Points {
		state.Points[i] = prototype
		state.Points[i].ID = fmt.Sprintf("point-%d", i)
		state.Points[i].IOA = uint32(i + 1)
	}
	const extra = 7
	for i := 0; i < retentionSnapshots+extra; i++ {
		now = start.Add(time.Duration(i) * time.Second)
		state.SnapshotSeq = uint64(i + 1)
		s.append(state)
	}
	if s.size != retentionSnapshots || s.accepted != retentionSnapshots+extra {
		t.Fatalf("ring unbounded or counter reset: size=%d count=%d", s.size, s.accepted)
	}
	h, _ := s.history("point-0", start, now)
	samples := h.Segments[0].Samples
	if len(samples) != retentionSnapshots || samples[0].SnapshotSeq != extra+1 {
		t.Fatalf("ring order/eviction: %d, first=%d", len(samples), samples[0].SnapshotSeq)
	}
	if span := samples[len(samples)-1].IngestedAt.Sub(samples[0].IngestedAt); span != 2*time.Hour {
		t.Fatalf("less than two hours at 1 Hz: %v", span)
	}
	if len(s.points()) != maxPoints {
		t.Fatal("lost point catalog")
	}
}

func TestRetentionEvictsCatalogSegmentsAndGaps(t *testing.T) {
	now := testClock()
	start := now
	s := newStore(func() time.Time { return now })
	for i := 0; i < retentionSnapshots+1; i++ {
		now = start.Add(time.Duration(i) * 4 * time.Second)
		state := ccState{
			CacheEpoch: fmt.Sprintf("epoch-%d", i), SnapshotSeq: 1,
			Points: []point{{ID: fmt.Sprintf("point-%d", i), Value: json.RawMessage("null")}},
		}
		s.append(state)
	}
	points := s.points()
	if len(points) != retentionSnapshots {
		t.Fatalf("catalog not bounded: %d", len(points))
	}
	for _, p := range points {
		if p.ID == "point-0" {
			t.Fatal("evicted point remains in catalog")
		}
	}
	if _, found := s.history("point-0", start, now); found {
		t.Fatal("evicted sample remains in history")
	}
	h, _ := s.history("point-1", start, now)
	if len(h.Segments) != 1 || h.Segments[0].CacheEpoch != "epoch-1" || len(h.Gaps) != retentionSnapshots {
		t.Fatalf("retained segments/gaps incorrect: segments=%d gaps=%d", len(h.Segments), len(h.Gaps))
	}
}

func TestConcurrentIngestAndRead(t *testing.T) {
	s := newStore(time.Now)
	state := fixtureState(t)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < 20; j++ {
				s.append(state)
				s.reject()
				s.health()
				s.points()
				s.history("oltc-1-tap", time.Time{}, time.Now())
			}
		}()
	}
	group.Wait()
	h := s.health()
	if h.IngestCount != 160 || h.RejectedCount != 160 {
		t.Fatalf("lost concurrent updates: %+v", h)
	}
}

func TestRejectedIngestDoesNotEndSilence(t *testing.T) {
	now := testClock()
	start := now
	s := newStore(func() time.Time { return now })
	post := func(body string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		s.ingestHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("ingest: %d want %d", w.Code, want)
		}
	}
	post(fixture, 200)
	now = start.Add(3 * time.Second)
	if s.health().Status != "ok" {
		t.Fatal("exactly three seconds is not a gap")
	}
	now = now.Add(time.Nanosecond)
	post(`{"schema":1}`, 400)
	if h := s.health(); h.Status != "degraded" || !h.LastIngestAt.Equal(start) {
		t.Fatalf("rejection refreshed health: %+v", h)
	}
	now = start.Add(5 * time.Second)
	post(fixture, 200)
	h, _ := s.history("oltc-1-tap", start, now)
	if len(h.Gaps) != 1 || h.Gaps[0].From != start || h.Gaps[0].To != now {
		t.Fatalf("rejection ended silence: %+v", h.Gaps)
	}
}
