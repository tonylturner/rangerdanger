package server

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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
