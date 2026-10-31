package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// resetEUTruth restores the process before devices, then waits for the
// solver to acknowledge their post-reset revisions. The PLC and historian
// deliberately have no reset calls.
func resetEUTruth(ctx context.Context, gen *lifecycle.Generation, recipe *packageRecipe) []resetAction {
	var actions []resetAction
	revisions := map[string]uint64{}
	steps := []struct{ device, command string }{
		{"process", "reset"},
		{"oltc-1", "reset"},
		{"rmu-2", "reset"},
		{"rmu-1", "reset"},
		{"breaker", "reset"},
		{"f2-breaker", "reset"},
		{"gps", "reset_offset"},
	}
	for _, step := range steps {
		state, err := executeTruthAction(ctx, gen, recipe, step.device, step.command, nil)
		if err == nil && step.device != "process" && step.device != "gps" {
			var reset struct {
				Schema   int     `json:"schema"`
				DeviceID string  `json:"device_id"`
				Revision *uint64 `json:"revision"`
			}
			err = json.Unmarshal(state, &reset)
			expectedID := recipe.simDevices[step.device]
			if step.device == "breaker" {
				expectedID = "feeder-breaker"
			}
			if err == nil && (reset.Schema != 3 || reset.DeviceID != expectedID || reset.Revision == nil) {
				err = fmt.Errorf("%s reset returned no schema-3 state with matching device id/revision", step.device)
			}
			if err == nil {
				revisions[reset.DeviceID] = *reset.Revision
			}
		}
		actions = append(actions, resetAction{
			Action: step.device + "/" + step.command, Success: err == nil,
			Detail: boolDetail(err == nil, "reset completed", errStr(err)),
		})
	}
	err := waitResetDelivery(ctx, gen, revisions)
	actions = append(actions, resetAction{
		Action: "Wait for post-reset measurement delivery", Success: err == nil,
		Detail: boolDetail(err == nil, "all devices delivered at post-reset revisions", errStr(err)),
	})
	return actions
}

func waitResetDelivery(ctx context.Context, gen *lifecycle.Generation, revisions map[string]uint64) error {
	if len(revisions) != 5 {
		return fmt.Errorf("cannot confirm delivery: missing post-reset device revisions")
	}
	solver, ok := solverURL(gen)
	if !ok {
		return fmt.Errorf("process solver has no api endpoint")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := fetchUpstreamObject(ctx, solverService, solver+"/api/electrical")
		if err != nil {
			return err
		}
		ready, err := resetDeliveryReady(state, revisions)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("process solver post-reset delivery: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func resetDeliveryReady(raw json.RawMessage, revisions map[string]uint64) (bool, error) {
	var state struct {
		Schema     int   `json:"schema"`
		TruthStale *bool `json:"truth_stale"`
		Devices    map[string]struct {
			Delivered      bool    `json:"delivered"`
			SolvedRevision *uint64 `json:"solved_revision"`
			Revision       *uint64 `json:"revision"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return false, fmt.Errorf("process solver delivery state: %w", err)
	}
	if state.Schema != 3 {
		return false, fmt.Errorf("process solver delivery state must use schema 3")
	}
	if state.TruthStale == nil || *state.TruthStale {
		return false, nil
	}
	for id := range revisions {
		if _, ok := state.Devices[id]; !ok {
			return false, nil
		}
	}
	for id, device := range state.Devices {
		if !device.Delivered || device.SolvedRevision == nil || device.Revision == nil ||
			*device.SolvedRevision != *device.Revision || *device.Revision < revisions[id] {
			return false, nil
		}
	}
	return true, nil
}
