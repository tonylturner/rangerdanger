package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

type euWorkshopDevice struct {
	Schema     int     `json:"schema"`
	Generation uint32  `json:"generation"`
	Revision   *uint64 `json:"revision"`
	EventSeq   uint64  `json:"event_seq"`
	DeviceID   string  `json:"device_id"`
	Position   string  `json:"position"`
	Remote     *bool   `json:"remote_enabled"`
	Auto       *bool   `json:"auto"`
	Tap        *int    `json:"tap"`
	Blocked    *bool   `json:"blocked"`
	Moving     *bool   `json:"moving"`
}

type euWorkshopProcess struct {
	Schema   int       `json:"schema"`
	Stale    *bool     `json:"truth_stale"`
	SolvedAt time.Time `json:"solved_at"`
	Buses    map[string]struct {
		Live *bool    `json:"live"`
		KV   *float64 `json:"kv"`
	} `json:"buses"`
	Devices map[string]struct {
		Delivered      bool    `json:"delivered"`
		Revision       *uint64 `json:"revision"`
		SolvedRevision *uint64 `json:"solved_revision"`
	} `json:"devices"`
}

func euDevice(ctx context.Context, gen *lifecycle.Generation, node string) (*euWorkshopDevice, error) {
	endpoint, ok := nodeSimURL(gen, node)
	if !ok {
		return nil, fmt.Errorf("no simulation endpoint for %s", node)
	}
	state, err := fetchJSON[euWorkshopDevice](ctx, simService, endpoint+"/api/state")
	if err != nil {
		return nil, err
	}
	id := node
	if node == "breaker-1" {
		id = "feeder-breaker"
	}
	if state.Schema != 3 || state.Generation == 0 || state.Revision == nil || state.DeviceID != id {
		return nil, fmt.Errorf("%s has no schema-3 identity/generation/revision", node)
	}
	return state, nil
}

func euProcess(ctx context.Context, gen *lifecycle.Generation) (*euWorkshopProcess, error) {
	endpoint, ok := solverURL(gen)
	if !ok {
		return nil, fmt.Errorf("no process solver endpoint")
	}
	return fetchJSON[euWorkshopProcess](ctx, solverService, endpoint+"/api/electrical")
}

func euSupply(state *euWorkshopProcess, now time.Time) (bool, error) {
	if state == nil || state.Schema != 3 || state.Stale == nil || *state.Stale || !insideEvidenceWindow(state.SolvedAt, now.Add(-3*time.Second), now) {
		return false, fmt.Errorf("no fresh schema-3 process solve (≤3 s)")
	}
	present := true
	for _, id := range []string{"bus20", "s1", "s2", "s3"} {
		bus, ok := state.Buses[id]
		if !ok || bus.Live == nil || bus.KV == nil {
			return false, fmt.Errorf("missing bus %s supply evidence", id)
		}
		present = present && *bus.Live && *bus.KV > 0
	}
	return present, nil
}

// Integrity requires a matched current-connection GI within the attempt cap.
// Receipt age does not invalidate quiet GOOD status.
func euLink(state *conduitState, journal *ccJournal, points []labs.IEC104Point, start, now time.Time) ValidationCheck {
	const name = "CC link, GI and station delivery"
	ev := conduitEvidence{state: state, journal: journal, stationPoints: points, rangeStart: start, collectedAt: now}
	anchor, window := conduitWindow(ev)
	if anchor.IsZero() {
		window.Name = name
		return window
	}
	check := conduitPolls(ev, anchor)
	check.Name = name
	return check
}

func euCC(ctx context.Context, gen *lifecycle.Generation) (*conduitState, *ccJournal, error) {
	endpoint, ok := controlCentreURL(gen)
	if !ok {
		return nil, nil, fmt.Errorf("no control centre endpoint")
	}
	journal, err := fetchJSON[ccJournal](ctx, controlCentreService, endpoint+"/api/journal?after=0")
	if err != nil {
		return nil, nil, err
	}
	state, err := fetchJSON[euPresenceState](ctx, controlCentreService, endpoint+"/api/state")
	if err != nil {
		return nil, journal, err
	}
	return &state.conduitState, journal, nil
}

// Keep presence-aware decoding local to the new EU engines. Legacy validators
// retain their wire types until the Phase 2 cutover.
type euPresenceState struct{ conduitState }

func (state *euPresenceState) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &state.conduitState); err != nil {
		return err
	}
	var raw struct {
		Points []struct {
			IOA     int                        `json:"ioa"`
			Quality map[string]json.RawMessage `json:"quality"`
		} `json:"points"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, point := range raw.Points {
		for _, key := range []string{"iv", "nt", "sb", "bl", "ov"} {
			value, exists := point.Quality[key]
			// OV is null on digital points, but a measured or step-position
			// point requires a boolean overflow descriptor (cc_state.c).
			digitalOV := key == "ov" && point.IOA < 2000 && point.IOA != 1301
			if !exists || string(value) != "true" && string(value) != "false" && !(digitalOV && string(value) == "null") {
				return fmt.Errorf("IOA %d missing or invalid quality.%s", point.IOA, key)
			}
		}
	}
	return nil
}

func euRMUNormal(one, two *euWorkshopDevice) bool {
	return one.Position == "closed" && two.Position == "open"
}
func euOLTCAuto(state *euWorkshopDevice) (bool, error) {
	if state.Remote == nil || state.Auto == nil || state.Tap == nil {
		return false, fmt.Errorf("OLTC mode/tap evidence missing")
	}
	return *state.Remote && *state.Auto && *state.Tap >= -8 && *state.Tap <= 8, nil
}

func (s *Server) executeEUCheck(ctx context.Context, gen *lifecycle.Generation, expect map[string]any) []StepActionResult {
	keys := make([]string, 0, len(expect))
	for key := range expect {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var results []StepActionResult
	for _, key := range keys {
		result := StepActionResult{Action: key}
		var actual bool
		var err error
		switch key {
		case "breaker_closed":
			var state *euWorkshopDevice
			state, err = euDevice(ctx, gen, "breaker-1")
			if err == nil {
				if state.Position != "closed" && state.Position != "open" {
					err = fmt.Errorf("breaker position missing")
				} else {
					actual = state.Position == "closed"
				}
			}
		case "supply_present":
			var state *euWorkshopProcess
			state, err = euProcess(ctx, gen)
			if err == nil {
				actual, err = euSupply(state, time.Now().UTC())
			}
		case "cc_link_fresh":
			var state *conduitState
			var journal *ccJournal
			state, journal, err = euCC(ctx, gen)
			if err == nil {
				check := euLink(state, journal, gen.Package.StationPoints, gen.StartedAt, time.Now().UTC())
				actual = check.Status == "pass"
				// Unknown/missing observations are not a measured false value.
				if !actual {
					err = fmt.Errorf("%s", check.Detail)
				}
			}
		case "rmu_normal":
			var one, two *euWorkshopDevice
			one, err = euDevice(ctx, gen, "rmu-1")
			if err == nil {
				two, err = euDevice(ctx, gen, "rmu-2")
			}
			if err == nil {
				if (one.Position != "closed" && one.Position != "open") || (two.Position != "closed" && two.Position != "open") {
					err = fmt.Errorf("RMU positions missing")
				} else {
					actual = euRMUNormal(one, two)
				}
			}
		case "oltc_auto":
			var state *euWorkshopDevice
			state, err = euDevice(ctx, gen, "oltc-1")
			if err == nil {
				actual, err = euOLTCAuto(state)
			}
		case "firewall_config":
			s.activeConfigMu.RLock()
			active := s.activeConfig
			s.activeConfigMu.RUnlock()
			expected, ok := expect[key].(string)
			result.Success = ok && (active == expected || expected == "improved" && active == "custom")
			result.Detail = fmt.Sprintf("Expected %v; active config: %s", expect[key], active)
			results = append(results, result)
			continue
		default:
			err = fmt.Errorf("unknown EU check key %q", key)
		}
		if err != nil {
			result.Detail = "Missing evidence: " + err.Error()
		} else {
			expected, ok := expect[key].(bool)
			result.Success = ok && actual == expected
			result.Detail = fmt.Sprintf("Expected %v; observed %t", expect[key], actual)
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		return []StepActionResult{{Action: "Check", Detail: "No conditions to verify"}}
	}
	return results
}
