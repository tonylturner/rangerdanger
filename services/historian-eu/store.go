package main

import (
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// Including both endpoints preserves a full two-hour interval at 1 Hz.
const retentionSnapshots = 2*60*60 + 1

type sample struct {
	SnapshotSeq       uint64          `json:"snapshot_seq"`
	Value             json.RawMessage `json:"value"`
	Quality           quality         `json:"quality"`
	StaleReason       *string         `json:"stale_reason"`
	SourceTime        *string         `json:"source_time"`
	SourceTimeInvalid *bool           `json:"source_time_invalid"`
	CCReceivedAt      *string         `json:"cc_received_at"`
	IngestedAt        time.Time       `json:"ingested_at"`
}

type snapshot struct {
	epoch   string
	seq     uint64
	segment uint64
	at      time.Time
	points  []point
	gapFrom time.Time
}

func (s snapshot) sample(p point) sample {
	return sample{
		SnapshotSeq: s.seq, Value: p.Value, Quality: p.Quality,
		StaleReason: p.StaleReason, SourceTime: p.SourceTime,
		SourceTimeInvalid: p.SourceTimeInvalid, CCReceivedAt: p.ReceivedAt,
		IngestedAt: s.at,
	}
}

type pointLast struct {
	ID   string `json:"id"`
	IOA  uint32 `json:"ioa"`
	ASDU string `json:"asdu"`
	Last sample `json:"last"`
}

type segment struct {
	CacheEpoch string   `json:"cache_epoch"`
	Samples    []sample `json:"samples"`
}

type gap struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type history struct {
	Point    string    `json:"point"`
	Segments []segment `json:"segments"`
	Gaps     []gap     `json:"gaps"`
}

type health struct {
	Status        string     `json:"status"`
	LastIngestAt  *time.Time `json:"last_ingest_at"`
	IngestCount   uint64     `json:"ingest_count"`
	RejectedCount uint64     `json:"rejected_count"`
}

type store struct {
	mu       sync.RWMutex
	now      func() time.Time
	ring     [retentionSnapshots]snapshot
	start    int
	size     int
	accepted uint64
	rejected uint64
}

func newStore(now func() time.Time) *store {
	return &store{now: now}
}

func (s *store) reject() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejected++
}

func (s *store) append(state ccState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	frame := snapshot{
		epoch: state.CacheEpoch, seq: state.SnapshotSeq,
		at: s.now().UTC(), points: state.Points, segment: 1,
	}
	if s.size > 0 {
		previous := s.ring[(s.start+s.size-1)%len(s.ring)]
		frame.segment = previous.segment
		if previous.epoch != frame.epoch {
			frame.segment++
		}
		if frame.at.Sub(previous.at) > 3*time.Second {
			frame.gapFrom = previous.at
		}
	}
	index := (s.start + s.size) % len(s.ring)
	s.ring[index] = frame
	if s.size < len(s.ring) {
		s.size++
	} else {
		s.start = (s.start + 1) % len(s.ring)
	}
	s.accepted++
}

func (s *store) points() []pointLast {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Rebuild from the bounded ring, not an ever-growing id/epoch catalog.
	latest := make(map[string]pointLast)
	for i := 0; i < s.size; i++ {
		frame := s.ring[(s.start+i)%len(s.ring)]
		for _, p := range frame.points {
			latest[p.ID] = pointLast{ID: p.ID, IOA: p.IOA, ASDU: p.ASDU, Last: frame.sample(p)}
		}
	}
	result := make([]pointLast, 0, len(latest))
	for _, p := range latest {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *store) history(id string, since, until time.Time) (history, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := history{Point: id, Segments: []segment{}, Gaps: []gap{}}
	found := false
	var lastSegment uint64
	for i := 0; i < s.size; i++ {
		frame := s.ring[(s.start+i)%len(s.ring)]
		// Gaps describe feed silence, independent of the selected point. Keep
		// original endpoints when a gap intersects the requested interval.
		if !frame.gapFrom.IsZero() && frame.at.After(since) && frame.gapFrom.Before(until) {
			result.Gaps = append(result.Gaps, gap{From: frame.gapFrom, To: frame.at})
		}
		for _, p := range frame.points {
			if p.ID != id {
				continue
			}
			found = true
			if frame.at.Before(since) || frame.at.After(until) {
				continue
			}
			if len(result.Segments) == 0 || lastSegment != frame.segment {
				result.Segments = append(result.Segments, segment{CacheEpoch: frame.epoch, Samples: []sample{}})
				lastSegment = frame.segment
			}
			n := len(result.Segments) - 1
			result.Segments[n].Samples = append(result.Segments[n].Samples, frame.sample(p))
		}
	}
	return result, found
}

func (s *store) health() health {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := health{Status: "degraded", IngestCount: s.accepted, RejectedCount: s.rejected}
	if s.size > 0 {
		at := s.ring[(s.start+s.size-1)%len(s.ring)].at
		result.LastIngestAt = &at
		if s.now().Sub(at) <= 3*time.Second {
			result.Status = "ok"
		}
	}
	return result
}
