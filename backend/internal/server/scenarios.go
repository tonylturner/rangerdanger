package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tturner/rangerdanger/backend/internal/models"
)

func (s *Server) handleListScenarios(c *gin.Context) {
	var scenarios []models.Scenario
	query := s.db
	if templateID := c.Query("lab_template_id"); templateID != "" {
		query = query.Where("lab_template_id = ?", templateID)
	}
	if err := query.Order("\"order\" ASC, name ASC").Find(&scenarios).Error; err != nil {
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
