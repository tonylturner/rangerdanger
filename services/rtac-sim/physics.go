package main

import (
	"encoding/json"
	"fmt"
	"time"
)

const physicsStaleAfter = 3

// PhysicsStatus summarizes the freshness of the last OpenDSS solve accepted
// by the RTAC.
type PhysicsStatus struct {
	Stale               bool      `json:"stale"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastGoodAt          time.Time `json:"last_good_at"`
	SolvedAt            string    `json:"solved_at"`
	LastError           string    `json:"last_error"`
}

// applyPhysicsReply applies one OpenDSS push result to state. The caller must
// hold state.mu; now is injected so the state transition is deterministic in
// tests. Electrical retains its last good solve after any failed push.
func applyPhysicsReply(state *AggregatedState, status int, body []byte, pushErr error, now time.Time) error {
	if pushErr == nil && (status < 200 || status >= 300) {
		pushErr = fmt.Errorf("physics push returned HTTP status %d", status)
	}

	var electrical map[string]any
	if pushErr == nil {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(body, &object); err != nil || object == nil {
			pushErr = fmt.Errorf("physics reply is not a JSON object")
		} else if rawConverged, ok := object["converged"]; !ok {
			pushErr = fmt.Errorf("physics reply did not report converged=true")
		} else {
			var converged bool
			if err := json.Unmarshal(rawConverged, &converged); err != nil || !converged {
				pushErr = fmt.Errorf("physics reply did not report converged=true")
			} else if err := json.Unmarshal(body, &electrical); err != nil || electrical == nil {
				pushErr = fmt.Errorf("physics reply is not a JSON object")
			}
		}
	}

	if pushErr != nil {
		state.Physics.ConsecutiveFailures++
		state.Physics.LastError = pushErr.Error()
		state.Physics.Stale = physicsIsStale(state.Physics)
		return pushErr
	}

	solvedAt, _ := electrical["solved_at"].(string)
	state.Electrical = electrical
	state.Physics = PhysicsStatus{
		LastGoodAt: now.UTC(),
		SolvedAt:   solvedAt,
	}
	return nil
}

func physicsIsStale(status PhysicsStatus) bool {
	return status.LastGoodAt.IsZero() || status.ConsecutiveFailures >= physicsStaleAfter
}
