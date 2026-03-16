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

	for _, file := range ymlFiles {
		if err := l.importLabFile(ctx, db, file); err != nil {
			return fmt.Errorf("import %s: %w", filepath.Base(file), err)
		}
	}

	return nil
}

// importLabFile imports a single lab definition YAML file.
func (l *Loader) importLabFile(ctx context.Context, db *gorm.DB, path string) error {
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
		tagsJSON, _ := json.Marshal(sc.Tags)
		stepsJSON, _ := json.Marshal(sc.Steps)
		scenario := models.Scenario{
			ID:            sc.ID,
			Name:          sc.Name,
			Description:   sc.Description,
			LabTemplateID: def.ID,
			Tags:          string(tagsJSON),
			Steps:         string(stepsJSON),
		}
		if err := db.WithContext(ctx).Where(models.Scenario{ID: sc.ID}).Assign(scenario).FirstOrCreate(&scenario).Error; err != nil {
			return err
		}
	}

	scenarioFiles, _ := filepath.Glob(filepath.Join(l.DefinitionsDir, "scenarios", "*.yml"))
	for _, file := range scenarioFiles {
		if err := l.importScenarioFile(ctx, db, file, def.ID); err != nil {
			return err
		}
	}

	return nil
}

func (l *Loader) importScenarioFile(ctx context.Context, db *gorm.DB, path string, templateID string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var sc ScenarioYAML
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}

	tagsJSON, _ := json.Marshal(sc.Tags)
	stepsJSON, _ := json.Marshal(sc.Steps)
	scenario := models.Scenario{
		ID:            sc.ID,
		Name:          sc.Name,
		Description:   sc.Description,
		LabTemplateID: templateID,
		Tags:          string(tagsJSON),
		Steps:         string(stepsJSON),
	}
	return db.WithContext(ctx).Where(models.Scenario{ID: sc.ID}).Assign(scenario).FirstOrCreate(&scenario).Error
}

// LabYAML mirrors the YAML schema for lab templates.
type LabYAML struct {
	ID             string         `yaml:"id"`
	Name           string         `yaml:"name"`
	Description    string         `yaml:"description"`
	FirewallConfig string         `yaml:"firewall_config"` // path relative to lab-definitions dir
	Networks       []NetworkYAML  `yaml:"networks"`
	Nodes          []NodeYAML     `yaml:"nodes"`
	Scenarios      []ScenarioYAML `yaml:"scenarios"`
}

// NetworkYAML defines a virtual network.
type NetworkYAML struct {
	ID          string `yaml:"id" json:"id,omitempty"`
	Name        string `yaml:"name" json:"name"`
	CIDR        string `yaml:"cidr" json:"cidr,omitempty"`
	Subnet      string `yaml:"subnet" json:"subnet,omitempty"`
	Zone        string `yaml:"zone" json:"zone,omitempty"`
	Description string `yaml:"description" json:"description,omitempty"`
}

// NodeYAML describes a node template in YAML.
type NodeYAML struct {
	ID        string   `yaml:"id" json:"id"`
	Type      string   `yaml:"type" json:"type"`
	Name      string   `yaml:"name" json:"name"`
	Networks  []string `yaml:"networks" json:"networks"`
	IP        string   `yaml:"ip" json:"ip,omitempty"`
	Container string   `yaml:"container" json:"container,omitempty"`
}

// ScenarioYAML defines scenario metadata loaded from YAML.
type ScenarioYAML struct {
	ID          string         `yaml:"id"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Tags        []string       `yaml:"tags"`
	Steps       []ScenarioStep `yaml:"steps"`
}

// ScenarioStep describes a single scenario instruction.
type ScenarioStep struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
}
