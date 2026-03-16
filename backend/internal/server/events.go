package server

import (
	"encoding/json"
	"net/http"
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

	// Get containd client URL from environment
	containdURL := s.cfg.ContaindAPIURL
	if containdURL == "" {
		containdURL = "http://firewall:8080"
	}

	client := containd.NewClient(containdURL)

	// Track last event ID for polling
	lastEventID := ""

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Check if containd is available
	if !client.IsAvailable() {
		// Send fallback stub events
		s.sendStubEvents(c)
		return
	}

	for {
		select {
		case <-ticker.C:
			// Poll containd for events
			events, err := client.GetEvents(lastEventID, 10)
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
				lastEventID = e.ID
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
			Type:      "connection",
			Source:    "192.168.241.10",
			Dest:      "192.168.242.20",
			Protocol:  "tcp",
			SrcPort:   45123,
			DstPort:   502,
			Details:   "HMI connecting to PLC via Modbus",
			Severity:  "info",
			Zone:      "dmz",
		},
		{
			ID:        "stub-2",
			Timestamp: time.Now().Add(-5 * time.Second),
			Type:      "modbus",
			Source:    "192.168.241.10",
			Dest:      "192.168.242.20",
			Protocol:  "modbus",
			SrcPort:   45123,
			DstPort:   502,
			Details:   "Read Holding Registers (FC03) addr=0x0000 qty=10",
			Severity:  "info",
			Zone:      "dmz",
		},
		{
			ID:        "stub-3",
			Timestamp: time.Now(),
			Type:      "info",
			Source:    "192.168.240.2",
			Dest:      "-",
			Protocol:  "-",
			Details:   "containd firewall not available - showing stub events",
			Severity:  "warning",
			Zone:      "wan",
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
	containdURL := s.cfg.ContaindAPIURL
	if containdURL == "" {
		containdURL = "http://firewall:8080"
	}

	client := containd.NewClient(containdURL)
	health, err := client.GetHealth()
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

// handleGetFirewallSessions returns active sessions through the firewall.
func (s *Server) handleGetFirewallSessions(c *gin.Context) {
	containdURL := s.cfg.ContaindAPIURL
	if containdURL == "" {
		containdURL = "http://firewall:8080"
	}

	client := containd.NewClient(containdURL)
	sessions, err := client.GetSessions()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"sessions": []any{},
			"error":    err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

// handleGetFirewallRules returns summarized firewall rules grouped by zone pairs.
func (s *Server) handleGetFirewallRules(c *gin.Context) {
	containdURL := s.cfg.ContaindAPIURL
	if containdURL == "" {
		containdURL = "http://firewall:8080"
	}

	client := containd.NewClient(containdURL)
	summaries, err := client.GetZoneRuleSummaries()
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
