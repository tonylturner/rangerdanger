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

// currentCatalog is the catalog of the last successful seed.
func (s *Server) currentCatalog() *labs.Catalog {
	s.catalogMu.RLock()
	defer s.catalogMu.RUnlock()
	return s.catalog
}

// activePackage is the single source of the package the workshop serves.
// The ID is the range lifecycle's: the recorded package, else
// RANGERDANGER_PACKAGE; its metadata comes from the last successful load.
// Without a loaded match it returns the zero value, whose empty IDs match
// no template or scenario row.
func (s *Server) activePackage() labs.PackageInfo {
	info, _ := s.currentCatalog().Info(s.rng.ActivePackage())
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
	infos := s.currentCatalog().Infos()
	active := s.rng.ActivePackage()

	packages := make([]packageSummary, len(infos))
	for index, info := range infos {
		packages[index] = packageSummary{
			ID:       info.ID,
			Title:    info.Title,
			Revision: info.Revision,
			Active:   info.ID == active,
		}
	}
	c.JSON(http.StatusOK, packages)
}
