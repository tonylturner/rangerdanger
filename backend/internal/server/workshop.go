package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

// handleGetWorkshopGraph returns the topology graph for the active workshop
// template, with zones and addresses from the range manifest. It does not
// require a lab instance.
func (s *Server) handleGetWorkshopGraph(c *gin.Context) {
	gen := rangeOf(c)
	var template models.LabTemplate
	if err := s.db.First(&template, "id = ?", s.activePackage().TemplateID).Error; err != nil {
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

	zoneOrder := manifestZones(gen)
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

		interfaceIPs := manifestInterfaceIPs(gen, n)

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
	rtacState, err := s.fetchRTACState(c.Request.Context(), rangeOf(c))
	if err == nil && rtacState != nil {
		rtacOk = true
	}

	// Check containd health
	_, fwErr := rangeOf(c).Containd().GetHealth(c.Request.Context())
	fwOk := fwErr == nil

	// Count scenarios
	var scenarioCount int64
	s.activeScenarios().Count(&scenarioCount)

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
		"workshop_id":     s.activePackage().TemplateID,
		"workshop_name":   "Distribution Substation Segmentation",
		"rtac_online":     rtacOk,
		"firewall_online": fwOk,
		"firewall_config": activeConfig,
		"scenario_count":  scenarioCount,
		"device_comms":    deviceComms,
	})
}

// manifestZones is the teaching zones' network keys, in manifest order.
func manifestZones(gen *lifecycle.Generation) []string {
	var zones []string
	for _, network := range gen.Manifest.Networks {
		if network.Zone != "" {
			zones = append(zones, network.Key)
		}
	}
	return zones
}

// manifestInterfaceIPs maps each of the node's topology networks to the
// node's address there, from the manifest. A node without a manifest
// service keeps its topology address on its first network.
func manifestInterfaceIPs(gen *lifecycle.Generation, n labs.NodeYAML) map[string]string {
	ips := map[string]string{}
	svc, ok := gen.Manifest.ServiceByNode(n.ID)
	if !ok {
		if n.IP != "" && len(n.Networks) > 0 {
			ips[n.Networks[0]] = n.IP
		}
		return ips
	}
	for _, network := range n.Networks {
		if ip, err := interfaceIP(svc, network); err == nil {
			ips[network] = ip
		}
	}
	return ips
}
