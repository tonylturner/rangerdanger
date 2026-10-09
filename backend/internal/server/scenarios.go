package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/models"
)

func (s *Server) handleListScenarios(c *gin.Context) {
	var scenarios []models.Scenario
	query := s.activeScenarios()
	if templateID := c.Query("lab_template_id"); templateID != "" {
		query = query.Where("lab_template_id = ?", templateID)
	}
	if err := query.Order("\"order\" ASC, name ASC").Find(&scenarios).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scenarios": scenarios})
}

func (s *Server) handleGetScenario(c *gin.Context) {
	scenario, err := s.findActiveScenario(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}
	c.JSON(http.StatusOK, scenario)
}
