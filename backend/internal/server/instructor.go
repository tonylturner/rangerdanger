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

// Instructor routes: out-of-band truth of the range's instructor device
// (the one service with an instructor endpoint) and of its process solver.
// They are for the instructor page and the validator, never for the
// operator view.

const (
	instructorService = "instructor device"
	solverService     = "process solver"
)

// instructorURL is the instructor device's API base URL, if any.
func instructorURL(gen *lifecycle.Generation) (string, bool) {
	_, url, ok := gen.Manifest.ServiceWithEndpoint(manifest.EndpointInstructor)
	return url, ok
}

// solverURL is the process solver's API base URL, if it has one.
func solverURL(gen *lifecycle.Generation) (string, bool) {
	return gen.Manifest.Endpoint(manifest.RoleProcessSolver, "api")
}

// instructorState is GET /api/instructor/state.
type instructorState struct {
	Device  json.RawMessage `json:"device"`
	Process json.RawMessage `json:"process"`
}

// handleInstructorState is the device state and the solver's electrical
// result; it fails when either does.
func (s *Server) handleInstructorState(c *gin.Context) {
	gen := rangeOf(c)
	device, ok := instructorURL(gen)
	if !ok {
		notSupported(c)
		return
	}
	solver, ok := solverURL(gen)
	if !ok {
		notSupported(c)
		return
	}
	state, err := fetchUpstreamObject(c.Request.Context(), instructorService, device+"/api/state")
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	electrical, err := fetchUpstreamObject(c.Request.Context(), solverService, solver+"/api/electrical")
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	c.JSON(http.StatusOK, instructorState{Device: state, Process: electrical})
}

// handleInstructorAudit is the device audit after ?after=.
func (s *Server) handleInstructorAudit(c *gin.Context) {
	device, ok := instructorURL(rangeOf(c))
	if !ok {
		notSupported(c)
		return
	}
	after, ok := afterParam(c)
	if !ok {
		return
	}
	audit, err := fetchUpstreamObject(c.Request.Context(), instructorService, fmt.Sprintf("%s/api/audit?after=%d", device, after))
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", audit)
}

// handleInstructorFault sets the device's fault and answers its new state.
func (s *Server) handleInstructorFault(c *gin.Context) {
	device, ok := instructorURL(rangeOf(c))
	if !ok {
		notSupported(c)
		return
	}
	var req struct {
		Active *bool `json:"active"`
	}
	if !bindStrictJSON(c, &req) {
		return
	}
	if req.Active == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "active is required"})
		return
	}
	body, _ := json.Marshal(map[string]bool{"active": *req.Active})
	s.instructorAction(c, device, "/api/instructor/fault", body)
}

// handleInstructorReset restores the device's initial state and answers it.
func (s *Server) handleInstructorReset(c *gin.Context) {
	device, ok := instructorURL(rangeOf(c))
	if !ok {
		notSupported(c)
		return
	}
	var req struct{}
	if !bindStrictJSON(c, &req) {
		return
	}
	s.instructorAction(c, device, "/api/instructor/reset", []byte(`{}`))
}

// instructorAction posts one instructor action and answers the device's
// state after it.
func (s *Server) instructorAction(c *gin.Context, device, path string, body []byte) {
	state, err := postInstructorAction(c.Request.Context(), device, path, body)
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", state)
}

// postInstructorAction posts an instructor action, then reads the device
// state. The device acknowledges an action without its state, so the
// state is read after it.
func postInstructorAction(ctx context.Context, device, path string, body []byte) (json.RawMessage, error) {
	status, answer, err := callUpstream(ctx, http.MethodPost, device+path, body, upstreamReadTimeout)
	if err != nil {
		return nil, fmt.Errorf("%s unreachable: %w", instructorService, err)
	}
	if status != http.StatusOK {
		return nil, &upstreamError{service: instructorService, status: status, body: answer}
	}
	return fetchUpstreamObject(ctx, instructorService, device+"/api/state")
}
