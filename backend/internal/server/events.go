package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// handleGetFirewallHealth returns the containd firewall health status.
func (s *Server) handleGetFirewallHealth(c *gin.Context) {
	health, err := rangeOf(c).Containd().GetHealth(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unavailable",
			"error":   err.Error(),
			"message": "containd firewall not reachable",
		})
		return
	}

	// Liveness remains status=ok even when forwarding/binding/recovery is
	// degraded. A diagnostic read failure is visible, not a false outage.
	reservation, reservationErr := rangeOf(c).Containd().GetManagementReservation(c.Request.Context())
	response := struct {
		*containd.HealthStatus
		ManagementReservation      json.RawMessage `json:"managementReservation,omitempty"`
		ManagementReservationError string          `json:"managementReservationError,omitempty"`
	}{HealthStatus: health, ManagementReservation: reservation}
	if reservationErr != nil {
		response.ManagementReservationError = reservationErr.Error()
	}
	c.JSON(http.StatusOK, response)
}

// handleGetFirewallFlows returns containd's engine flow table. An
// optional ?limit= within containd's own bounds is passed through.
func (s *Server) handleGetFirewallFlows(c *gin.Context) {
	limit := containd.DefaultFlowLimit
	if q := c.Query("limit"); q != "" {
		v, err := strconv.Atoi(q)
		if err != nil || v < 1 || v > containd.MaxFlowLimit {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("limit must be an integer in 1..%d", containd.MaxFlowLimit)})
			return
		}
		limit = v
	}

	flows, err := rangeOf(c).Containd().GetFlows(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"flows": []containd.Flow{},
			"error": err.Error(),
		})
		return
	}
	if flows == nil {
		flows = []containd.Flow{}
	}

	c.JSON(http.StatusOK, gin.H{"flows": flows})
}

// handleGetFirewallRules returns summarized firewall rules grouped by zone
// pairs. While containd is unreachable a package with static summaries
// shows those; any other package fails.
func (s *Server) handleGetFirewallRules(c *gin.Context) {
	gen := rangeOf(c)
	summaries, err := gen.Containd().GetZoneRuleSummaries(c.Request.Context())
	if err != nil {
		recipe, recipeErr := recipeFor(gen)
		if recipeErr != nil || recipe.staticRules == nil {
			upstreamFailed(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"summaries": recipe.staticRules,
			"source":    "static",
			"error":     err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"summaries": summaries,
		"source":    "containd",
	})
}
