package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/config"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

// Server wraps the Gin router and dependencies.
type Server struct {
	engine       *gin.Engine
	cfg          *config.Config
	db           *gorm.DB
	loader       *labs.Loader
	orchestrator *orchestrator.Orchestrator
}

type graphNodeData struct {
	Label         string            `json:"label"`
	Zone          string            `json:"zone"`
	Networks      []string          `json:"networks"`
	Status        string            `json:"status,omitempty"`
	IP            string            `json:"ip,omitempty"`
	InterfaceIPs  map[string]string `json:"interface_ips,omitempty"`  // network -> IP for multi-homed nodes
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
	InterfaceIPs  map[string]string `json:"interface_ips,omitempty"`  // network -> IP for multi-homed nodes
	UIPath        string            `json:"ui_path,omitempty"`         // override UI path (e.g., for proxied access)
	ExternalUIURL string            `json:"external_ui_url,omitempty"` // direct URL for external UI access
}

type nodeUIProxy struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Path string `json:"path,omitempty"`
}

// New constructs a server with routes registered.
func New(cfg *config.Config, db *gorm.DB, loader *labs.Loader, orchestrator *orchestrator.Orchestrator) *Server {
	engine := gin.Default()
	s := &Server{
		engine:       engine,
		cfg:          cfg,
		db:           db,
		loader:       loader,
		orchestrator: orchestrator,
	}

	s.applyMigrations()
	s.registerMiddleware()
	s.registerRoutes()
	return s
}

// Run starts the HTTP server.
func (s *Server) Run(_ context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.HTTPPort)
	return s.engine.Run(addr)
}

func (s *Server) applyMigrations() {
	_ = s.db.AutoMigrate(
		&models.LabTemplate{},
		&models.LabInstance{},
		&models.NodeDefinition{},
		&models.Scenario{},
		&models.ScenarioRun{},
		&models.TelemetryPoint{},
	)
}

func (s *Server) registerRoutes() {
	api := s.engine.Group("/api")
	{
		api.GET("/health", s.handleHealth)

		admin := api.Group("/admin")
		admin.POST("/seed", s.handleSeedDefinitions)

		labsGroup := api.Group("/labs")
		{
			labsGroup.GET("/templates", s.handleListLabTemplates)
			labsGroup.POST("/templates", s.handleCreateLabTemplate)

			labsGroup.POST("/instances", s.handleCreateLabInstance)
			labsGroup.GET("/instances", s.handleListLabInstances)
			labsGroup.GET("/instances/:id", s.handleGetLabInstance)
			labsGroup.POST("/instances/:id/start", s.handleStartLabInstance)
			labsGroup.POST("/instances/:id/stop", s.handleStopLabInstance)
			labsGroup.DELETE("/instances/:id", s.handleDeleteLabInstance)
			labsGroup.GET("/instances/:id/topology", s.handleGetTopology)
			labsGroup.GET("/instances/:id/graph", s.handleGetInstanceGraph)
			labsGroup.PATCH("/instances/:id/topology", s.handlePatchTopology)
			labsGroup.GET("/instances/:id/metrics", s.handleGetMetrics)
			labsGroup.GET("/instances/:id/events", s.handleGetEvents)
			labsGroup.Any("/instances/:id/nodes/:nodeId/ui/*path", s.handleProxyNodeUI)
			labsGroup.GET("/instances/:id/nodes/:nodeId/terminal", s.handleTerminal)
			labsGroup.GET("/instances/:id/live-events", s.handleGetLiveEvents)
		}

		// Firewall endpoints
		api.GET("/firewall/health", s.handleGetFirewallHealth)
		api.GET("/firewall/sessions", s.handleGetFirewallSessions)
		api.GET("/firewall/rules", s.handleGetFirewallRules)

		api.POST("/nodes/:node_id/action", s.handleNodeAction)

		api.GET("/scenarios", s.handleListScenarios)
		api.POST("/scenarios", s.handleCreateScenario)
		api.GET("/scenarios/:id", s.handleGetScenario)
		api.POST("/scenarios/:id/run", s.handleStartScenarioRun)
		api.GET("/scenario-runs/:id", s.handleGetScenarioRun)

		// Containd proxy - enables same-origin access to containd UI
		api.Any("/containd/*path", s.handleProxyContaind)
	}
}

func (s *Server) registerMiddleware() {
	s.engine.Use(s.corsMiddleware())
}

func (s *Server) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowedOrigin := ""

		if origin != "" && s.isOriginAllowed(origin) {
			allowedOrigin = origin
		} else if s.isOriginAllowed("*") {
			allowedOrigin = "*"
		}

		if allowedOrigin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		}
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func (s *Server) isOriginAllowed(origin string) bool {
	if len(s.cfg.AllowedOrigins) == 0 {
		return true
	}
	for _, allowed := range s.cfg.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) handleSeedDefinitions(c *gin.Context) {
	if err := s.loader.SeedFromDisk(c.Request.Context(), s.db); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "seeded"})
}

func (s *Server) handleListLabTemplates(c *gin.Context) {
	var templates []models.LabTemplate
	if err := s.db.Order("created_at desc").Find(&templates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

func (s *Server) handleCreateLabTemplate(c *gin.Context) {
	var payload models.LabTemplate
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if payload.ID == "" {
		payload.ID = uuid.NewString()
	}
	if err := s.db.WithContext(c).Save(&payload).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, payload)
}

func (s *Server) handleCreateLabInstance(c *gin.Context) {
	var payload struct {
		TemplateID string `json:"template_id"`
		Name       string `json:"name"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var template models.LabTemplate
	if err := s.db.First(&template, "id = ?", payload.TemplateID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}

	instance := models.LabInstance{
		ID:         uuid.NewString(),
		TemplateID: template.ID,
		Name:       payload.Name,
		Status:     "creating",
	}
	if err := s.db.WithContext(c).Create(&instance).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	go func(inst models.LabInstance) {
		_ = s.orchestrator.ProvisionLabInstance(context.Background(), s.db, &inst)
	}(instance)

	c.JSON(http.StatusAccepted, instance)
}

func (s *Server) handleListLabInstances(c *gin.Context) {
	var instances []models.LabInstance
	if err := s.db.Preload("Template").Find(&instances).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"instances": instances})
}

func (s *Server) handleGetLabInstance(c *gin.Context) {
	id := c.Param("id")
	var instance models.LabInstance
	if err := s.db.Preload("Template").Preload("Nodes").First(&instance, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}
	c.JSON(http.StatusOK, instance)
}

func (s *Server) handleStartLabInstance(c *gin.Context) {
	id := c.Param("id")

	// Start containers via orchestrator
	if err := s.orchestrator.StartLabContainers(c.Request.Context(), s.db, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update instance status
	if err := s.db.Model(&models.LabInstance{}).Where("id = ?", id).Update("status", "running").Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "running"})
}

func (s *Server) handleStopLabInstance(c *gin.Context) {
	id := c.Param("id")

	// Stop containers via orchestrator
	if err := s.orchestrator.StopLabContainers(c.Request.Context(), s.db, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update instance status
	if err := s.db.Model(&models.LabInstance{}).Where("id = ?", id).Update("status", "stopped").Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

func (s *Server) handleDeleteLabInstance(c *gin.Context) {
	id := c.Param("id")

	// Remove containers via orchestrator
	if err := s.orchestrator.RemoveLabContainers(c.Request.Context(), s.db, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Delete from database
	if err := s.db.Delete(&models.LabInstance{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) updateInstanceStatus(c *gin.Context, status string) {
	id := c.Param("id")
	if err := s.db.Model(&models.LabInstance{}).Where("id = ?", id).Update("status", status).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": status})
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

	zoneOrder := []string{"it_net", "dmz_net", "ot_control_net", "ot_safety_net"}
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

func (s *Server) handleListScenarios(c *gin.Context) {
	var scenarios []models.Scenario
	query := s.db
	if templateID := c.Query("lab_template_id"); templateID != "" {
		query = query.Where("lab_template_id = ?", templateID)
	}
	if err := query.Find(&scenarios).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scenarios": scenarios})
}

func (s *Server) handleCreateScenario(c *gin.Context) {
	var payload models.Scenario
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if payload.ID == "" {
		payload.ID = uuid.NewString()
	}
	if err := s.db.WithContext(c).Save(&payload).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, payload)
}

func (s *Server) handleGetScenario(c *gin.Context) {
	id := c.Param("id")
	var scenario models.Scenario
	if err := s.db.First(&scenario, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}
	c.JSON(http.StatusOK, scenario)
}

func (s *Server) handleStartScenarioRun(c *gin.Context) {
	id := c.Param("id")
	var payload struct {
		LabInstanceID string `json:"lab_instance_id"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.LabInstanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lab_instance_id required"})
		return
	}

	var scenario models.Scenario
	if err := s.db.First(&scenario, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}

	run := models.ScenarioRun{
		ID:            uuid.NewString(),
		ScenarioID:    scenario.ID,
		LabInstanceID: payload.LabInstanceID,
		Status:        "in_progress",
	}
	if err := s.db.WithContext(c).Create(&run).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, run)
}

func (s *Server) handleGetScenarioRun(c *gin.Context) {
	id := c.Param("id")
	var run models.ScenarioRun
	if err := s.db.First(&run, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario run not found"})
		return
	}
	c.JSON(http.StatusOK, run)
}

func (s *Server) handleGetMetrics(c *gin.Context) {
	instanceID := c.Param("id")
	var metrics []models.TelemetryPoint
	if err := s.db.Where("lab_instance_id = ?", instanceID).Find(&metrics).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"metrics": metrics})
}

func (s *Server) handleGetEvents(c *gin.Context) {
	instanceID := c.Param("id")
	var runs []models.ScenarioRun
	if err := s.db.Where("lab_instance_id = ?", instanceID).Find(&runs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": runs})
}

func (s *Server) handleProxyNodeUI(c *gin.Context) {
	labID := c.Param("id")
	nodeID := c.Param("nodeId")

	// Get lab instance with template to access topology
	var instance models.LabInstance
	if err := s.db.Preload("Template").First(&instance, "id = ?", labID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "lab not found"})
		return
	}

	// Parse topology to find node config
	var topo struct {
		Nodes []labs.NodeYAML `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(instance.Template.Topology), &topo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid topology"})
		return
	}

	// Find the node in topology
	var nodeConfig *labs.NodeYAML
	for i := range topo.Nodes {
		if topo.Nodes[i].ID == nodeID {
			nodeConfig = &topo.Nodes[i]
			break
		}
	}
	if nodeConfig == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found in topology"})
		return
	}

	// Get UI host and port based on node type and container
	host, port := getNodeUIHostPort(nodeConfig.Type, nodeConfig.Container)
	if host == "" || port == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "ui proxy not configured for this node type"})
		return
	}

	target, err := url.Parse(fmt.Sprintf("http://%s:%d", host, port))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid upstream host"})
		return
	}

	path := c.Param("path")
	if path == "" {
		path = "/"
	}

	// Base path for rewriting HTML base href
	basePath := fmt.Sprintf("/api/labs/instances/%s/nodes/%s/ui/", labID, nodeID)

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, path)
		// Preserve query string for socket.io polling
		req.URL.RawQuery = c.Request.URL.RawQuery
	}

	// Handle WebSocket upgrades for socket.io
	if c.GetHeader("Upgrade") == "websocket" {
		proxy.ServeHTTP(c.Writer, c.Request)
		return
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		// Only modify HTML responses
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()

		// Rewrite <base href="/"> to use our proxy path
		modified := strings.Replace(string(body), `<base href="/">`, `<base href="`+basePath+`">`, 1)

		resp.Body = io.NopCloser(strings.NewReader(modified))
		resp.ContentLength = int64(len(modified))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(modified)))
		return nil
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		rw.WriteHeader(http.StatusBadGateway)
		_, _ = rw.Write([]byte("proxy error: " + err.Error()))
	}

	proxy.ServeHTTP(c.Writer, c.Request)
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
		return "/apps/firewall/", "http://localhost:9080"
	case "hmi_view":
		return "/apps/hmi-view/", ""
	case "hmi_control":
		return "/apps/hmi-control/", ""
	case "hmi_scada":
		return "/apps/fuxa/", ""
	case "plc_trainer", "sis_plc":
		// OpenPLC has a web UI on port 8080
		if container != "" {
			return fmt.Sprintf("/api/labs/instances/%s/nodes/%s/ui/", labID, nodeID), ""
		}
		return "", ""
	case "ews", "ubuntu_jumpbox":
		// Webtop desktop environments
		if container != "" {
			return fmt.Sprintf("/api/labs/instances/%s/nodes/%s/ui/", labID, nodeID), ""
		}
		return "", ""
	case "kali_pentest":
		// Kali doesn't have a web UI by default, but has terminal
		return "", ""
	case "historian":
		// InfluxDB has a web UI on port 8086
		return fmt.Sprintf("/api/labs/instances/%s/nodes/%s/ui/", labID, nodeID), ""
	case "ot_ids":
		// Suricata doesn't have a web UI
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

// getNodeUIHostPort returns the host and port for a node's web UI based on type.
func getNodeUIHostPort(nodeType, container string) (string, int) {
	// Use container name as host if available, otherwise derive from type
	host := container
	if host == "" {
		// Default container naming convention
		switch nodeType {
		case "plc_trainer":
			host = "plc_process"
		case "sis_plc":
			host = "plc_safety"
		case "hmi_view":
			host = "hmi_view"
		case "hmi_control":
			host = "hmi_control"
		case "ews":
			host = "ews"
		case "ubuntu_jumpbox":
			host = "ubuntu_jumpbox"
		default:
			return "", 0
		}
	}

	// Determine port based on node type
	switch nodeType {
	case "plc_trainer", "sis_plc":
		return host, 8080 // OpenPLC web UI
	case "hmi_view", "hmi_control", "hmi_scada":
		return host, 1881 // FUXA
	case "ews", "ubuntu_jumpbox":
		return host, 3000 // Webtop/noVNC
	case "historian":
		return host, 8086 // InfluxDB
	default:
		return "", 0
	}
}

// Mirrors httputil.singleJoiningSlash without importing unexported logic.
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

// handleProxyContaind proxies requests to the containd firewall UI.
// This enables same-origin access, allowing the containd UI to be embedded
// in iframes with full authentication/cookie support.
func (s *Server) handleProxyContaind(c *gin.Context) {
	containdURL := s.cfg.ContaindAPIURL
	if containdURL == "" {
		containdURL = "http://firewall:8080"
	}

	target, err := url.Parse(containdURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid containd url"})
		return
	}

	path := c.Param("path")
	if path == "" {
		path = "/"
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, path)
		req.URL.RawQuery = c.Request.URL.RawQuery
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		rw.WriteHeader(http.StatusBadGateway)
		_, _ = rw.Write([]byte("containd proxy error: " + err.Error()))
	}

	// Rewrite HTML responses to fix asset paths
	proxy.ModifyResponse = func(resp *http.Response) error {
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()

		// Rewrite asset and link paths to go through the proxy
		// Use a placeholder to avoid double-replacement
		modified := string(body)
		modified = strings.ReplaceAll(modified, `"/_next/`, `"__CONTAIND_PROXY__/_next/`)
		modified = strings.ReplaceAll(modified, `"/api/`, `"__CONTAIND_PROXY__/api/`)
		modified = strings.ReplaceAll(modified, `href="/`, `href="__CONTAIND_PROXY__/`)
		modified = strings.ReplaceAll(modified, `__CONTAIND_PROXY__`, `/api/containd`)

		resp.Body = io.NopCloser(strings.NewReader(modified))
		resp.ContentLength = int64(len(modified))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(modified)))
		return nil
	}

	proxy.ServeHTTP(c.Writer, c.Request)
}
