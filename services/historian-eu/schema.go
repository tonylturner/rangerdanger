package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxPoints    = 31
	maxBodyBytes = 64 * 1024
)

type quality struct {
	IV bool  `json:"iv"`
	NT bool  `json:"nt"`
	SB bool  `json:"sb"`
	BL bool  `json:"bl"`
	OV *bool `json:"ov"`
}

type point struct {
	ID                string          `json:"id"`
	IOA               uint32          `json:"ioa"`
	ASDU              string          `json:"asdu"`
	Value             json.RawMessage `json:"value"`
	Quality           quality         `json:"quality"`
	StaleReason       *string         `json:"stale_reason"`
	SourceTime        *string         `json:"source_time"`
	SourceTimeInvalid *bool           `json:"source_time_invalid"`
	ReceivedAt        *string         `json:"received_at"`
}

type ccState struct {
	Schema      int     `json:"schema"`
	CacheEpoch  string  `json:"cache_epoch"`
	SnapshotSeq uint64  `json:"snapshot_seq"`
	RenderedAt  string  `json:"rendered_at"`
	Points      []point `json:"points"`
}

// Decode tokens ourselves because encoding/json otherwise accepts duplicate
// members (including escaped duplicates). Evidence must not be ambiguous.
func readJSON(d *json.Decoder, depth int) (any, error) {
	if depth > 12 {
		return nil, errors.New("JSON nesting too deep")
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		m := make(map[string]any)
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("invalid object key")
			}
			if _, exists := m[name]; exists {
				return nil, fmt.Errorf("duplicate member %q", name)
			}
			v, err := readJSON(d, depth+1)
			if err != nil {
				return nil, err
			}
			m[name] = v
		}
		_, err = d.Token()
		return m, err
	case json.Delim('['):
		a := make([]any, 0)
		for d.More() {
			v, err := readJSON(d, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		_, err = d.Token()
		return a, err
	default:
		return token, nil
	}
}

type rule func(any) bool

func object(fields map[string]rule) rule {
	return func(v any) bool {
		m, ok := v.(map[string]any)
		if !ok || len(m) != len(fields) {
			return false
		}
		for name, check := range fields {
			value, exists := m[name]
			if !exists || !check(value) {
				return false
			}
		}
		return true
	}
}

func nullable(check rule) rule {
	return func(v any) bool { return v == nil || check(v) }
}

func boolean(v any) bool {
	_, ok := v.(bool)
	return ok
}

func text(limit int) rule {
	return func(v any) bool {
		s, ok := v.(string)
		return ok && len(s) > 0 && len(s) <= limit && !strings.ContainsAny(s, "\x00\r\n")
	}
}

func enum(values ...string) rule {
	return func(v any) bool {
		for _, allowed := range values {
			if v == allowed {
				return true
			}
		}
		return false
	}
}

func integer(min, max int64) rule {
	return func(v any) bool {
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		i, err := n.Int64()
		return err == nil && i >= min && i <= max
	}
}

func unsigned(v any) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	var i uint64
	return json.Unmarshal([]byte(n), &i) == nil
}

func stamp(v any) bool {
	s, ok := v.(string)
	if !ok || len(s) > 40 || !strings.HasSuffix(s, "Z") {
		return false
	}
	// time.Parse permits a comma decimal separator and single-digit hours;
	// neither is an RFC 3339 timestamp.
	if len(s) < 20 || s[10] != 'T' || s[13] != ':' || s[16] != ':' || strings.Contains(s, ",") {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil
}

func finite(v any) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	f, err := n.Float64()
	return err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}

var qualityRule = object(map[string]rule{
	"iv": boolean, "nt": boolean, "sb": boolean, "bl": boolean, "ov": nullable(boolean),
})

var stepRule = object(map[string]rule{
	"value": integer(-64, 63), "transient": boolean,
})

var pointRule = object(map[string]rule{
	"id": text(63), "ioa": integer(1, 16777215),
	"asdu":  enum("M_DP_TB_1", "M_SP_TB_1", "M_ME_NC_1", "M_ST_TB_1"),
	"value": func(any) bool { return true }, "quality": qualityRule,
	"stale_reason": nullable(enum("never_received", "link_lost", "device_unreachable", "measurement_invalid", "heartbeat")),
	"source_time":  nullable(stamp), "source_time_invalid": nullable(boolean),
	"received_at": nullable(stamp), "cot": nullable(integer(0, 63)),
})

func validPoint(v any) bool {
	if !pointRule(v) {
		return false
	}
	m := v.(map[string]any)
	q := m["quality"].(map[string]any)
	if (m["source_time"] == nil) != (m["source_time_invalid"] == nil) {
		return false
	}
	if m["asdu"] == "M_ME_NC_1" {
		if !boolean(q["ov"]) {
			return false
		}
		return nullable(finite)(m["value"])
	}
	if q["ov"] != nil {
		return false
	}
	switch m["asdu"] {
	case "M_DP_TB_1":
		return nullable(enum("intermediate", "open", "closed", "indeterminate"))(m["value"])
	case "M_SP_TB_1":
		return nullable(boolean)(m["value"])
	case "M_ST_TB_1":
		return nullable(stepRule)(m["value"])
	}
	return false
}

func validPoints(v any) bool {
	points, ok := v.([]any)
	if !ok || len(points) > maxPoints {
		return false
	}
	ids := make(map[string]bool)
	ioas := make(map[int64]bool)
	for _, p := range points {
		if !validPoint(p) {
			return false
		}
		m := p.(map[string]any)
		ioa, _ := m["ioa"].(json.Number).Int64()
		id := m["id"].(string)
		if ids[id] || ioas[ioa] {
			return false
		}
		ids[id] = true
		ioas[ioa] = true
	}
	return true
}

var stateRule = object(map[string]rule{
	"schema": integer(2, 2), "cache_epoch": text(63),
	"snapshot_seq": unsigned, "rendered_at": stamp,
	"link": object(map[string]rule{
		"state": enum("disconnected", "connecting", "started"), "peer": text(63),
		"since": nullable(stamp), "last_rx_at": nullable(stamp), "connection_seq": unsigned,
		"gi": object(map[string]rule{
			"state":      enum("none", "running", "complete"),
			"started_at": nullable(stamp), "completed_at": nullable(stamp),
		}),
	}),
	"points": validPoints,
})

func decodeState(body []byte) (ccState, error) {
	var state ccState
	if !utf8.Valid(body) {
		return state, errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	value, err := readJSON(d, 0)
	if err != nil {
		return state, err
	}
	if _, err := d.Token(); err != io.EOF {
		return state, errors.New("trailing JSON")
	}
	if !stateRule(value) {
		return state, errors.New("expected a complete CC state schema 2")
	}
	// The generic validation above also checks fields not retained in the ring.
	err = json.Unmarshal(body, &state)
	return state, err
}
