package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// Operator routes: the range's control centre (CC), the operator's only
// view of the process. The backend relays the CC's own state, commands and
// journal; it never reads device truth for them.

// commandTimeout bounds one CC command. The CC answers within 10 s.
const commandTimeout = 15 * time.Second

// controlCentreService names the CC in errors.
const controlCentreService = "control centre"

// commandPassThrough are the CC command statuses relayed with their body:
// bad input, a command already in flight, the link not started.
var commandPassThrough = map[int]bool{
	http.StatusBadRequest:         true,
	http.StatusConflict:           true,
	http.StatusServiceUnavailable: true,
}

// controlCentreURL is the CC API base URL, if the package has a CC.
func controlCentreURL(gen *lifecycle.Generation) (string, bool) {
	return gen.Manifest.Endpoint(manifest.RoleControlCentre, "api")
}

// fetchOperatorState is the CC's GET /api/state, decoded field by field.
func fetchOperatorState(ctx context.Context, cc string) (map[string]json.RawMessage, error) {
	body, err := fetchUpstreamObject(ctx, controlCentreService, cc+"/api/state")
	if err != nil {
		return nil, err
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, fmt.Errorf("%s state: %w", controlCentreService, err)
	}
	return state, nil
}

// handleOperatorState is the CC state plus the range it belongs to.
func (s *Server) handleOperatorState(c *gin.Context) {
	gen := rangeOf(c)
	cc, ok := controlCentreURL(gen)
	if !ok {
		notSupported(c)
		return
	}
	state, err := fetchOperatorState(c.Request.Context(), cc)
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	state["package_id"], _ = json.Marshal(gen.Package.ID)
	state["range_generation"], _ = json.Marshal(gen.ID)
	c.JSON(http.StatusOK, state)
}

// handleOperatorCommand relays one command to the CC, once: the CC runs
// select, execute and feedback synchronously and the backend never retries.
func (s *Server) handleOperatorCommand(c *gin.Context) {
	cc, ok := controlCentreURL(rangeOf(c))
	if !ok {
		notSupported(c)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxRouteRequestBody))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body too large or unreadable"})
		return
	}
	status, answer, err := callUpstream(c.Request.Context(), http.MethodPost, cc+"/api/commands", body, commandTimeout)
	if err != nil {
		upstreamFailed(c, fmt.Errorf("%s unreachable: %w", controlCentreService, err))
		return
	}
	if status != http.StatusOK && !commandPassThrough[status] {
		upstreamFailed(c, &upstreamError{service: controlCentreService, status: status, body: answer})
		return
	}
	result, err := jsonObject(controlCentreService, answer)
	switch {
	case err == nil:
		c.Data(status, "application/json; charset=utf-8", result)
	case status == http.StatusOK:
		upstreamFailed(c, err)
	default:
		// A refusal without a JSON body keeps its status and text.
		c.JSON(status, gin.H{"error": strings.TrimSpace(string(answer))})
	}
}

// handleOperatorJournal is the CC journal after ?after=.
func (s *Server) handleOperatorJournal(c *gin.Context) {
	cc, ok := controlCentreURL(rangeOf(c))
	if !ok {
		notSupported(c)
		return
	}
	after, ok := afterParam(c)
	if !ok {
		return
	}
	journal, err := fetchUpstreamObject(c.Request.Context(), controlCentreService, fmt.Sprintf("%s/api/journal?after=%d", cc, after))
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", journal)
}

// handleOperatorGI requests interrogation once; only the CC link thread sends it.
func (s *Server) handleOperatorGI(c *gin.Context) {
	cc, ok := controlCentreURL(rangeOf(c))
	if !ok {
		notSupported(c)
		return
	}
	status, answer, err := callUpstream(c.Request.Context(), http.MethodPost, cc+"/api/gi", nil, upstreamReadTimeout)
	if err != nil {
		upstreamFailed(c, fmt.Errorf("%s unreachable: %w", controlCentreService, err))
		return
	}
	if status != http.StatusAccepted && status != http.StatusConflict && status != http.StatusServiceUnavailable {
		upstreamFailed(c, &upstreamError{service: controlCentreService, status: status, body: answer})
		return
	}
	result, err := jsonObject(controlCentreService, answer)
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	c.Data(status, "application/json; charset=utf-8", result)
}
