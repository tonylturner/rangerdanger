package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// handleGetWorkshopGraph returns the serving range's topology as a graph,
// with zones and addresses from its manifest.
func (s *Server) handleGetWorkshopGraph(c *gin.Context) {
	gen := rangeOf(c)
	topology := gen.Package.Template
	statuses := s.workshopNodeStatuses(c.Request.Context(), gen)

	zoneOrder := manifestZones(gen)
	zoneCounts := map[string]int{}

	nodes := []graphNode{}
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

	edges := []graphEdge{}

	for _, n := range topology.Nodes {
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

		uiPath, externalURL := getNodeUIConfig(n.Type)

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
				Status:        statuses[n.ID],
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

	flows := topology.Flows
	if flows == nil {
		flows = []labs.TopologyFlow{}
	}
	bindings := topology.Liveness
	if bindings == nil {
		bindings = map[string]labs.LivenessBinding{}
	}
	networks := topology.Networks
	if networks == nil {
		networks = []labs.NetworkYAML{}
	}
	recipe, _ := recipeFor(gen)
	c.JSON(http.StatusOK, gin.H{
		"nodes": nodes, "edges": edges, "flows": flows, "liveness": bindings, "networks": networks,
		"operations": gin.H{"traffic_generation": recipe != nil && len(recipe.traffic) > 0},
	})
}

// handleGetWorkshopStatus returns the status of the workshop environment:
// the firewall, the curriculum, and the package's supervisory service (the
// RTAC's health and device comms, or the control centre's link and GI).
func (s *Server) handleGetWorkshopStatus(c *gin.Context) {
	gen := rangeOf(c)
	ctx := c.Request.Context()

	// Check containd health
	_, fwErr := gen.Containd().GetHealth(ctx)
	fwOk := fwErr == nil

	// Count scenarios
	var scenarioCount int64
	s.activeScenarios().Count(&scenarioCount)

	// Get active firewall config
	s.activeConfigMu.RLock()
	activeConfig := s.activeConfig
	s.activeConfigMu.RUnlock()

	name := gen.Package.Template.Name
	if recipe, err := recipeFor(gen); err == nil && recipe.workshopName != "" {
		name = recipe.workshopName
	}
	status := gin.H{
		"workshop_id":     gen.Package.Template.ID,
		"workshop_name":   name,
		"firewall_online": fwOk,
		"firewall_config": activeConfig,
		"scenario_count":  scenarioCount,
	}
	if _, ok := gen.Manifest.ServiceByRole(manifest.RoleRTAC); ok {
		status["rtac_online"], status["device_comms"] = s.rtacStatus(ctx, gen)
	}
	if cc, ok := controlCentreURL(gen); ok {
		status["control_centre"] = controlCentreLinkStatus(ctx, cc)
	}
	c.JSON(http.StatusOK, status)
}

// rtacStatus is whether the RTAC answers, and its device comms.
func (s *Server) rtacStatus(ctx context.Context, gen *lifecycle.Generation) (bool, map[string]bool) {
	deviceComms := map[string]bool{}
	rtacState, err := s.fetchRTACState(ctx, gen)
	if err != nil || rtacState == nil {
		return false, deviceComms
	}
	if comms, ok := rtacState["device_comms"].(map[string]any); ok {
		for k, v := range comms {
			if b, ok := v.(bool); ok {
				deviceComms[k] = b
			}
		}
	}
	return true, deviceComms
}

// ccLinkStatus is the control centre's link and GI state in the workshop
// status; both are null while the control centre does not answer.
type ccLinkStatus struct {
	Link *string `json:"link"`
	GI   *string `json:"gi"`
}

func controlCentreLinkStatus(ctx context.Context, cc string) ccLinkStatus {
	body, err := fetchUpstreamObject(ctx, controlCentreService, cc+"/api/state")
	if err != nil {
		return ccLinkStatus{}
	}
	var state struct {
		Schema int `json:"schema"`
		Link   struct {
			State *string `json:"state"`
			GI    struct {
				State          *string `json:"state"`
				LastCompleteAt *string `json:"last_complete_at"`
			} `json:"gi"`
		} `json:"link"`
	}
	if err := json.Unmarshal(body, &state); err != nil || state.Schema != 3 {
		return ccLinkStatus{}
	}
	var integrity *string
	if state.Link.GI.State != nil {
		value := "none"
		if state.Link.GI.LastCompleteAt != nil {
			value = "complete"
		} else if *state.Link.GI.State == "running" {
			value = "running"
		}
		integrity = &value
	}
	return ccLinkStatus{Link: state.Link.State, GI: integrity}
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

// workshopNodeStatuses uses one bounded inventory read, including stopped containers.
// An unavailable daemon is unknown, not proof that a container is missing.
func (s *Server) workshopNodeStatuses(ctx context.Context, gen *lifecycle.Generation) map[string]string {
	states := map[string]string{}
	for _, node := range gen.Package.Template.Nodes {
		states[node.ID] = "unknown"
	}
	if s.orchestrator == nil || s.orchestrator.DockerClient() == nil {
		return states
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	containers, err := s.orchestrator.DockerClient().ContainerList(ctx, container.ListOptions{
		All: true, Filters: filters.NewArgs(filters.Arg("label", "com.docker.compose.project="+manifest.RangeProject)),
	})
	if err != nil {
		return states
	}
	byName := map[string]string{}
	for _, item := range containers {
		// Check the label as well as the query: never display a platform container.
		if item.Labels["com.docker.compose.project"] != manifest.RangeProject {
			continue
		}
		state := "unknown"
		switch item.State {
		case "running", "exited", "restarting", "paused":
			state = item.State
		}
		for _, name := range item.Names {
			byName[strings.TrimPrefix(name, "/")] = state
		}
	}
	for _, node := range gen.Package.Template.Nodes {
		service, ok := gen.Manifest.ServiceByNode(node.ID)
		if !ok {
			continue
		}
		state, found := byName[service.Container]
		if !found {
			state = "missing"
		}
		states[node.ID] = state
	}
	return states
}
