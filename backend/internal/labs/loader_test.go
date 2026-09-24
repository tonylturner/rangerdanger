package labs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/models"
	"gorm.io/gorm"
)

func testDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Connect(filepath.Join(t.TempDir(), "rangerdanger.sqlite"))
	if err != nil {
		t.Fatalf("db.Connect(): %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := database.DB()
		if err != nil {
			t.Errorf("database.DB(): %v", err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return database
}

func writeDefinition(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

const validLabYAML = `id: substation
name: Substation Lab
description: Segmentation practice
firewall_config: firewall/weak.json
networks:
  - id: ot-ops
    name: OT Operations
    cidr: 10.30.30.0/24
    zone: lan2
nodes:
  - id: rtac-1
    type: controller
    name: RTAC
    networks: [ot-ops]
    ip: 10.30.30.20
    container: rtac_sim
scenarios:
  - id: baseline
    name: Baseline Review
    summary: Inspect the weak policy
    description: Identify unnecessary cross-zone access.
    order: "1.2"
    nodes: [rtac-1]
    tags: [segmentation, firewall]
    estimated_minutes: 15
    steps:
      - title: Inspect policy
        description: Review the active firewall rules.
        expected_config: weak
        node: rtac-1
`

func TestSeedFromDiskEmptyDirectory(t *testing.T) {
	definitions := t.TempDir()
	err := NewLoader(definitions).SeedFromDisk(context.Background(), testDatabase(t))
	if err == nil || !strings.Contains(err.Error(), "no lab definition YAML files found") {
		t.Fatalf("SeedFromDisk() error = %v, want missing-YAML error", err)
	}
}

func TestSeedFromDiskImportsTemplateTopology(t *testing.T) {
	definitions := t.TempDir()
	writeDefinition(t, filepath.Join(definitions, "substation.yml"), validLabYAML)
	database := testDatabase(t)
	if err := NewLoader(definitions).SeedFromDisk(context.Background(), database); err != nil {
		t.Fatalf("SeedFromDisk(): %v", err)
	}

	var template models.LabTemplate
	if err := database.First(&template, "id = ?", "substation").Error; err != nil {
		t.Fatalf("load template: %v", err)
	}
	wantNetworks := []NetworkYAML{{ID: "ot-ops", Name: "OT Operations", CIDR: "10.30.30.0/24", Zone: "lan2"}}
	wantNodes := []NodeYAML{{ID: "rtac-1", Type: "controller", Name: "RTAC", Networks: []string{"ot-ops"}, IP: "10.30.30.20", Container: "rtac_sim"}}
	wantScenarios := []ScenarioYAML{{
		ID: "baseline", Name: "Baseline Review", Summary: "Inspect the weak policy",
		Description: "Identify unnecessary cross-zone access.", Order: "1.2",
		Nodes: []string{"rtac-1"}, Tags: []string{"segmentation", "firewall"}, EstimatedMinutes: 15,
		Steps: []ScenarioStep{{Title: "Inspect policy", Description: "Review the active firewall rules.", ExpectedConfig: "weak", Node: "rtac-1"}},
	}}
	var topology struct {
		Networks  []NetworkYAML  `json:"networks"`
		Nodes     []NodeYAML     `json:"nodes"`
		Scenarios []ScenarioYAML `json:"scenarios"`
	}
	if err := json.Unmarshal([]byte(template.Topology), &topology); err != nil {
		t.Fatalf("decode template topology %q: %v", template.Topology, err)
	}
	if !reflect.DeepEqual(topology.Networks, wantNetworks) {
		t.Errorf("topology networks = %#v, want %#v", topology.Networks, wantNetworks)
	}
	if !reflect.DeepEqual(topology.Nodes, wantNodes) {
		t.Errorf("topology nodes = %#v, want %#v", topology.Nodes, wantNodes)
	}
	if !reflect.DeepEqual(topology.Scenarios, wantScenarios) {
		t.Errorf("topology scenarios = %#v, want %#v", topology.Scenarios, wantScenarios)
	}
	var defaultScenarios []string
	if err := json.Unmarshal([]byte(template.DefaultScenarios), &defaultScenarios); err != nil {
		t.Fatalf("decode default scenarios %q: %v", template.DefaultScenarios, err)
	}
	if want := []string{"baseline"}; !reflect.DeepEqual(defaultScenarios, want) {
		t.Errorf("default scenarios = %#v, want %#v", defaultScenarios, want)
	}
}

func TestSeedFromDiskMissingIDNamesFile(t *testing.T) {
	definitions := t.TempDir()
	writeDefinition(t, filepath.Join(definitions, "no-id.yml"), "name: Missing identifier\n")
	err := NewLoader(definitions).SeedFromDisk(context.Background(), testDatabase(t))
	if err == nil {
		t.Fatal("SeedFromDisk() error = nil, want missing-ID error")
	}
	for _, want := range []string{"missing id", "no-id.yml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("SeedFromDisk() error = %q, want it to contain %q", err, want)
		}
	}
}

func TestSeedFromDiskRefreshesExistingRows(t *testing.T) {
	definitions := t.TempDir()
	path := filepath.Join(definitions, "substation.yml")
	writeDefinition(t, path, validLabYAML)
	database := testDatabase(t)
	loader := NewLoader(definitions)
	ctx := context.Background()
	if err := loader.SeedFromDisk(ctx, database); err != nil {
		t.Fatalf("first SeedFromDisk(): %v", err)
	}

	updated := strings.Replace(validLabYAML, "name: Substation Lab", "name: Updated Substation Lab", 1)
	updated = strings.Replace(updated, "name: Baseline Review", "name: Updated Baseline Review", 1)
	updated = strings.Replace(updated, "estimated_minutes: 15", "estimated_minutes: 25", 1)
	writeDefinition(t, path, updated)
	if err := loader.SeedFromDisk(ctx, database); err != nil {
		t.Fatalf("second SeedFromDisk(): %v", err)
	}

	var templateCount, scenarioCount int64
	if err := database.Model(&models.LabTemplate{}).Where("id = ?", "substation").Count(&templateCount).Error; err != nil {
		t.Fatalf("count templates: %v", err)
	}
	if err := database.Model(&models.Scenario{}).Where("id = ?", "baseline").Count(&scenarioCount).Error; err != nil {
		t.Fatalf("count scenarios: %v", err)
	}
	if templateCount != 1 || scenarioCount != 1 {
		t.Fatalf("row counts after re-seed: templates=%d scenarios=%d, want one each", templateCount, scenarioCount)
	}
	var template models.LabTemplate
	if err := database.First(&template, "id = ?", "substation").Error; err != nil {
		t.Fatalf("load refreshed template: %v", err)
	}
	if template.Name != "Updated Substation Lab" {
		t.Errorf("template name = %q, want updated name", template.Name)
	}
	var scenario models.Scenario
	if err := database.First(&scenario, "id = ?", "baseline").Error; err != nil {
		t.Fatalf("load refreshed scenario: %v", err)
	}
	if scenario.Name != "Updated Baseline Review" || scenario.EstimatedMinutes != 25 {
		t.Errorf("scenario = %#v, want refreshed name and estimate", scenario)
	}
}

func TestSeedFromDiskImportsPerScenarioAndDeletesStaleRows(t *testing.T) {
	definitions := t.TempDir()
	writeDefinition(t, filepath.Join(definitions, "substation.yml"), validLabYAML)
	writeDefinition(t, filepath.Join(definitions, "scenarios", "advanced.yml"), `id: advanced
name: Advanced Exercise
summary: Use the protected path
description: Validate access after hardening.
order: "2.1"
nodes: [rtac-1]
tags: [hardening]
estimated_minutes: 18
steps:
  - title: Verify block
    description: Confirm untrusted traffic is denied.
`)
	database := testDatabase(t)
	if err := database.Create(&models.Scenario{ID: "stale", Name: "Old scenario", LabTemplateID: "substation"}).Error; err != nil {
		t.Fatalf("create stale scenario: %v", err)
	}
	if err := NewLoader(definitions).SeedFromDisk(context.Background(), database); err != nil {
		t.Fatalf("SeedFromDisk(): %v", err)
	}

	var imported models.Scenario
	if err := database.First(&imported, "id = ?", "advanced").Error; err != nil {
		t.Fatalf("load per-file scenario: %v", err)
	}
	if imported.LabTemplateID != "substation" {
		t.Errorf("scenario template ID = %q, want %q", imported.LabTemplateID, "substation")
	}
	var staleCount int64
	if err := database.Model(&models.Scenario{}).Where("id = ?", "stale").Count(&staleCount).Error; err != nil {
		t.Fatalf("count stale scenario: %v", err)
	}
	if staleCount != 0 {
		t.Errorf("stale scenario count = %d, want 0", staleCount)
	}
}

func TestSeedFromDiskRequiresDefinitionsDirectory(t *testing.T) {
	err := NewLoader("").SeedFromDisk(context.Background(), testDatabase(t))
	if err == nil || !strings.Contains(err.Error(), "definitions dir not configured") {
		t.Fatalf("SeedFromDisk() error = %v, want configuration error", err)
	}
}
