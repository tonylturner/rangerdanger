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

// nodeSimURL resolves one node, never the first sim in manifest order.
func nodeSimURL(gen *lifecycle.Generation, node string) (string, bool) {
	service, ok := gen.Manifest.ServiceByNode(node)
	if !ok {
		return "", false
	}
	url, ok := service.Endpoints[manifest.EndpointSim]
	return url, ok
}

// solverURL is the process solver's API base URL, if it has one.
func solverURL(gen *lifecycle.Generation) (string, bool) {
	return gen.Manifest.Endpoint(manifest.RoleProcessSolver, "api")
}

// fieldState is GET /api/field/state.
type fieldState struct {
	Devices map[string]json.RawMessage `json:"devices"`
	Process json.RawMessage            `json:"process"`
}

// handleFieldState is the device state and the solver's electrical
// result; it fails when either does.
func (s *Server) handleFieldState(c *gin.Context) {
	gen := rangeOf(c)
	solver, ok := solverURL(gen)
	if !ok {
		notSupported(c)
		return
	}
	devices := map[string]json.RawMessage{}
	for _, service := range gen.Manifest.Services {
		url, ok := service.Endpoints[manifest.EndpointSim]
		if !ok {
			continue
		}
		if service.Node == "" {
			c.JSON(http.StatusBadGateway, gin.H{"error": "sim service " + service.Key + " has no node id"})
			return
		}
		state, err := fetchUpstreamObject(c.Request.Context(), service.Node, url+"/api/state")
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		devices[service.Node] = state
	}
	if len(devices) == 0 {
		notSupported(c)
		return
	}
	electrical, err := fetchUpstreamObject(c.Request.Context(), solverService, solver+"/api/electrical")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, fieldState{Devices: devices, Process: electrical})
}

// handleFieldAudit is the device audit after ?after=.
func (s *Server) handleFieldAudit(c *gin.Context) {
	device, ok := nodeSimURL(rangeOf(c), "breaker-1")
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

// postStateAction sends one action and rejects transport-only acknowledgements.
func postStateAction(ctx context.Context, service, device, path string, body []byte) (json.RawMessage, error) {
	status, answer, err := callUpstream(ctx, http.MethodPost, device+path, body, upstreamReadTimeout)
	if err != nil {
		return nil, fmt.Errorf("%s unreachable: %w", service, err)
	}
	if status != http.StatusOK {
		return nil, &upstreamError{service: service, status: status, body: answer}
	}
	state, err := jsonObject(service, answer)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(state, &fields); err != nil {
		return nil, err
	}
	if _, acknowledgement := fields["accepted"]; acknowledgement {
		return nil, fmt.Errorf("%s returned an action acknowledgement instead of state", service)
	}
	return state, nil
}
