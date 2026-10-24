// SPDX-License-Identifier: Apache-2.0

package pointmap

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile("../../check/points.test.json")
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func entry(d map[string]any, section string, i int) map[string]any {
	return d[section].([]any)[i].(map[string]any)
}
func source(d map[string]any, i int) map[string]any {
	return entry(d, "points", i)["source"].(map[string]any)
}
func TestLoaderScenarios(t *testing.T) {
	// All 42 scenarios from check/loader_checks.py, including its six
	// successful loads. Expected errors are literal C loader diagnostics.
	type scenario struct {
		name string
		edit func(map[string]any)
		want string
	}
	cases := []scenario{
		{"schema", func(d map[string]any) { d["schema"] = d["schema"].(float64) - 1 }, `root: "schema" must be an integer`},
		{"address", func(d map[string]any) { source(d, 0)["address"] = 17 }, "point feeder-breaker-position: source outside poll block"},
		{"table", func(d map[string]any) { source(d, 0)["table"] = "holding" }, "point feeder-breaker-position: source outside poll block"},
		{"f32-boundary", func(d map[string]any) { source(d, 2)["address"] = 16 }, "point feeder-current: source outside poll block"},
		{"format-kind", func(d map[string]any) { source(d, 0)["format"] = "u16" }, "point feeder-breaker-position: kind and source.format disagree"},
		{"format-unknown", func(d map[string]any) { source(d, 2)["format"] = "bogus" }, "point feeder-current: unknown source.format"},
		{"feedback-kind", func(d map[string]any) { entry(d, "commands", 0)["feedback"] = "feeder-current" }, "command feeder-breaker-command: requires holding/SBO and same-device typed feedback"},
		{"feedback-device", func(d map[string]any) { entry(d, "commands", 0)["feedback"] = "rmu-1-position" }, "command feeder-breaker-command: requires holding/SBO and same-device typed feedback"},
		{"heartbeat-range", func(d map[string]any) { entry(d, "devices", 4)["heartbeat"].(map[string]any)["address"] = 1030 }, `heartbeat: "address" must be an integer`},
		{"transient-range", func(d map[string]any) { source(d, 18)["transient"].(map[string]any)["address"] = 21 }, "point oltc-1-tap: source outside poll block"},
		{"ioa-duplicate", func(d map[string]any) { entry(d, "points", 1)["ioa"] = 1001 }, "duplicate IOA or id: feeder-protection-trip"},
		{"device-duplicate", func(d map[string]any) { entry(d, "devices", 1)["id"] = "feeder-breaker" }, `point rmu-1-position: source.device "rmu-1" is not a defined device`},
		{"command-duplicate", func(d map[string]any) { entry(d, "commands", 1)["id"] = "feeder-breaker-command" }, "duplicate IOA or id: feeder-breaker-command"},
		{"asdu", func(d map[string]any) { entry(d, "points", 0)["asdu"] = "M_ST_TB_1" }, `feeder-breaker-position: "asdu" must be "M_DP_TB_1" for this kind, not "M_ST_TB_1"`},
		{"scale", func(d map[string]any) { source(d, 2)["scale"] = 0.1 }, "point feeder-current: scale requires u16/i16"},
		{"valid-bit-range", func(d map[string]any) { entry(d, "devices", 0)["measurements_valid"].(map[string]any)["address"] = 17 }, "device feeder-breaker: measurements_valid outside poll block"},
		{"limits-order", func(d map[string]any) { entry(d, "points", 3)["limits"] = map[string]any{"low": 21, "high": 19} }, "point feeder-voltage: invalid voltage limits"},
		{"limits-extra", func(d map[string]any) {
			entry(d, "points", 3)["limits"] = map[string]any{"low": 19, "high": 21, "extra": 0}
		}, `limits: unknown key "extra"`},
		{"limits-kind", func(d map[string]any) { entry(d, "points", 0)["limits"] = map[string]any{"low": 19, "high": 21} }, "point feeder-breaker-position: invalid voltage limits"},
	}
	for _, spec := range []struct {
		key       string
		low, high int
	}{{"gi_interval_ms", 60000, 86400000}, {"cyclic_ms", 1000, 3600000}} {
		target := func(d map[string]any) map[string]any {
			if spec.key == "gi_interval_ms" {
				return d
			}
			return entry(d, "points", 2)
		}
		where := "point"
		if spec.key == "gi_interval_ms" {
			where = "root"
		}
		cases = append(cases, scenario{spec.key + "-missing", func(d map[string]any) { delete(target(d), spec.key) }, fmt.Sprintf("%s: missing required key %q", where, spec.key)})
		for i, bad := range []any{nil, true, "1000", 1000.5, spec.low - 1, spec.high + 1} {
			cases = append(cases, scenario{fmt.Sprintf("%s-bad-%d", spec.key, i), func(d map[string]any) { target(d)[spec.key] = bad }, fmt.Sprintf("%s: %q must be an integer", where, spec.key)})
		}
		for _, boundary := range []int{spec.low, spec.high} {
			cases = append(cases, scenario{fmt.Sprintf("%s-boundary-%d", spec.key, boundary), func(d map[string]any) { target(d)[spec.key] = boundary }, ""})
		}
	}
	for _, i := range []int{0, 1, 18} {
		id := entry(fixture(t), "points", i)["id"]
		cases = append(cases, scenario{fmt.Sprintf("cyclic-kind-%d", i), func(d map[string]any) { entry(d, "points", i)["cyclic_ms"] = 1000 }, fmt.Sprintf("point %s: \"cyclic_ms\" applies only to float points", id)})
	}
	cases = append(cases, scenario{"original", func(map[string]any) {}, ""}, scenario{"signed", func(d map[string]any) { source(d, 2)["format"], source(d, 2)["scale"] = "i16", 0.1 }, ""})
	if len(cases) != 42 {
		t.Fatalf("have %d scenarios", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := fixture(t)
			tc.edit(d)
			b, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			m, err := Parse(b, "test")
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if tc.want == "" && (len(m.Points) != 42 || len(m.Devices) != 6 || len(m.Commands) != 5) {
				t.Fatalf("incomplete configuration: %+v", m)
			}
		})
	}
}

func TestStrictJSON(t *testing.T) {
	b, err := os.ReadFile("../../check/points.test.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, data, want string }{
		{"trailing", string(b) + " {}", "test: invalid JSON"},
		{"nul", string(b) + "\x00", "test: invalid JSON"},
		{"duplicate", `{"schema":2,"schema":2}`, "root: duplicate key schema"},
		{"unknown", `{"bogus":0}`, `root: unknown key "bogus"`},
		{"compound-key", `{"ca timers":0}`, `root: unknown key "ca timers"`},
		{"empty", "", "test: empty or implausibly large"},
		{"huge", strings.Repeat(" ", (1<<20)+1), "test: empty or implausibly large"},
		{"root-not-object", `[]`, "root: must be an object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.data), "test")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestResolvedMap(t *testing.T) {
	m, err := Load("../../check/points.test.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.PointByIOA(1001) != &m.Points[0] || m.CommandByIOA(3001) != &m.Commands[0] ||
		m.PointByIOA(0) != nil || m.CommandByIOA(0) != nil {
		t.Fatal("IOA lookup")
	}
	for _, c := range m.Commands {
		if m.Points[c.FeedbackIndex].ID != c.FeedbackID || m.Points[c.FeedbackIndex].DeviceIndex != c.DeviceIndex {
			t.Fatal("unresolved feedback")
		}
	}
	if !m.Clock.StatusSynced("locked") || m.Clock.StatusSynced("bogus") {
		t.Fatalf("statuses: %v", m.Clock.SyncedStatus)
	}
}
