package server

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// Substation data proxy — forwards requests to rtac-sim running in the OT ops zone.
// This gives the frontend access to live substation state without direct OT network access.

const rtacSimDefault = "http://10.30.30.20:8080"

func (s *Server) rtacURL() string {
	return rtacSimDefault
}

// handleSubstationTags returns all flattened RTAC tags (device state + electrical + alarms).
func (s *Server) handleSubstationTags(c *gin.Context) {
	s.proxyRTAC(c, "/api/tags")
}

// handleSubstationState returns raw device state from all field devices + physics engine.
func (s *Server) handleSubstationState(c *gin.Context) {
	s.proxyRTAC(c, "/api/state")
}

// handleSubstationCommand forwards a command to a field device via RTAC.
func (s *Server) handleSubstationCommand(c *gin.Context) {
	device := c.Param("device")
	s.proxyRTACPost(c, "/api/command/"+device)
}

// handleSubstationAudit returns the RTAC command audit log.
func (s *Server) handleSubstationAudit(c *gin.Context) {
	s.proxyRTAC(c, "/api/audit")
}

// handleSubstationHealth returns RTAC health including device comms status.
func (s *Server) handleSubstationHealth(c *gin.Context) {
	s.proxyRTAC(c, "/api/health")
}

func (s *Server) proxyRTAC(c *gin.Context, path string) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(s.rtacURL() + path)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "rtac-sim not reachable",
			"details": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}

// handleSubstationNetworkEvents returns containd DPI events filtered to substation-relevant traffic.
func (s *Server) handleSubstationNetworkEvents(c *gin.Context) {
	containdURL := s.cfg.ContaindAPIURL
	if containdURL == "" {
		containdURL = "http://firewall:8080"
	}

	client := containd.NewClient(containdURL)
	events, err := client.GetEvents("", 50)
	if err != nil {
		// Return empty with source info rather than error
		c.JSON(http.StatusOK, gin.H{
			"events":  []any{},
			"source":  "unavailable",
			"message": "containd not reachable: " + err.Error(),
		})
		return
	}

	// Filter to OT-relevant subnets: 10.30.30.x (OT ops), 10.40.40.x (field), 10.10.10.x (enterprise attacking field)
	var filtered []containd.Event
	for _, e := range events {
		if isSubstationRelevant(e.Source, e.Dest) {
			filtered = append(filtered, e)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"events": filtered,
		"source": "containd",
	})
}

// isSubstationRelevant checks if traffic involves OT/field subnets.
func isSubstationRelevant(src, dest string) bool {
	relevantPrefixes := []string{"10.30.30.", "10.40.40.", "10.10.10.", "10.20.20."}
	for _, prefix := range relevantPrefixes {
		if strings.HasPrefix(src, prefix) || strings.HasPrefix(dest, prefix) {
			return true
		}
	}
	return false
}

func (s *Server) proxyRTACPost(c *gin.Context, path string) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(s.rtacURL()+path, "application/json", c.Request.Body)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "rtac-sim not reachable",
			"details": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", body)
}
