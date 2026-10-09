package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

// packageSummary is one row of GET /api/packages.
type packageSummary struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Revision int    `json:"revision"`
	Active   bool   `json:"active"`
}

// setCatalog replaces the loaded package catalog after a successful seed.
func (s *Server) setCatalog(catalog *labs.Catalog) {
	s.catalogMu.Lock()
	s.catalog = catalog
	s.catalogMu.Unlock()
}

// activePackage is the single source of the package the workshop serves.
// The ID comes from configuration (RANGERDANGER_PACKAGE); its metadata comes
// from the last successful load. Without a loaded match it returns the zero
// value, whose empty IDs match no template or scenario row.
func (s *Server) activePackage() labs.PackageInfo {
	s.catalogMu.RLock()
	defer s.catalogMu.RUnlock()
	info, _ := s.catalog.Info(s.cfg.Package)
	return info
}

// activeScenarios scopes a scenario query to the active package.
func (s *Server) activeScenarios() *gorm.DB {
	return s.db.Model(&models.Scenario{}).Where("package_id = ?", s.activePackage().ID)
}

// findActiveScenario loads a scenario only if the active package owns it.
func (s *Server) findActiveScenario(id string) (models.Scenario, error) {
	var scenario models.Scenario
	err := s.activeScenarios().Where("id = ?", id).First(&scenario).Error
	return scenario, err
}

func (s *Server) handleListPackages(c *gin.Context) {
	s.catalogMu.RLock()
	infos := s.catalog.Infos()
	s.catalogMu.RUnlock()

	packages := make([]packageSummary, len(infos))
	for index, info := range infos {
		packages[index] = packageSummary{
			ID:       info.ID,
			Title:    info.Title,
			Revision: info.Revision,
			Active:   info.ID == s.cfg.Package,
		}
	}
	c.JSON(http.StatusOK, packages)
}
