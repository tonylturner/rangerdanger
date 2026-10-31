package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleSeedDefinitions reloads every package. It fails with the same
// error startup would, and keeps the previous catalog when it does.
func (s *Server) handleSeedDefinitions(c *gin.Context) {
	catalog, err := s.loader.Seed(c.Request.Context(), s.db, s.rng.ActivePackage())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s.setCatalog(catalog)
	c.JSON(http.StatusOK, gin.H{"status": "seeded"})
}
