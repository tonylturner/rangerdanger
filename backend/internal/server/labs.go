package server

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tturner/rangerdanger/backend/internal/models"
)

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
