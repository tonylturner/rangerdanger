package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

const workshopTemplateID = "substation-segmentation"

// handleGetWorkshopGraph returns the topology graph for the active workshop template.
// This does not require a lab instance — it reads directly from the template.
func (s *Server) handleGetWorkshopGraph(c *gin.Context) {
	var template models.LabTemplate
	if err := s.db.First(&template, "id = ?", workshopTemplateID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workshop template not found — run seed first"})
		return
	}

	var topo struct {
		Networks []labs.NetworkYAML `json:"networks"`
		Nodes    []labs.NodeYAML    `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(template.Topology), &topo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid topology"})
		return
	}

	zoneOrder := []string{
		"enterprise_net", "vendor_net", "ot_ops_net", "field_net", "physics_net",
	}
	zoneCounts := map[string]int{}

	var nodes []graphNode
	for idx, zone := range zoneOrder {
		nodes = append(nodes, graphNode{
			ID:   "zone-" + zone,
			Type: "zone",
			Position: gin.H{
				"x": float64(idx * 260),
				"y": 0,
			},
			Data: graphNodeData{
				Label:    strings.ToUpper(zone),
				Zone:     zone,
				Networks: []string{zone},
				Status:   "zone",
			},
		})
	}

	var edges []graphEdge

	for _, n := range topo.Nodes {
		zone := ""
		if len(n.Networks) > 0 {
			zone = n.Networks[0]
		}
		column := 0
		for idx, z := range zoneOrder {
			if z == zone {
				column = idx
				break
			}
		}
		count := zoneCounts[zone]
		zoneCounts[zone] = count + 1

		uiPath, externalURL := getNodeUIConfig(n.Type, n.Container, "workshop", n.ID)

		interfaceIPs := map[string]string{}
		if n.IP != "" && len(n.Networks) > 0 {
			interfaceIPs[n.Networks[0]] = n.IP
		}
		// For multi-homed nodes, build IPs from the known addresses
		if len(n.Networks) > 1 {
			interfaceIPs = buildWorkshopInterfaceIPs(n)
		}

		nodes = append(nodes, graphNode{
			ID:   n.ID,
			Type: n.Type,
			Position: gin.H{
				"x": float64(column * 260),
				"y": float64(160 + count*170),
			},
			Data: graphNodeData{
				Label:         n.Name,
				Zone:          zone,
				Networks:      n.Networks,
				Status:        "running",
				IP:            n.IP,
				InterfaceIPs:  interfaceIPs,
				UIPath:        uiPath,
				ExternalUIURL: externalURL,
			},
		})

		if zone != "" {
			edges = append(edges, graphEdge{
				ID:     fmt.Sprintf("edge-%s-%s", zone, n.ID),
				Source: "zone-" + zone,
				Target: n.ID,
				Label:  zone,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"nodes": nodes, "edges": edges})
}

// handleGetWorkshopStatus returns the status of the workshop environment.
func (s *Server) handleGetWorkshopStatus(c *gin.Context) {
	// Check RTAC health
	rtacOk := false
	rtacState, err := s.fetchRTACState()
	if err == nil && rtacState != nil {
		rtacOk = true
	}

	// Check containd health
	fwOk := false
	if s.containdClient != nil {
		_, err := s.containdClient.GetHealth()
		fwOk = err == nil
	}

	// Count scenarios
	var scenarioCount int64
	s.db.Model(&models.Scenario{}).Where("lab_template_id = ?", workshopTemplateID).Count(&scenarioCount)

	// Get active firewall config
	s.activeConfigMu.RLock()
	activeConfig := s.activeConfig
	s.activeConfigMu.RUnlock()

	// Get device comms from RTAC state
	deviceComms := map[string]bool{}
	if rtacState != nil {
		if comms, ok := rtacState["device_comms"].(map[string]any); ok {
			for k, v := range comms {
				if b, ok := v.(bool); ok {
					deviceComms[k] = b
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"workshop_id":     workshopTemplateID,
		"workshop_name":   "Distribution Substation Segmentation",
		"rtac_online":     rtacOk,
		"firewall_online": fwOk,
		"firewall_config": activeConfig,
		"scenario_count":  scenarioCount,
		"device_comms":    deviceComms,
	})
}

// buildWorkshopInterfaceIPs creates interface IP mapping for multi-homed workshop nodes.
// Uses known IP assignments from the substation topology.
func buildWorkshopInterfaceIPs(n labs.NodeYAML) map[string]string {
	// Known multi-homed node IPs from docker-compose
	knownIPs := map[string]map[string]string{
		"rtac": {
			"ot_ops_net":  "10.30.30.20",
			"field_net":   "10.40.40.10",
			"physics_net": "10.50.50.10",
		},
		"openplc": {
			"ot_ops_net": "10.30.30.30",
			"field_net":  "10.40.40.30",
		},
	}

	if ips, ok := knownIPs[n.ID]; ok {
		return ips
	}

	// Fallback: use primary IP for first network
	result := map[string]string{}
	if n.IP != "" && len(n.Networks) > 0 {
		result[n.Networks[0]] = n.IP
	}
	return result
}
