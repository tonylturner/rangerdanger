package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

type statusEvent struct {
	Point   string          `json:"point"`
	At      time.Time       `json:"at"`
	From    json.RawMessage `json:"from"`
	To      json.RawMessage `json:"to"`
	Quality quality         `json:"quality"`
}

type events struct {
	Events []statusEvent `json:"events"`
	Gaps   []gap         `json:"gaps"`
}

// A baseline must be an established observation, not an unknown or retained
// stale value. Quality recovery alone is not a status change.
func eventGood(p point) bool {
	q := p.Quality
	return p.StaleReason == nil && !q.IV && !q.NT && !q.SB && !q.BL && (q.OV == nil || !*q.OV) && !bytes.Equal(p.Value, []byte("null"))
}

func eventValue(p point) (json.RawMessage, bool) {
	if !eventGood(p) {
		return nil, false
	}
	switch p.Kind {
	case "single-point", "double-point":
		return p.Value, true
	case "step-position":
		var step struct {
			Value     int  `json:"value"`
			Transient bool `json:"transient"`
		}
		if json.Unmarshal(p.Value, &step) == nil && !step.Transient {
			value, _ := json.Marshal(step.Value)
			return value, true
		}
	}
	return nil, false
}

func (s *store) events(since, until time.Time) events {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := events{Events: []statusEvent{}, Gaps: []gap{}}
	previous := make(map[string]json.RawMessage)
	var lastSegment uint64
	for i := 0; i < s.size; i++ {
		frame := s.ring[(s.start+i)%len(s.ring)]
		if !frame.gapFrom.IsZero() && frame.at.After(since) && frame.gapFrom.Before(until) {
			result.Gaps = append(result.Gaps, gap{From: frame.gapFrom, To: frame.at})
		}
		if frame.at.After(until) {
			continue
		}
		if frame.segment != lastSegment || !frame.gapFrom.IsZero() {
			clear(previous)
		}
		lastSegment = frame.segment
		present := make(map[string]bool)
		for _, p := range frame.points {
			present[p.ID] = true
			value, ok := eventValue(p)
			if !ok {
				// Keep the last settled tap during its movement, but never
				// bridge a quality outage or missing point.
				if p.Kind != "step-position" || !eventGood(p) {
					delete(previous, p.ID)
				}
				continue
			}
			if from, exists := previous[p.ID]; exists && !bytes.Equal(from, value) && !frame.at.Before(since) {
				result.Events = append(result.Events, statusEvent{
					Point: p.ID, At: frame.at, From: from, To: value, Quality: p.Quality,
				})
			}
			previous[p.ID] = value
		}
		for id := range previous {
			if !present[id] {
				delete(previous, id)
			}
		}
	}
	return result
}

func (s *store) serveEvents(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q) != 2 || len(q["since"]) != 1 || len(q["until"]) != 1 || !stamp(q.Get("since")) || !stamp(q.Get("until")) {
		writeError(w, http.StatusBadRequest, "since and until are required exactly once (UTC RFC 3339)")
		return
	}
	since, _ := time.Parse(time.RFC3339Nano, q.Get("since"))
	until, _ := time.Parse(time.RFC3339Nano, q.Get("until"))
	if until.Before(since) {
		writeError(w, http.StatusBadRequest, "until precedes since")
		return
	}
	writeJSON(w, http.StatusOK, s.events(since, until))
}
