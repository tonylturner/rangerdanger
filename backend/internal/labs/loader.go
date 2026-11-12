package labs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tturner/rangerdanger/backend/internal/models"
)

// Loader imports curriculum packages into persistence.
type Loader struct {
	DefinitionsDir string
	// Validators lists the validator keys scenarios may declare and the
	// capabilities each one needs.
	Validators ValidatorRequirements
}

// NewLoader builds a Loader for a definitions directory and validator set.
func NewLoader(definitionsDir string, validators ValidatorRequirements) *Loader {
	return &Loader{DefinitionsDir: definitionsDir, Validators: validators}
}

// Load parses and validates every packages/*/package.yml without touching
// persistence. Any package failure fails the whole load.
func (l *Loader) Load() (*Catalog, error) {
	if l.DefinitionsDir == "" {
		return nil, fmt.Errorf("definitions dir not configured")
	}
	manifests, err := filepath.Glob(filepath.Join(l.DefinitionsDir, packagesDirName, "*", packageManifestName))
	if err != nil {
		return nil, fmt.Errorf("find packages: %w", err)
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("no packages found in %s", filepath.Join(l.DefinitionsDir, packagesDirName))
	}
	sort.Strings(manifests)

	catalog := &Catalog{Packages: make([]Package, 0, len(manifests))}
	templateOwners := map[string]string{}
	for _, manifest := range manifests {
		pkg, err := l.LoadPackage(manifest)
		if err != nil {
			return nil, fmt.Errorf("package %s: %w", filepath.Base(filepath.Dir(manifest)), err)
		}
		if owner, taken := templateOwners[pkg.Template.ID]; taken {
			return nil, fmt.Errorf("package %s: topology id %q is already used by package %s", pkg.ID, pkg.Template.ID, owner)
		}
		templateOwners[pkg.Template.ID] = pkg.ID
		catalog.Packages = append(catalog.Packages, pkg)
	}
	return catalog, nil
}

// Seed loads every package, requires activeID among them, and only then
// writes and prunes the entire scenario projection in one transaction.
func (l *Loader) Seed(ctx context.Context, db *gorm.DB, activeID string) (*Catalog, error) {
	catalog, err := l.Load()
	if err != nil {
		return nil, err
	}
	if _, ok := catalog.Info(activeID); !ok {
		return nil, fmt.Errorf("active package %q not found in %s", activeID, filepath.Join(l.DefinitionsDir, packagesDirName))
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, pkg := range catalog.Packages {
			if err := storePackage(tx, pkg); err != nil {
				return fmt.Errorf("store package %s: %w", pkg.ID, err)
			}
		}
		return pruneRemovedPackages(ctx, tx, catalog)
	}); err != nil {
		return nil, err
	}
	return catalog, nil
}

// storePackage writes the package's scenarios and drops the ones whose file
// is gone. The topology stays in the catalog; nothing persists it.
func storePackage(tx *gorm.DB, pkg Package) error {
	ids := make([]string, 0, len(pkg.Scenarios))
	for _, scenario := range pkg.Scenarios {
		row, err := scenarioRow(scenario, pkg.ID, pkg.Template.ID)
		if err != nil {
			return err
		}
		if err := upsert(tx, &row); err != nil {
			return fmt.Errorf("upsert scenario %s: %w", row.ID, err)
		}
		ids = append(ids, scenario.ID)
	}

	stale := tx.Where("package_id = ?", pkg.ID)
	if len(ids) > 0 {
		stale = stale.Where("id NOT IN ?", ids)
	}
	if err := stale.Delete(&models.Scenario{}).Error; err != nil {
		return fmt.Errorf("prune scenarios: %w", err)
	}
	return nil
}

// upsert inserts a row or overwrites every non-key column, so a field that
// became empty on disk is cleared too.
func upsert(tx *gorm.DB, row any) error {
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "package_id"}, {Name: "id"}}, UpdateAll: true}).Create(row).Error
}

func scenarioRow(sc ScenarioYAML, packageID, templateID string) (models.Scenario, error) {
	encode := func(field string, value any) (string, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("scenario %s: marshal %s: %w", sc.ID, field, err)
		}
		return string(data), nil
	}
	tags, err := encode("tags", sc.Tags)
	if err != nil {
		return models.Scenario{}, err
	}
	steps, err := encode("steps", sc.Steps)
	if err != nil {
		return models.Scenario{}, err
	}
	nodes, err := encode("nodes", sc.Nodes)
	if err != nil {
		return models.Scenario{}, err
	}
	return models.Scenario{
		ID:               sc.ID,
		PackageID:        packageID,
		Name:             sc.Name,
		Summary:          sc.Summary,
		Description:      sc.Description,
		Order:            sc.Order,
		LabTemplateID:    templateID,
		Tags:             tags,
		Steps:            steps,
		Nodes:            nodes,
		EstimatedMinutes: sc.EstimatedMinutes,
		Validator:        sc.Validator,
	}, nil
}

func pruneRemovedPackages(ctx context.Context, db *gorm.DB, catalog *Catalog) error {
	ids := make([]string, len(catalog.Packages))
	for index, pkg := range catalog.Packages {
		ids[index] = pkg.ID
	}
	const gone = "package_id NOT IN ?"
	if err := db.WithContext(ctx).Where(gone, ids).Delete(&models.Scenario{}).Error; err != nil {
		return fmt.Errorf("prune scenarios of removed packages: %w", err)
	}
	return nil
}
