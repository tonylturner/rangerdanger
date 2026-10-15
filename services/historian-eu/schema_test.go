package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const fixture = `{
  "schema":2,"cache_epoch":"0123456789abcdef","snapshot_seq":1,"rendered_at":"2026-10-15T10:00:00Z",
  "link":{"state":"started","peer":"10.30.30.20:2404","since":"2026-10-15T09:59:00Z",
    "last_rx_at":"2026-10-15T10:00:00Z","connection_seq":1,
    "gi":{"state":"complete","started_at":"2026-10-15T09:59:01Z","completed_at":"2026-10-15T09:59:02Z"}},
  "points":[
    {"id":"rmu-1-position","ioa":1101,"asdu":"M_DP_TB_1","value":"closed",
     "quality":{"iv":false,"nt":false,"sb":false,"bl":false,"ov":null},
     "stale_reason":null,"source_time":"2026-10-15T10:00:00Z","source_time_invalid":false,
     "received_at":"2026-10-15T10:00:00.123Z","cot":3},
    {"id":"oltc-1-auto","ioa":1303,"asdu":"M_SP_TB_1","value":true,
     "quality":{"iv":false,"nt":false,"sb":false,"bl":false,"ov":null},
     "stale_reason":null,"source_time":null,"source_time_invalid":null,
     "received_at":"2026-10-15T10:00:00Z","cot":20},
    {"id":"oltc-1-bus-kv","ioa":2301,"asdu":"M_ME_NC_1","value":19.8,
     "quality":{"iv":false,"nt":false,"sb":false,"bl":false,"ov":false},
     "stale_reason":null,"source_time":null,"source_time_invalid":null,
     "received_at":"2026-10-15T10:00:00Z","cot":3},
    {"id":"oltc-1-tap","ioa":1301,"asdu":"M_ST_TB_1","value":{"value":-3,"transient":true},
     "quality":{"iv":false,"nt":false,"sb":false,"bl":false,"ov":false},
     "stale_reason":null,"source_time":"2026-10-15T10:00:00Z","source_time_invalid":true,
     "received_at":"2026-10-15T10:00:00Z","cot":3}
  ]
}`

func fixtureMap(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(fixture), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fixtureState(t *testing.T) ccState {
	t.Helper()
	state, err := decodeState([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func pointMap(m map[string]any, n int) map[string]any {
	return m["points"].([]any)[n].(map[string]any)
}

func TestSchemaValid(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"all-kinds", func(map[string]any) {}},
		{"never-received", func(m map[string]any) {
			for _, v := range m["points"].([]any) {
				p := v.(map[string]any)
				p["value"], p["received_at"], p["cot"] = nil, nil, nil
				p["source_time"], p["source_time_invalid"] = nil, nil
				p["stale_reason"] = "never_received"
				p["quality"].(map[string]any)["iv"] = true
				p["quality"].(map[string]any)["nt"] = true
			}
		}},
		{"step-lower-limit", func(m map[string]any) { pointMap(m, 3)["value"].(map[string]any)["value"] = -64 }},
		{"step-upper-limit", func(m map[string]any) { pointMap(m, 3)["value"].(map[string]any)["value"] = 63 }},
		{"step-overflow", func(m map[string]any) { pointMap(m, 3)["quality"].(map[string]any)["ov"] = true }},
		{"heartbeat", func(m map[string]any) { pointMap(m, 1)["stale_reason"] = "heartbeat" }},
		{"no-points", func(m map[string]any) { m["points"] = []any{} }},
		{"null-link-times", func(m map[string]any) {
			l := m["link"].(map[string]any)
			l["state"], l["since"], l["last_rx_at"] = "disconnected", nil, nil
			gi := l["gi"].(map[string]any)
			gi["state"], gi["started_at"], gi["completed_at"] = "none", nil, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fixtureMap(t)
			tc.edit(m)
			if _, err := decodeState([]byte(encode(t, m))); err != nil {
				t.Fatal(err)
			}
		})
	}
	// u64 must not pass through float64 and lose snapshot identity.
	body := strings.Replace(fixture, `"snapshot_seq":1`, `"snapshot_seq":18446744073709551615`, 1)
	state, err := decodeState([]byte(body))
	if err != nil || state.SnapshotSeq != ^uint64(0) {
		t.Fatalf("u64 sequence: %d, %v", state.SnapshotSeq, err)
	}
}

func TestStrictValidationAndRejectionCounting(t *testing.T) {
	cases := []struct {
		name string
		edit func(map[string]any)
	}{
		{"v1", func(m map[string]any) { m["schema"] = 1 }},
		{"null-schema", func(m map[string]any) { m["schema"] = nil }},
		{"fractional-schema", func(m map[string]any) { m["schema"] = 2.1 }},
		{"unknown-top", func(m map[string]any) { m["truth"] = true }},
		{"unknown-link", func(m map[string]any) { m["link"].(map[string]any)["extra"] = true }},
		{"unknown-gi", func(m map[string]any) { m["link"].(map[string]any)["gi"].(map[string]any)["extra"] = true }},
		{"unknown-point", func(m map[string]any) { pointMap(m, 0)["extra"] = true }},
		{"unknown-quality", func(m map[string]any) { pointMap(m, 0)["quality"].(map[string]any)["extra"] = true }},
		{"unknown-step", func(m map[string]any) { pointMap(m, 3)["value"].(map[string]any)["extra"] = true }},
		{"empty-epoch", func(m map[string]any) { m["cache_epoch"] = "" }},
		{"oversize-epoch", func(m map[string]any) { m["cache_epoch"] = strings.Repeat("a", 64) }},
		{"negative-seq", func(m map[string]any) { m["snapshot_seq"] = -1 }},
		{"fractional-seq", func(m map[string]any) { m["snapshot_seq"] = 1.1 }},
		{"null-seq", func(m map[string]any) { m["snapshot_seq"] = nil }},
		{"bad-time", func(m map[string]any) { m["rendered_at"] = "not-a-time" }},
		{"non-utc", func(m map[string]any) { m["rendered_at"] = "2026-10-15T10:00:00+00:00" }},
		{"comma-time", func(m map[string]any) { m["rendered_at"] = "2026-10-15T10:00:00,123Z" }},
		{"short-hour", func(m map[string]any) { m["rendered_at"] = "2026-10-15T1:00:00Z" }},
		{"invalid-date", func(m map[string]any) { m["rendered_at"] = "2026-02-30T10:00:00Z" }},
		{"null-render-time", func(m map[string]any) { m["rendered_at"] = nil }},
		{"link-enum", func(m map[string]any) { m["link"].(map[string]any)["state"] = "up" }},
		{"gi-enum", func(m map[string]any) { m["link"].(map[string]any)["gi"].(map[string]any)["state"] = "up" }},
		{"null-points", func(m map[string]any) { m["points"] = nil }},
		{"points-object", func(m map[string]any) { m["points"] = map[string]any{} }},
		{"null-point", func(m map[string]any) { m["points"].([]any)[0] = nil }},
		{"duplicate-id", func(m map[string]any) { pointMap(m, 1)["id"] = pointMap(m, 0)["id"] }},
		{"duplicate-ioa", func(m map[string]any) { pointMap(m, 1)["ioa"] = pointMap(m, 0)["ioa"] }},
		{"zero-ioa", func(m map[string]any) { pointMap(m, 0)["ioa"] = 0 }},
		{"oversize-ioa", func(m map[string]any) { pointMap(m, 0)["ioa"] = 16777216 }},
		{"fractional-ioa", func(m map[string]any) { pointMap(m, 0)["ioa"] = 1.1 }},
		{"asdu-enum", func(m map[string]any) { pointMap(m, 0)["asdu"] = "C_DC_NA_1" }},
		{"wrong-dp-type", func(m map[string]any) { pointMap(m, 0)["value"] = 2 }},
		{"wrong-dp-enum", func(m map[string]any) { pointMap(m, 0)["value"] = "on" }},
		{"wrong-sp-type", func(m map[string]any) { pointMap(m, 1)["value"] = 1 }},
		{"wrong-float-type", func(m map[string]any) { pointMap(m, 2)["value"] = "19.8" }},
		{"step-scalar", func(m map[string]any) { pointMap(m, 3)["value"] = -3 }},
		{"step-fractional", func(m map[string]any) { pointMap(m, 3)["value"].(map[string]any)["value"] = -1.5 }},
		{"step-low", func(m map[string]any) { pointMap(m, 3)["value"].(map[string]any)["value"] = -65 }},
		{"step-high", func(m map[string]any) { pointMap(m, 3)["value"].(map[string]any)["value"] = 64 }},
		{"step-transient-type", func(m map[string]any) { pointMap(m, 3)["value"].(map[string]any)["transient"] = 1 }},
		{"step-missing-transient", func(m map[string]any) { delete(pointMap(m, 3)["value"].(map[string]any), "transient") }},
		{"null-iv", func(m map[string]any) { pointMap(m, 0)["quality"].(map[string]any)["iv"] = nil }},
		{"numeric-nt", func(m map[string]any) { pointMap(m, 0)["quality"].(map[string]any)["nt"] = 1 }},
		{"float-ov-null", func(m map[string]any) { pointMap(m, 2)["quality"].(map[string]any)["ov"] = nil }},
		{"step-ov-null", func(m map[string]any) { pointMap(m, 3)["quality"].(map[string]any)["ov"] = nil }},
		{"dp-ov-bool", func(m map[string]any) { pointMap(m, 0)["quality"].(map[string]any)["ov"] = false }},
		{"sp-ov-bool", func(m map[string]any) { pointMap(m, 1)["quality"].(map[string]any)["ov"] = false }},
		{"unknown-stale", func(m map[string]any) { pointMap(m, 0)["stale_reason"] = "bad" }},
		{"bad-source", func(m map[string]any) { pointMap(m, 0)["source_time"] = "bad" }},
		{"missing-source-invalid", func(m map[string]any) { pointMap(m, 0)["source_time_invalid"] = nil }},
		{"invalid-without-source", func(m map[string]any) { pointMap(m, 1)["source_time_invalid"] = false }},
		{"bad-received", func(m map[string]any) { pointMap(m, 0)["received_at"] = "bad" }},
		{"cot-range", func(m map[string]any) { pointMap(m, 0)["cot"] = 64 }},
		{"too-many-points", func(m map[string]any) {
			points := make([]any, maxPoints+1)
			for i := range points {
				p := pointMap(fixtureMap(t), 0)
				p["id"], p["ioa"] = fmt.Sprintf("point-%d", i), i+1
				points[i] = p
			}
			m["points"] = points
		}},
	}
	s := newStore(time.Now)
	handler := s.ingestHandler()
	post := func(body string) int {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(body)))
		return w.Code
	}
	if code := post(fixture); code != http.StatusOK {
		t.Fatalf("valid ingest: %d", code)
	}
	rejected := uint64(0)
	reject := func(t *testing.T, body string) {
		t.Helper()
		if code := post(body); code != http.StatusBadRequest {
			t.Fatalf("invalid ingest: %d", code)
		}
		rejected++
		h := s.health()
		if h.RejectedCount != rejected || h.IngestCount != 1 || s.size != 1 {
			t.Fatalf("invalid body changed storage or wrong counts: %+v", h)
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := fixtureMap(t)
			tc.edit(m)
			reject(t, encode(t, m))
		})
	}
	// Every field is required even when its value may be null.
	objects := map[string]func(map[string]any) map[string]any{
		"top":     func(m map[string]any) map[string]any { return m },
		"link":    func(m map[string]any) map[string]any { return m["link"].(map[string]any) },
		"gi":      func(m map[string]any) map[string]any { return m["link"].(map[string]any)["gi"].(map[string]any) },
		"point":   func(m map[string]any) map[string]any { return pointMap(m, 0) },
		"quality": func(m map[string]any) map[string]any { return pointMap(m, 0)["quality"].(map[string]any) },
	}
	for name, get := range objects {
		for key := range get(fixtureMap(t)) {
			t.Run("missing-"+name+"-"+key, func(t *testing.T) {
				m := fixtureMap(t)
				delete(get(m), key)
				reject(t, encode(t, m))
			})
		}
	}
	for name, body := range map[string]string{
		"empty": "", "array": "[]", "null": "null", "malformed": "{",
		"truncated":        fixture[:len(fixture)-3],
		"mismatched-close": `{"schema":2]`, "unexpected-close": `]`,
		"trailing": fixture + `{}`, "oversize": strings.Repeat(" ", maxBodyBytes+1),
		"invalid-utf8":      strings.Replace(fixture, "closed", string([]byte{0xff}), 1),
		"duplicate":         strings.Replace(fixture, `"schema":2`, `"schema":2,"schema":2`, 1),
		"escaped-duplicate": strings.Replace(fixture, `"iv":false`, `"iv":false,"\u0069v":true`, 1),
		"overflow-float":    strings.Replace(fixture, `19.8`, `1e999`, 1),
		"nan":               strings.Replace(fixture, `19.8`, `NaN`, 1),
		"overflow-u64":      strings.Replace(fixture, `"snapshot_seq":1`, `"snapshot_seq":18446744073709551616`, 1),
		"deep":              `{"unknown":` + strings.Repeat("[", 14) + `0` + strings.Repeat("]", 14) + `}`,
	} {
		t.Run(name, func(t *testing.T) { reject(t, body) })
	}
}

func TestCapturedCCStateIngest(t *testing.T) {
	// Preserve the live CC body byte-for-byte, including step QDS overflow.
	body, err := os.ReadFile("testdata/cc-state.json")
	if err != nil {
		t.Fatal(err)
	}
	s := newStore(testClock)
	w := httptest.NewRecorder()
	s.ingestHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("captured CC state rejected: %d %s", w.Code, w.Body)
	}
	if h := s.health(); h.IngestCount != 1 || h.RejectedCount != 0 {
		t.Fatalf("captured ingest counters: %+v", h)
	}
	points := s.points()
	if len(points) != 31 {
		t.Fatalf("stored %d points, want all 31 captured points", len(points))
	}
	for _, p := range points {
		if p.ID == "oltc-1-tap" {
			if p.Last.Quality.OV == nil || *p.Last.Quality.OV {
				t.Fatalf("step OV=false not preserved: %+v", p.Last.Quality)
			}
			return
		}
	}
	t.Fatal("captured step point missing")
}
