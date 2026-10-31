package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// executeCommand dispatches exclusively through the active package's recipe.
func (s *Server) executeCommand(ctx context.Context, gen *lifecycle.Generation, device, command, source string, value *float64) StepActionResult {
	recipe, err := recipeFor(gen)
	if err != nil {
		return StepActionResult{Action: device + "/" + command, Detail: err.Error()}
	}
	if recipe.commandExecutor == nil {
		return StepActionResult{Action: device + "/" + command, Detail: "device commands not supported by this package"}
	}
	return recipe.commandExecutor(s, ctx, gen, recipe, device, command, source, value)
}

// executeSimCommand uses the out-of-band simulation control plane, not RTAC
// or an operator protocol. The device attributes these actions to "lab".
func (s *Server) executeSimCommand(ctx context.Context, gen *lifecycle.Generation, recipe *packageRecipe, device, command, source string, value *float64) StepActionResult {
	result := StepActionResult{Action: device + "/" + command}
	if source != "" {
		result.Detail = "simulation actions do not accept a source override"
		return result
	}
	_, err := executeTruthAction(ctx, gen, recipe, device, command, value)
	result.Success = err == nil
	result.Detail = boolDetail(err == nil, "Simulation action completed", errStr(err))
	return result
}

// executeTruthAction sends a lab action once, without any protocol fallback.
func executeTruthAction(ctx context.Context, gen *lifecycle.Generation, recipe *packageRecipe, device, command string, value *float64) (json.RawMessage, error) {
	if err := labs.ValidateTruthCommand(device, command, value); err != nil {
		return nil, err
	}
	var url, path string
	var ok bool
	if device == "process" {
		url, ok = solverURL(gen)
		path = "/api/process/actions"
	} else {
		url, ok = nodeSimURL(gen, recipe.simDevices[device])
		path = "/api/sim/actions"
	}
	if !ok {
		return nil, fmt.Errorf("no simulation control endpoint for %s", device)
	}
	if device == "gps" {
		body, err := json.Marshal(struct {
			Command string   `json:"command"`
			Value   *float64 `json:"value,omitempty"`
			Source  string   `json:"source"`
		}{command, value, "lab"})
		if err != nil {
			return nil, err
		}
		answer, err := postStateAction(ctx, device, url, "/api/command", body)
		if err != nil {
			return nil, err
		}
		var result struct {
			Result string `json:"result"`
		}
		if err := json.Unmarshal(answer, &result); err != nil {
			return nil, err
		}
		if result.Result != "executed" {
			return nil, fmt.Errorf("gps command did not execute: %s", answer)
		}
		return answer, nil
	}
	body, err := json.Marshal(struct {
		Action string   `json:"action"`
		Value  *float64 `json:"value,omitempty"`
	}{command, value})
	if err != nil {
		return nil, err
	}
	return postStateAction(ctx, device, url, path, body)
}

type sessionCutSpec struct {
	sourceNetwork, targetNetwork, transport string
}

// executeSessionCut looks up a fresh exact tuple. Missing, incomplete,
// ambiguous or truncated inventories never cause a deletion.
func (s *Server) executeSessionCut(ctx context.Context, gen *lifecycle.Generation, flowID string) StepActionResult {
	result := StepActionResult{Action: "Cut session: " + flowID}
	recipe, err := recipeFor(gen)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	spec, ok := recipe.sessionCuts[flowID]
	if !ok {
		result.Detail = "session cut not supported for flow " + flowID
		return result
	}
	for _, flow := range gen.Package.Template.Flows {
		if flow.ID != flowID {
			continue
		}
		src, err := nodeIP(gen, flow.Source, spec.sourceNetwork)
		if err != nil {
			result.Detail = err.Error()
			return result
		}
		dst, err := nodeIP(gen, flow.Target, spec.targetNetwork)
		if err != nil {
			result.Detail = err.Error()
			return result
		}
		sessions, err := gen.Containd().ListSessions(ctx, sessionListLimit)
		if err != nil {
			result.Detail = err.Error()
			return result
		}
		if len(sessions) >= sessionListLimit {
			result.Detail = "session inventory may be truncated; nothing was killed"
			return result
		}
		count := 0
		var tuple containd.SessionTuple
		for _, session := range sessions {
			if session.Proto != spec.transport || session.Src != src || session.Dst != dst || session.Dport == nil || *session.Dport != flow.Port {
				continue
			}
			count++
			tuple = containd.SessionTuple{Proto: session.Proto, Src: src, Dst: dst, Dport: flow.Port}
			if session.Sport != nil {
				tuple.Sport = *session.Sport
			}
		}
		if count != 1 || validateSessionTuple(tuple) != nil {
			result.Detail = fmt.Sprintf("need exactly one complete %s session (found %d); nothing was killed", flowID, count)
			return result
		}
		_, err = gen.Containd().KillSession(ctx, tuple)
		result.Success = err == nil
		result.Detail = boolDetail(err == nil, "Deleted "+tupleText(tuple)+"; exact tuple revoked in both directions for 60 seconds; a fresh source port still meets the active policy", errStr(err))
		return result
	}
	result.Detail = "package has no flow " + flowID
	return result
}
