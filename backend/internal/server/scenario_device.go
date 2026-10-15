package server

import (
	"context"
	"fmt"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
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
	service, url, ok := gen.Manifest.ServiceWithEndpoint(manifest.EndpointSim)
	if !ok || recipe.simDevices[device] == "" || service.Node != recipe.simDevices[device] {
		result.Detail = "no simulation control endpoint for device " + device
		return result
	}
	if source != "" || value != nil {
		result.Detail = "simulation actions do not accept a source override or value"
		return result
	}
	path, body := "", ""
	switch command {
	case "inject_fault":
		path, body = "/api/sim/fault", `{"active":true}`
	case "clear_fault":
		path, body = "/api/sim/fault", `{"active":false}`
	case "reset":
		path, body = "/api/sim/reset", `{}`
	default:
		result.Detail = "unknown simulation command " + command
		return result
	}
	_, err := postSimAction(ctx, url, path, []byte(body))
	result.Success = err == nil
	result.Detail = boolDetail(err == nil, "Device simulation action completed", errStr(err))
	return result
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
		err = gen.Containd().KillSession(ctx, tuple)
		result.Success = err == nil
		result.Detail = boolDetail(err == nil, "Deleted "+tupleText(tuple)+"; the next packet meets the active policy", errStr(err))
		return result
	}
	result.Detail = "package has no flow " + flowID
	return result
}
