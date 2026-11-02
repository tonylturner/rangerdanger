package server

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

// handleFieldAction exposes only fault injection and latch recovery, never
// switching or protocol commands. executeTruthAction preserves lab attribution.
func (s *Server) handleFieldAction(c *gin.Context) {
	gen := rangeOf(c)
	if !slices.Contains(gen.Package.Capabilities, labs.CapabilityProcessTruth) {
		notSupported(c)
		return
	}
	recipe, err := recipeFor(gen)
	if err != nil {
		notSupported(c)
		return
	}
	var body struct {
		Device string `json:"device"`
		Action string `json:"action"`
	}
	if !bindStrictJSON(c, &body) {
		return
	}
	allowed := false
	switch body.Device {
	case "process":
		allowed = body.Action == "fault_s1" || body.Action == "fault_s2" || body.Action == "clear_fault"
	case "feeder-breaker", "f2-breaker":
		allowed = body.Action == "reset_lockout"
	case "rmu-1", "rmu-2":
		allowed = body.Action == "reset_fault_passage"
	}
	if !allowed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported field simulation action"})
		return
	}
	// The public physical device ID and the scenario vocabulary differ:
	// feeder-breaker is named "breaker" by ValidateTruthCommand and the recipe.
	device := body.Device
	if device == "feeder-breaker" {
		device = "breaker"
	}
	state, err := executeTruthAction(c.Request.Context(), gen, recipe, device, body.Action, nil)
	if err != nil {
		upstreamFailed(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", state)
}
