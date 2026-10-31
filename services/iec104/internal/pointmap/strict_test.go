// SPDX-License-Identifier: Apache-2.0

package pointmap

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestStrictSections(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"null-devices", func(d map[string]any) { d["devices"] = nil }, `"devices" must be a non-empty array`},
		{"null-points", func(d map[string]any) { d["points"] = nil }, `"points" must be a non-empty array`},
		{"null-commands", func(d map[string]any) { d["commands"] = nil }, `"commands" must be an array`},
		{"null-heartbeat", func(d map[string]any) { entry(d, "devices", 0)["heartbeat"] = nil }, `device feeder-breaker: "heartbeat" must be an object`},
		{"unknown-device-key", func(d map[string]any) { entry(d, "devices", 0)["bogus"] = 0 }, `device: unknown key "bogus"`},
		{"unknown-source-key", func(d map[string]any) { source(d, 0)["bogus"] = 0 }, `source: unknown key "bogus"`},
		{"unknown-command-key", func(d map[string]any) { entry(d, "commands", 0)["bogus"] = 0 }, `command: unknown key "bogus"`},
		{"name-length", func(d map[string]any) { entry(d, "points", 0)["id"] = strings.Repeat("a", 64) }, `point: "id" is too long`},
		{"null-name", func(d map[string]any) { entry(d, "points", 0)["id"] = nil }, `point: "id" must be a string`},
		{"duplicate-point", func(d map[string]any) { entry(d, "points", 1)["id"] = "feeder-breaker-position" }, `duplicate point id "feeder-breaker-position"`},
		{"command-point-id", func(d map[string]any) { entry(d, "commands", 0)["id"] = "feeder-current" }, `duplicate IOA or id: feeder-current`},
		{"command-point-ioa", func(d map[string]any) { entry(d, "commands", 0)["ioa"] = 1001 }, `duplicate IOA or id: feeder-breaker-command`},
		{"direct-command", func(d map[string]any) { entry(d, "commands", 0)["sbo"] = false }, `command feeder-breaker-command: requires holding/SBO and same-device typed feedback`},
		{"sbo-type", func(d map[string]any) { entry(d, "commands", 0)["sbo"] = 1 }, `command: "sbo" must be true or false`},
		{"unknown-feedback", func(d map[string]any) { entry(d, "commands", 0)["feedback"] = "missing" }, `command feeder-breaker-command: feedback "missing" is not a defined point`},
		{"poll-overflow", func(d map[string]any) {
			poll := entry(d, "devices", 0)["poll"].(map[string]any)
			poll["start"], poll["count"] = 65535, 2
		}, `device feeder-breaker: poll block overflow`},
		{"bad-transient-bit", func(d map[string]any) { source(d, 18)["transient"].(map[string]any)["bit"] = 16 }, `transient: "bit" must be an integer`},
		{"non-bit", func(d map[string]any) { source(d, 0)["bit"] = 0 }, `point feeder-breaker-position: "bit" is only valid with format "bit"`},
		{"negative-deadband", func(d map[string]any) { entry(d, "points", 2)["deadband"] = -1 }, `point feeder-current: "deadband" must be >= 0`},
		{"status-length", func(d map[string]any) { d["clock"].(map[string]any)["synced_status"] = []any{strings.Repeat("x", 16)} }, `clock: each "synced_status" entry must be a short string`},
		{"status-limit", func(d map[string]any) {
			d["clock"].(map[string]any)["synced_status"] = []any{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
		}, `clock: too many synced_status entries (max 8)`},
		{"device-limit", func(d map[string]any) { a := d["devices"].([]any); d["devices"] = append(a, a[0], a[0], a[0]) }, `too many devices (max 8)`},
		{"point-limit", func(d map[string]any) {
			a := d["points"].([]any)
			for i := len(a); i < 65; i++ {
				pt := map[string]any{}
				for k, v := range a[0].(map[string]any) {
					pt[k] = v
				}
				pt["id"], pt["ioa"] = fmt.Sprintf("extra-%d", i), 10000+i
				a = append(a, pt)
			}
			d["points"] = a
		}, `too many points (max 64)`},
		{"command-limit", func(d map[string]any) {
			a := d["commands"].([]any)
			for len(a) < 17 {
				a = append(a, a[0])
			}
			d["commands"] = a
		}, `too many commands (max 16)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := fixture(t)
			tc.edit(d)
			b, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Parse(b, "test")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
