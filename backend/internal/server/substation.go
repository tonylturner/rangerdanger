package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// Substation data proxy: forwards requests to the range's RTAC API, which
// the manifest names by role. This gives the frontend access to live
// substation state without direct OT network access.

// rtacTimeout bounds one RTAC API request.
const rtacTimeout = 5 * time.Second

// rtacClient is shared: requests carry their own context and deadline.
var rtacClient = &http.Client{}

// rtacRequest sends one request to the RTAC API, bounded by rtacTimeout and
// by ctx.
func rtacRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, rtacTimeout)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		cancel()
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := rtacClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// cancelOnClose releases a request's context when its body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b cancelOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
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

// handleSubstationLabControl forwards a Load Simulator load override (and
// optional audit entry) to the RTAC. Training-only; off by default.
func (s *Server) handleSubstationLabControl(c *gin.Context) {
	s.proxyRTACPost(c, "/api/lab-control")
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
	s.forwardRTAC(c, http.MethodGet, path, nil)
}

func (s *Server) proxyRTACPost(c *gin.Context, path string) {
	s.forwardRTAC(c, http.MethodPost, path, c.Request.Body)
}

func (s *Server) forwardRTAC(c *gin.Context, method, path string, body io.Reader) {
	rtac, err := rtacURL(rangeOf(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	resp, err := rtacRequest(c.Request.Context(), method, rtac+path, body)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "rtac-sim not reachable",
			"details": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", respBody)
}

// handleSubstationNetworkEvents returns containd DPI events filtered to substation-relevant traffic.
func (s *Server) handleSubstationNetworkEvents(c *gin.Context) {
	events, err := rangeOf(c).Containd().GetEvents(c.Request.Context(), "", 50)
	if err != nil {
		// Return empty with source info rather than error
		c.JSON(http.StatusOK, gin.H{
			"events":  []any{},
			"source":  "unavailable",
			"message": "containd not reachable: " + err.Error(),
		})
		return
	}

	// Keep traffic that touches a teaching zone (enterprise, vendor, OT ops
	// and field for the US package): the manifest's zone networks.
	zones := zoneSubnets(rangeOf(c))
	var filtered []containd.Event
	for _, e := range events {
		if inAnySubnet(zones, e.Source) || inAnySubnet(zones, e.Dest) {
			filtered = append(filtered, e)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"events": filtered,
		"source": "containd",
	})
}

// zoneSubnets is the subnets of the manifest's teaching zone networks.
func zoneSubnets(gen *lifecycle.Generation) []*net.IPNet {
	var subnets []*net.IPNet
	for _, network := range gen.Manifest.Networks {
		if network.Zone == "" {
			continue
		}
		if _, subnet, err := net.ParseCIDR(network.Subnet); err == nil {
			subnets = append(subnets, subnet)
		}
	}
	return subnets
}

func inAnySubnet(subnets []*net.IPNet, address string) bool {
	ip := net.ParseIP(address)
	if ip == nil {
		return false
	}
	for _, subnet := range subnets {
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}
