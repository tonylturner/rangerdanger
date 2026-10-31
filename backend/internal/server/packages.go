package server

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

// packageSummary is one row of GET /api/packages.
type packageSummary struct {
	Curriculum *labs.Curriculum `json:"curriculum,omitempty"`
	ID         string           `json:"id"`
	Title      string           `json:"title"`
	Revision   int              `json:"revision"`
	Active     bool             `json:"active"`
	// OperatorView selects the frontend operator page for the package.
	OperatorView string `json:"operator_view"`
	// Policies are the names package.yml declares under `policies`, the
	// ones POST /api/firewall/apply accepts: the topology's default
	// policy first, then the others by name.
	Policies []string `json:"policies"`
}

// policyNames orders the package's policy names default first, then by
// name, so the firewall menus list the starting policy first.
func policyNames(pkg labs.Package) []string {
	defaultName, _ := pkg.PolicyName(pkg.FirewallConfigPath)
	names := make([]string, 0, len(pkg.Policies))
	for name := range pkg.Policies {
		if name != defaultName {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if _, ok := pkg.Policies[defaultName]; ok {
		names = append([]string{defaultName}, names...)
	}
	return names
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
	packages := []packageSummary{}
	catalog := s.currentCatalog()
	if catalog == nil {
		c.JSON(http.StatusOK, packages)
		return
	}
	active := s.rng.ActivePackage()
	for _, pkg := range catalog.Packages {
		packages = append(packages, packageSummary{
			ID:           pkg.ID,
			Title:        pkg.Title,
			Revision:     pkg.Revision,
			Active:       pkg.ID == active,
			OperatorView: pkg.OperatorView,
			Policies:     policyNames(pkg),
			Curriculum:   pkg.Curriculum,
		})
	}
	c.JSON(http.StatusOK, packages)
}
