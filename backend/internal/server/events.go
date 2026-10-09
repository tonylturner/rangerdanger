package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// handleGetLiveEvents streams events via SSE (Server-Sent Events).
func (s *Server) handleGetLiveEvents(c *gin.Context) {
	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	// Track last event ID for polling
	lastEventID := ""

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Check if containd is available
	if !s.containdClient.IsAvailable(c.Request.Context()) {
		// Send fallback stub events
		s.sendStubEvents(c)
		return
	}

	for {
		select {
		case <-ticker.C:
			// Poll containd for events
			events, err := s.containdClient.GetEvents(c.Request.Context(), lastEventID, 10)
			if err != nil {
				// Send error event
				c.SSEvent("error", gin.H{"message": err.Error()})
				c.Writer.Flush()
				continue
			}

			// Send each event
			for _, e := range events {
				eventData, _ := json.Marshal(e)
				c.SSEvent("event", string(eventData))
				lastEventID = string(e.ID)
			}
			c.Writer.Flush()

		case <-c.Request.Context().Done():
			return
		}
	}
}

// sendStubEvents sends simulated events when containd is not available.
func (s *Server) sendStubEvents(c *gin.Context) {
	// Send a few stub events for development/demo
	stubEvents := []containd.Event{
		{
			ID:        "stub-1",
			Timestamp: time.Now().Add(-10 * time.Second),
			Kind:      "firewall.rule.hit",
			Source:    "192.168.241.10",
			Dest:      "192.168.242.20",
			Protocol:  "tcp",
			Transport: "tcp",
			SrcPort:   45123,
			DstPort:   502,
			Attributes: map[string]any{
				"ruleId": "stub-hmi-to-plc",
				"action": "ALLOW",
				"via":    "stub",
				"proto":  "tcp",
				"port":   "502",
			},
		},
		{
			ID:        "stub-2",
			Timestamp: time.Now().Add(-5 * time.Second),
			Kind:      "request",
			Source:    "192.168.241.10",
			Dest:      "192.168.242.20",
			Protocol:  "modbus",
			Transport: "tcp",
			SrcPort:   45123,
			DstPort:   502,
			Attributes: map[string]any{
				"proto":          "modbus",
				"function_code":  3,
				"address":        0,
				"quantity":       10,
				"is_write":       false,
				"transaction_id": 1,
				"unit_id":        1,
			},
		},
		{
			ID:        "stub-2b",
			Timestamp: time.Now().Add(-3 * time.Second),
			Kind:      "request",
			Source:    "10.30.30.20",
			Dest:      "10.40.40.20",
			Protocol:  "dnp3",
			Transport: "tcp",
			SrcPort:   49201,
			DstPort:   20000,
			Attributes: map[string]any{
				"proto":         "dnp3",
				"function_code": 1,
				"unit_id":       1,
			},
		},
		{
			ID:        "stub-3",
			Timestamp: time.Now(),
			Kind:      "anomaly",
			Source:    "192.168.240.2",
			Dest:      "-",
			Attributes: map[string]any{
				"anomaly_type": "firewall.unavailable",
				"message":      "containd firewall not available - showing stub events",
				"severity":     "warning",
			},
		},
	}

	for _, e := range stubEvents {
		eventData, _ := json.Marshal(e)
		c.SSEvent("event", string(eventData))
	}
	c.Writer.Flush()
}

// handleGetFirewallHealth returns the containd firewall health status.
func (s *Server) handleGetFirewallHealth(c *gin.Context) {
	health, err := s.containdClient.GetHealth(c.Request.Context())
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

	flows, err := s.containdClient.GetFlows(c.Request.Context(), limit)
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
	summaries, err := s.containdClient.GetZoneRuleSummaries(c.Request.Context())
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
