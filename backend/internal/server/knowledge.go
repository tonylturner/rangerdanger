package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleKnowledge serves only the active package's validated catalog snapshot.
// It deliberately does not require a healthy range or read files per request.
func (s *Server) handleKnowledge(c *gin.Context) {
	catalog := s.currentCatalog()
	active := s.rng.ActivePackage()
	if catalog != nil && active != "" {
		for _, pkg := range catalog.Packages {
			if pkg.ID == active {
				c.JSON(http.StatusOK, gin.H{"package_id": pkg.ID, "revision": pkg.Revision, "sections": pkg.Knowledge})
				return
			}
		}
	}
	c.JSON(http.StatusConflict, gin.H{"error": "no active knowledge package"})
}
