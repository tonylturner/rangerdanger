package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEventsSettledChangesAndWindow(t *testing.T) {
	now := testClock()
	start := now
	s := newStore(func() time.Time { return now })
	appendPoints := func(second int, tap string, closed bool) {
		now = start.Add(time.Duration(second) * time.Second)
		position, _ := json.Marshal(closed)
		s.append(ccState{CacheEpoch: "one", Points: []point{
			{ID: "oltc-1-tap", Kind: "step-position", Value: json.RawMessage(tap)},
			{ID: "plc-1-running", Kind: "single-point", Value: position},
			{ID: "voltage", Kind: "float", Value: json.RawMessage("20")},
		}})
	}
	appendPoints(0, `{"value":1,"transient":false}`, true)
	appendPoints(1, `{"value":1,"transient":true}`, true)
	appendPoints(2, `{"value":2,"transient":true}`, true)
	appendPoints(3, `{"value":2,"transient":false}`, false)
	appendPoints(4, `{"value":2,"transient":false}`, false)
	result := s.events(start.Add(3*time.Second), start.Add(3*time.Second))
	if len(result.Events) != 2 || len(result.Gaps) != 0 {
		t.Fatalf("settled events: %+v", result)
	}
	tap := result.Events[0]
	if tap.Point != "oltc-1-tap" || string(tap.From) != "1" || string(tap.To) != "2" || !tap.At.Equal(start.Add(3*time.Second)) {
		t.Fatalf("tap move not collapsed at first settled ingest: %+v", tap)
	}
	if len(s.events(start.Add(4*time.Second), now).Events) != 0 {
		t.Fatal("repeated snapshot emitted event")
	}
}

func TestEventsDoNotBridgeUnknowns(t *testing.T) {
	for _, boundary := range []string{"epoch", "gap", "null", "iv", "nt", "sb", "bl", "ov", "stale", "missing", "retention"} {
		t.Run(boundary, func(t *testing.T) {
			now := testClock()
			start := now
			s := newStore(func() time.Time { return now })
			state := ccState{CacheEpoch: "one", Points: []point{{ID: "switch", Kind: "double-point", Value: json.RawMessage(`"closed"`)}}}
			s.append(state)
			state.Points = append([]point(nil), state.Points...)
			now = now.Add(time.Second)
			switch boundary {
			case "epoch":
				state.CacheEpoch = "two"
				state.Points[0].Value = json.RawMessage(`"open"`)
			case "gap":
				now = now.Add(4 * time.Second)
				state.Points[0].Value = json.RawMessage(`"open"`)
			case "null":
				state.Points[0].Value = json.RawMessage("null")
			case "iv":
				state.Points[0].Quality.IV = true
			case "nt":
				state.Points[0].Quality.NT = true
			case "sb":
				state.Points[0].Quality.SB = true
			case "bl":
				state.Points[0].Quality.BL = true
			case "ov":
				overflow := true
				state.Points[0].Quality.OV = &overflow
			case "stale":
				reason := "link_lost"
				state.Points[0].StaleReason = &reason
			case "missing":
				state.Points = nil
			case "retention":
				for i := 0; i < retentionSnapshots; i++ {
					s.append(ccState{CacheEpoch: "one"})
				}
				state.Points = nil
			}
			s.append(state)
			now = now.Add(time.Second)
			s.append(ccState{CacheEpoch: state.CacheEpoch, Points: []point{{ID: "switch", Kind: "double-point", Value: json.RawMessage(`"open"`)}}})
			result := s.events(start, now)
			if len(result.Events) != 0 {
				t.Fatalf("boundary fabricated a change: %+v", result)
			}
			if boundary == "gap" && len(result.Gaps) != 1 {
				t.Fatalf("lost ingest gap: %+v", result)
			}
		})
	}
}

func TestEventsQuery(t *testing.T) {
	s := newStore(testClock)
	valid := "since=2026-10-15T10:00:00Z&until=2026-10-15T10:00:01Z"
	for _, query := range []string{
		valid, "", "since=bad&until=bad", valid + "&since=2026-10-15T10:00:00Z",
		valid + "&point=x", "since=2026-10-15T10:00:02Z&until=2026-10-15T10:00:01Z",
		"since=2026-10-15T10:00:00%2B01:00&until=2026-10-15T10:00:01Z",
		valid + "&bad=%zz",
	} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.readHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/events?"+query, nil))
			want := 400
			if query == valid {
				want = 200
				if w.Body.String() != "{\"events\":[],\"gaps\":[]}\n" {
					t.Fatalf("empty arrays: %s", w.Body)
				}
			}
			if w.Code != want {
				t.Fatalf("status %d want %d: %s", w.Code, want, w.Body)
			}
		})
	}
}

func TestEventsReadOverHTTP(t *testing.T) {
	now := testClock()
	start := now
	s := newStore(func() time.Time { return now })
	for _, value := range []string{`"closed"`, `"open"`} {
		s.append(ccState{CacheEpoch: "one", Points: []point{{ID: "switch", Kind: "double-point", Value: json.RawMessage(value)}}})
		now = now.Add(time.Second)
	}
	server := httptest.NewServer(s.readHandler())
	defer server.Close()
	response, err := listenerClient.Get(server.URL + "/api/events?since=" + start.Format(time.RFC3339Nano) + "&until=" + now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result events
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" || len(result.Events) != 1 {
		t.Fatalf("read response: %d %+v", response.StatusCode, result)
	}
	event := result.Events[0]
	if string(event.From) != `"closed"` || string(event.To) != `"open"` || !event.At.Equal(start.Add(time.Second)) || event.Quality.IV {
		t.Fatalf("wire event lost provenance: %+v", event)
	}
}
