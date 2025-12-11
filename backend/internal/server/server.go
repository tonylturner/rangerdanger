package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tturner/rangerrocks/backend/internal/config"
	"github.com/tturner/rangerrocks/backend/internal/labs"
	"github.com/tturner/rangerrocks/backend/internal/models"
	"github.com/tturner/rangerrocks/backend/internal/orchestrator"
)

// Server wraps the Gin router and dependencies.
type Server struct {
	engine       *gin.Engine
	cfg          *config.Config
	db           *gorm.DB
	loader       *labs.Loader
	orchestrator *orchestrator.Orchestrator
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
			labsGroup.PATCH("/instances/:id/topology", s.handlePatchTopology)
			labsGroup.GET("/instances/:id/metrics", s.handleGetMetrics)
			labsGroup.GET("/instances/:id/events", s.handleGetEvents)
		}

		api.POST("/nodes/:node_id/action", s.handleNodeAction)

		api.GET("/scenarios", s.handleListScenarios)
		api.POST("/scenarios", s.handleCreateScenario)
		api.GET("/scenarios/:id", s.handleGetScenario)
		api.POST("/scenarios/:id/run", s.handleStartScenarioRun)
		api.GET("/scenario-runs/:id", s.handleGetScenarioRun)
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
	s.updateInstanceStatus(c, "running")
}

func (s *Server) handleStopLabInstance(c *gin.Context) {
	s.updateInstanceStatus(c, "stopped")
}

func (s *Server) handleDeleteLabInstance(c *gin.Context) {
	id := c.Param("id")
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
