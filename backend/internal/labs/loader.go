package labs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/models"
)

// Loader imports YAML definitions into persistence.
type Loader struct {
	DefinitionsDir string
}

// NewLoader builds a Loader for a given directory.
func NewLoader(definitionsDir string) *Loader {
	return &Loader{DefinitionsDir: definitionsDir}
}

func loadScenarioFiles(dir string) ([]ScenarioYAML, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("find scenario YAML files: %w", err)
	}

	scenarios := make([]ScenarioYAML, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read scenario %s: %w", filepath.Base(file), err)
		}
		var scenario ScenarioYAML
		if err := yaml.Unmarshal(data, &scenario); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Base(file), err)
		}
		scenarios = append(scenarios, scenario)
	}
	return scenarios, nil
}

// SeedFromDisk ingests all YAML lab definition files at startup.
func (l *Loader) SeedFromDisk(ctx context.Context, db *gorm.DB) error {
	if l.DefinitionsDir == "" {
		return fmt.Errorf("definitions dir not configured")
	}

	// Load all *.yml files in the definitions directory
	ymlFiles, _ := filepath.Glob(filepath.Join(l.DefinitionsDir, "*.yml"))
	if len(ymlFiles) == 0 {
		return fmt.Errorf("no lab definition YAML files found in %s", l.DefinitionsDir)
	}

	scenarios, err := loadScenarioFiles(filepath.Join(l.DefinitionsDir, "scenarios"))
	if err != nil {
		return err
	}

	for _, file := range ymlFiles {
		if err := l.importLabFile(ctx, db, file, scenarios); err != nil {
			return fmt.Errorf("import %s: %w", filepath.Base(file), err)
		}
	}

	return nil
}

// importLabFile imports a single lab definition YAML file.
func (l *Loader) importLabFile(ctx context.Context, db *gorm.DB, path string, scenarios []ScenarioYAML) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read lab: %w", err)
	}

	var def LabYAML
	if err := yaml.Unmarshal(data, &def); err != nil {
		return fmt.Errorf("unmarshal lab: %w", err)
	}

	if def.ID == "" {
		return fmt.Errorf("lab definition missing id in %s", filepath.Base(path))
	}

	if err := ValidateLab(&def, scenarios); err != nil {
		return err
	}

	topology := map[string]any{
		"networks":  def.Networks,
		"nodes":     def.Nodes,
		"scenarios": def.Scenarios,
	}
	topologyJSON, err := json.Marshal(topology)
	if err != nil {
		return fmt.Errorf("marshal topology: %w", err)
	}

	var defaultScenarioIDs []string
	for _, scn := range def.Scenarios {
		defaultScenarioIDs = append(defaultScenarioIDs, scn.ID)
	}
	defaultScenariosJSON, _ := json.Marshal(defaultScenarioIDs)

	tmpl := models.LabTemplate{
		ID:                 def.ID,
		Name:               def.Name,
		Description:        def.Description,
		Topology:           string(topologyJSON),
		DefaultScenarios:   string(defaultScenariosJSON),
		ComposeFile:        "docker-compose.yml",
		FirewallConfigPath: def.FirewallConfig,
	}

	if err := db.WithContext(ctx).Where(models.LabTemplate{ID: def.ID}).Assign(tmpl).FirstOrCreate(&tmpl).Error; err != nil {
		return err
	}

	for _, sc := range def.Scenarios {
		if err := l.importScenario(ctx, db, sc, def.ID); err != nil {
			return err
		}
	}
	for _, sc := range scenarios {
		if err := l.importScenario(ctx, db, sc, def.ID); err != nil {
			return err
		}
	}

	// Collect all valid exercise IDs and delete stale DB entries
	validIDs := make(map[string]bool)
	for _, sc := range def.Scenarios {
		validIDs[sc.ID] = true
	}
	for _, sc := range scenarios {
		validIDs[sc.ID] = true
	}
	if len(validIDs) > 0 {
		var ids []string
		for id := range validIDs {
			ids = append(ids, id)
		}
		if err := db.WithContext(ctx).Where("lab_template_id = ? AND id NOT IN ?", def.ID, ids).Delete(&models.Scenario{}).Error; err != nil {
			return err
		}
	}

	return nil
}

func (l *Loader) importScenario(ctx context.Context, db *gorm.DB, sc ScenarioYAML, templateID string) error {
	tagsJSON, _ := json.Marshal(sc.Tags)
	stepsJSON, _ := json.Marshal(sc.Steps)
	nodesJSON, _ := json.Marshal(sc.Nodes)
	scenario := models.Scenario{
		ID:               sc.ID,
		Name:             sc.Name,
		Summary:          sc.Summary,
		Description:      sc.Description,
		Order:            sc.Order,
		LabTemplateID:    templateID,
		Tags:             string(tagsJSON),
		Steps:            string(stepsJSON),
		Nodes:            string(nodesJSON),
		EstimatedMinutes: sc.EstimatedMinutes,
	}
	return db.WithContext(ctx).Where(models.Scenario{ID: sc.ID}).Assign(scenario).FirstOrCreate(&scenario).Error
}
