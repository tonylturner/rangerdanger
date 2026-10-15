package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// Field routes expose physical device state and solver output to the portal.
// The control centre itself still learns values only through IEC 104.

const (
	simService    = "sim device"
	solverService = "process solver"
)

// simURL is the sim device's API base URL, if any.
func simURL(gen *lifecycle.Generation) (string, bool) {
	_, url, ok := gen.Manifest.ServiceWithEndpoint(manifest.EndpointSim)
	return url, ok
}

// solverURL is the process solver's API base URL, if it has one.
func solverURL(gen *lifecycle.Generation) (string, bool) {
	return gen.Manifest.Endpoint(manifest.RoleProcessSolver, "api")
}

// fieldState is GET /api/field/state.
type fieldState struct {
	Device  json.RawMessage `json:"device"`
	Process json.RawMessage `json:"process"`
}

// handleFieldState is the device state and the solver's electrical
// result; it fails when either does.
func (s *Server) handleFieldState(c *gin.Context) {
	gen := rangeOf(c)
	device, ok := simURL(gen)
	if !ok {
		notSupported(c)
		return
	}
	solver, ok := solverURL(gen)
	if !ok {
		notSupported(c)
		return
	}
	state, err := fetchUpstreamObject(c.Request.Context(), simService, device+"/api/state")
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	electrical, err := fetchUpstreamObject(c.Request.Context(), solverService, solver+"/api/electrical")
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	c.JSON(http.StatusOK, fieldState{Device: state, Process: electrical})
}

// handleFieldAudit is the device audit after ?after=.
func (s *Server) handleFieldAudit(c *gin.Context) {
	device, ok := simURL(rangeOf(c))
	if !ok {
		notSupported(c)
		return
	}
	after, ok := afterParam(c)
	if !ok {
		return
	}
	audit, err := fetchUpstreamObject(c.Request.Context(), simService, fmt.Sprintf("%s/api/audit?after=%d", device, after))
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", audit)
}

// postSimAction posts one sim action and requires the device
// to return its state rather than an action acknowledgement.
func postSimAction(ctx context.Context, device, path string, body []byte) (json.RawMessage, error) {
	status, answer, err := callUpstream(ctx, http.MethodPost, device+path, body, upstreamReadTimeout)
	if err != nil {
		return nil, fmt.Errorf("%s unreachable: %w", simService, err)
	}
	if status != http.StatusOK {
		return nil, &upstreamError{service: simService, status: status, body: answer}
	}
	state, err := jsonObject(simService, answer)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(state, &fields); err != nil {
		return nil, err
	}
	if _, acknowledgement := fields["accepted"]; acknowledgement {
		return nil, fmt.Errorf("%s returned an action acknowledgement instead of state", simService)
	}
	return state, nil
}

// resetSimDevice is the workshop reset's step for the sim
// device: its own reset, which restores the initial state.
func resetSimDevice(ctx context.Context, gen *lifecycle.Generation) resetAction {
	const action = "Reset the sim device to its initial state"
	device, ok := simURL(gen)
	if !ok {
		return resetAction{Action: action, Detail: "package " + gen.Package.ID + " has no sim device"}
	}
	_, err := postSimAction(ctx, device, "/api/sim/reset", []byte(`{}`))
	return resetAction{Action: action, Success: err == nil, Detail: boolDetail(err == nil, "device reset", errStr(err))}
}
