package server

import (
	"context"
	"fmt"
	"sort"
	"time"

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
	if state.Schema != 2 || state.Generation == 0 || state.Revision == nil || state.DeviceID != id {
		return nil, fmt.Errorf("%s has no schema-2 identity/generation/revision", node)
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
	if state == nil || state.Schema != 2 || state.Stale == nil || *state.Stale || !insideEvidenceWindow(state.SolvedAt, now.Add(-3*time.Second), now) {
		return false, fmt.Errorf("no fresh schema-2 process solve (≤3 s)")
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

// A persistent healthy connection need not be cut every ten minutes just to
// check health. GI must belong to the current connection and cache epoch;
// freshness belongs to received observations, not the age of link_up.
func euLink(state *conduitState, journal *ccJournal, start, now time.Time) ValidationCheck {
	const name = "Fresh CC link, GI and station observations"
	fail := func() ValidationCheck {
		return checkFail(name, "missing current-connection GI or fresh GOOD station observations")
	}
	if state == nil || journal == nil || state.Schema != 2 || state.CacheEpoch == "" || state.CacheEpoch != journal.CacheEpoch || state.Link.State != "started" || state.Link.ConnectionSeq == 0 {
		return fail()
	}
	gi := false
	for _, entry := range journal.Entries {
		if entry.Kind == "gi_complete" && entry.Detail.ConnectionSeq != nil && *entry.Detail.ConnectionSeq == state.Link.ConnectionSeq && insideEvidenceWindow(entry.At, start, now) {
			gi = true
		}
	}
	if !gi {
		return fail()
	}
	check := conduitPolls(conduitEvidence{state: state, collectedAt: now}, start)
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
	state, err := fetchJSON[conduitState](ctx, controlCentreService, endpoint+"/api/state")
	return state, journal, err
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
				check := euLink(state, journal, gen.StartedAt, time.Now().UTC())
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
