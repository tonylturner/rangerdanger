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

type graphNodeData struct {
	Label         string            `json:"label"`
	Zone          string            `json:"zone"`
	Networks      []string          `json:"networks"`
	Status        string            `json:"status,omitempty"`
	IP            string            `json:"ip,omitempty"`
	InterfaceIPs  map[string]string `json:"interface_ips,omitempty"` // network -> IP for multi-homed nodes
	UIPath        string            `json:"ui_path,omitempty"`
	ExternalUIURL string            `json:"external_ui_url,omitempty"` // direct URL for external UI access
}

type graphNode struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Position gin.H         `json:"position"`
	Data     graphNodeData `json:"data"`
}

type graphEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

type nodeMetadata struct {
	Networks      []string          `json:"networks,omitempty"`
	UI            *nodeUIProxy      `json:"ui,omitempty"`
	InterfaceIPs  map[string]string `json:"interface_ips,omitempty"`   // network -> IP for multi-homed nodes
	UIPath        string            `json:"ui_path,omitempty"`         // override UI path (e.g., for proxied access)
	ExternalUIURL string            `json:"external_ui_url,omitempty"` // direct URL for external UI access
}

type nodeUIProxy struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Path string `json:"path,omitempty"`
}

func (s *Server) handleGetTopology(c *gin.Context) {
	id := c.Param("id")
	var instance models.LabInstance
	if err := s.db.Preload("Template").First(&instance, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}
	var topology any
	if err := json.Unmarshal([]byte(instance.Template.Topology), &topology); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid topology"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"topology": topology})
}

func (s *Server) handleGetInstanceGraph(c *gin.Context) {
	id := c.Param("id")
	var instance models.LabInstance
	if err := s.db.Preload("Template").Preload("Nodes").First(&instance, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}

	var topo struct {
		Networks []labs.NetworkYAML `json:"networks"`
		Nodes    []labs.NodeYAML    `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(instance.Template.Topology), &topo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid topology"})
		return
	}

	nodeLookup := map[string]models.NodeDefinition{}
	for _, n := range instance.Nodes {
		nodeLookup[n.ID] = n
	}

	zoneOrder := []string{
		"enterprise_net", "vendor_net", "ot_ops_net", "field_net",
		"it_net", "dmz_net", "ot_control_net", "ot_safety_net",
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
		def := nodeLookup[n.ID]
		meta := decodeNodeMetadata(def.Metadata, n.Networks)

		// Prefer networks from YAML topology (source of truth)
		zone := ""
		if len(n.Networks) > 0 {
			zone = n.Networks[0]
		} else if len(meta.Networks) > 0 {
			zone = meta.Networks[0]
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

		status := def.Status
		if status == "" {
			status = "running" // Default to running for active containers
		}

		// Prefer IP from YAML topology (source of truth), fall back to database
		ip := n.IP
		if ip == "" {
			ip = def.IP
		}

		// Generate UI path and external URL based on node type
		uiPath, externalURL := getNodeUIConfig(n.Type, n.Container, id, n.ID)
		if meta.UIPath != "" {
			uiPath = meta.UIPath
		}
		if meta.ExternalUIURL != "" {
			externalURL = meta.ExternalUIURL
		}

		// Build interface IPs for multi-homed nodes
		interfaceIPs := meta.InterfaceIPs
		if interfaceIPs == nil && len(n.Networks) > 1 && n.IP != "" {
			interfaceIPs = buildInterfaceIPs(n.Networks, n.IP)
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
				Status:        status,
				IP:            ip,
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

func (s *Server) handlePatchTopology(c *gin.Context) {
	id := c.Param("id")
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.db.Model(&models.LabInstance{}).Where("id = ?", id).Update("runtime_config", string(serialized)).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runtime_config": payload})
}

func (s *Server) handleNodeAction(c *gin.Context) {
	c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
}
func decodeNodeMetadata(raw string, fallbackNetworks []string) nodeMetadata {
	if raw == "" {
		return nodeMetadata{Networks: fallbackNetworks}
	}
	var meta nodeMetadata
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nodeMetadata{Networks: fallbackNetworks}
	}
	if len(meta.Networks) == 0 {
		meta.Networks = fallbackNetworks
	}
	return meta
}

// getNodeUIConfig returns UI path and external URL based on node type.
func getNodeUIConfig(nodeType, container, labID, nodeID string) (uiPath, externalURL string) {
	// Map node types to their UI configurations
	switch nodeType {
	case "containd_ngfw":
		// Same-origin paths only — frontend can prepend window.location
		// if it ever needs an absolute URL. Hardcoding localhost:9080
		// here breaks any deployment that isn't bound to loopback.
		return "/apps/firewall/", "/containd/"
	case "hmi_view":
		return "/apps/hmi-view/", ""
	case "hmi_control":
		return "/apps/hmi-control/", ""
	case "hmi_scada":
		return "/apps/fuxa/", ""
	case "fuxa_hmi":
		return "/apps/fuxa-hmi/", ""
	case "plc_trainer", "sis_plc", "openplc":
		if container != "" {
			return fmt.Sprintf("/api/labs/instances/%s/nodes/%s/ui/", labID, nodeID), ""
		}
		return "/apps/openplc/", ""
	case "ews", "ubuntu_jumpbox":
		if container != "" {
			return fmt.Sprintf("/api/labs/instances/%s/nodes/%s/ui/", labID, nodeID), ""
		}
		return "", ""
	case "corp_workstation":
		return "/apps/corp-ws/", ""
	case "vendor_jumpbox":
		return "/apps/vendor-jump/", ""
	case "eng_workstation":
		return "/apps/eng-ws/", ""
	case "kali_pentest":
		return "", ""
	case "historian":
		return fmt.Sprintf("/api/labs/instances/%s/nodes/%s/ui/", labID, nodeID), ""
	case "ot_ids":
		return "", ""
	case "rtac_sim", "relay_sim", "recloser_sim", "regulator_sim", "opendss_sim":
		// Simulators have API but no web UI for direct access
		return "", ""
	default:
		return "", ""
	}
}

// buildInterfaceIPs creates interface IP mapping for multi-homed nodes.
// This is a simple implementation - in production you'd query Docker for actual IPs.
func buildInterfaceIPs(networks []string, primaryIP string) map[string]string {
	if len(networks) <= 1 {
		return nil
	}
	// For now, we just return the primary IP for the first network
	// A full implementation would query container inspect for all IPs
	ips := make(map[string]string)
	ips[networks[0]] = primaryIP
	return ips
}
