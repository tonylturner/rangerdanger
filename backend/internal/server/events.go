package server

import (
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

	c.JSON(http.StatusOK, health)
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

// handleGetFirewallRules returns summarized firewall rules grouped by zone pairs.
func (s *Server) handleGetFirewallRules(c *gin.Context) {
	summaries, err := rangeOf(c).Containd().GetZoneRuleSummaries(c.Request.Context())
	if err != nil {
		// Return fallback static rules if containd is unavailable
		c.JSON(http.StatusOK, gin.H{
			"summaries": getStaticRuleSummaries(),
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

// getStaticRuleSummaries returns fallback rule summaries when containd is unavailable.
func getStaticRuleSummaries() []containd.ZoneRuleSummary {
	return []containd.ZoneRuleSummary{
		{SourceZone: "wan", DestZone: "dmz", Summary: "SSH/HTTP/S", Action: "ALLOW", RuleDetails: []string{"Enterprise to Vendor: Standard protocols"}},
		{SourceZone: "wan", DestZone: "lan1", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: Enterprise direct access to OT Ops (should be blocked)"}},
		{SourceZone: "wan", DestZone: "lan2", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: Enterprise direct access to Field Devices (should be blocked)"}},
		{SourceZone: "dmz", DestZone: "lan1", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: Vendor broad access to OT Ops (should be narrowed)"}},
		{SourceZone: "dmz", DestZone: "lan2", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: Vendor direct access to Field Devices (should be blocked)"}},
		{SourceZone: "lan1", DestZone: "lan2", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: All OT nodes can reach Field Devices (should be RTAC only)"}},
		{SourceZone: "lan1", DestZone: "lan1", Summary: "OT Internal", Action: "ALLOW", RuleDetails: []string{"OT Operations internal communication"}},
	}
}
