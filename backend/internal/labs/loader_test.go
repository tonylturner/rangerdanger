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

const validTopologyYAML = `id: substation
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
scenarios: []
`

const validScenarioYAML = `id: baseline
name: Baseline Review
summary: Inspect the weak policy
description: Identify unnecessary cross-zone access.
order: "1.2"
nodes: [rtac-1]
tags: [segmentation, firewall]
estimated_minutes: 15
validator: electrical-check
steps:
  - id: inspect-policy
    title: Inspect policy
    description: Review the active firewall rules.
    expected_config: weak
    node: rtac-1
`

func packageYAML(id string) string {
	return `schema: 1
id: ` + id + `
title: "Package ` + id + `"
revision: 3
topology: topology.yml
scenarios: scenarios
capabilities: [process.electrical]
`
}

// writePackage writes a self-contained package directory: its manifest,
// a topology whose firewall_config resolves inside the package, and the
// given scenario files keyed by file name.
func writePackage(t *testing.T, definitions, id, topologyID string, scenarios map[string]string) string {
	t.Helper()
	dir := filepath.Join(definitions, "packages", id)
	writeDefinition(t, filepath.Join(dir, "package.yml"), packageYAML(id))
	writeDefinition(t, filepath.Join(dir, "topology.yml"), strings.Replace(validTopologyYAML, "id: substation", "id: "+topologyID, 1))
	writeDefinition(t, filepath.Join(dir, "firewall", "weak.json"), "{}")
	if err := os.MkdirAll(filepath.Join(dir, "scenarios"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range scenarios {
		writeDefinition(t, filepath.Join(dir, "scenarios", name), content)
	}
	return dir
}

func scenarioNamed(id string) string {
	return strings.Replace(validScenarioYAML, "id: baseline", "id: "+id, 1)
}

func seedLoader(definitions string) *Loader {
	return NewLoader(definitions, testValidators)
}

func countRows(t *testing.T, database *gorm.DB, model any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := database.Model(model).Where(query, args...).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func TestLoadRequiresDefinitionsDirectory(t *testing.T) {
	_, err := seedLoader("").Load()
	if err == nil || !strings.Contains(err.Error(), "definitions dir not configured") {
		t.Fatalf("Load() error = %v, want configuration error", err)
	}
}

func TestLoadWithoutPackages(t *testing.T) {
	definitions := t.TempDir()
	// A top-level topology is no longer a curriculum source on its own.
	writeDefinition(t, filepath.Join(definitions, "substation.yml"), validTopologyYAML)
	_, err := seedLoader(definitions).Load()
	if err == nil || !strings.Contains(err.Error(), "no packages found") {
		t.Fatalf("Load() error = %v, want missing-packages error", err)
	}
}

func TestSeedImportsPackage(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
	database := testDatabase(t)
	catalog, err := seedLoader(definitions).Seed(context.Background(), database, "alpha")
	if err != nil {
		t.Fatalf("Seed(): %v", err)
	}

	info, ok := catalog.Info("alpha")
	want := PackageInfo{ID: "alpha", Title: "Package alpha", Revision: 3, TemplateID: "substation", Capabilities: []string{CapabilityProcessElectrical}}
	if !ok || !reflect.DeepEqual(info, want) {
		t.Errorf("catalog.Info(alpha) = %#v, %v; want %#v", info, ok, want)
	}

	var template models.LabTemplate
	if err := database.First(&template, "id = ?", "substation").Error; err != nil {
		t.Fatalf("load template: %v", err)
	}
	if template.PackageID != "alpha" {
		t.Errorf("template package = %q, want alpha", template.PackageID)
	}
	if want := "packages/alpha/firewall/weak.json"; template.FirewallConfigPath != want {
		t.Errorf("firewall config path = %q, want %q (resolved from the topology directory)", template.FirewallConfigPath, want)
	}
	var topology map[string]json.RawMessage
	if err := json.Unmarshal([]byte(template.Topology), &topology); err != nil {
		t.Fatalf("decode topology %q: %v", template.Topology, err)
	}
	if _, inline := topology["scenarios"]; inline || len(topology) != 2 {
		t.Errorf("topology keys = %v, want only networks and nodes", reflect.ValueOf(topology).MapKeys())
	}
	var nodes []NodeYAML
	if err := json.Unmarshal(topology["nodes"], &nodes); err != nil {
		t.Fatal(err)
	}
	wantNodes := []NodeYAML{{ID: "rtac-1", Type: "controller", Name: "RTAC", Networks: []string{"ot-ops"}, IP: "10.30.30.20", Container: "rtac_sim"}}
	if !reflect.DeepEqual(nodes, wantNodes) {
		t.Errorf("topology nodes = %#v, want %#v", nodes, wantNodes)
	}

	var scenario models.Scenario
	if err := database.First(&scenario, "id = ?", "baseline").Error; err != nil {
		t.Fatalf("load scenario: %v", err)
	}
	if scenario.PackageID != "alpha" || scenario.LabTemplateID != "substation" || scenario.Validator != "electrical-check" {
		t.Errorf("scenario ownership = (%q, %q, %q), want (alpha, substation, electrical-check)", scenario.PackageID, scenario.LabTemplateID, scenario.Validator)
	}
	var steps []ScenarioStep
	if err := json.Unmarshal([]byte(scenario.Steps), &steps); err != nil {
		t.Fatal(err)
	}
	wantSteps := []ScenarioStep{{ID: "inspect-policy", Title: "Inspect policy", Description: "Review the active firewall rules.", ExpectedConfig: "weak", Node: "rtac-1"}}
	if !reflect.DeepEqual(steps, wantSteps) {
		t.Errorf("steps = %#v, want %#v", steps, wantSteps)
	}
}

func TestSeedRefreshesExistingRows(t *testing.T) {
	definitions := t.TempDir()
	dir := writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
	database := testDatabase(t)
	loader := seedLoader(definitions)
	ctx := context.Background()
	if _, err := loader.Seed(ctx, database, "alpha"); err != nil {
		t.Fatalf("first Seed(): %v", err)
	}

	updated := strings.Replace(validScenarioYAML, "name: Baseline Review", "name: Updated Baseline Review", 1)
	updated = strings.Replace(updated, "estimated_minutes: 15\n", "", 1)
	updated = strings.Replace(updated, "validator: electrical-check\n", "", 1)
	writeDefinition(t, filepath.Join(dir, "scenarios", "baseline.yml"), updated)
	if _, err := loader.Seed(ctx, database, "alpha"); err != nil {
		t.Fatalf("second Seed(): %v", err)
	}

	if templates, scenarios := countRows(t, database, &models.LabTemplate{}, "1 = 1"), countRows(t, database, &models.Scenario{}, "1 = 1"); templates != 1 || scenarios != 1 {
		t.Fatalf("row counts after re-seed: templates=%d scenarios=%d, want one each", templates, scenarios)
	}
	var scenario models.Scenario
	if err := database.First(&scenario, "id = ?", "baseline").Error; err != nil {
		t.Fatalf("load refreshed scenario: %v", err)
	}
	// Fields that became empty on disk must be cleared, not kept.
	if scenario.Name != "Updated Baseline Review" || scenario.EstimatedMinutes != 0 || scenario.Validator != "" {
		t.Errorf("scenario = %#v, want refreshed name and cleared estimate and validator", scenario)
	}
}

func TestSeedPrunesStaleAndLegacyRows(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
	database := testDatabase(t)
	if err := database.Create(&models.Scenario{ID: "stale", Name: "Old scenario", PackageID: "alpha"}).Error; err != nil {
		t.Fatal(err)
	}
	// Rows written before packages existed have a NULL package_id.
	if err := database.Exec(`INSERT INTO scenarios (id, name, lab_template_id) VALUES ('legacy', 'Legacy', 'substation')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO lab_templates (id, name) VALUES ('legacy-template', 'Legacy')`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := seedLoader(definitions).Seed(context.Background(), database, "alpha"); err != nil {
		t.Fatalf("Seed(): %v", err)
	}
	if n := countRows(t, database, &models.Scenario{}, "id IN ?", []string{"stale", "legacy"}); n != 0 {
		t.Errorf("stale and legacy scenarios left = %d, want 0", n)
	}
	if n := countRows(t, database, &models.LabTemplate{}, "id = ?", "legacy-template"); n != 0 {
		t.Errorf("legacy template rows = %d, want 0", n)
	}
	if n := countRows(t, database, &models.Scenario{}, "id = ?", "baseline"); n != 1 {
		t.Errorf("current scenario rows = %d, want 1", n)
	}
}

func TestSeedPrunesEmptyCurriculum(t *testing.T) {
	definitions := t.TempDir()
	dir := writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
	database := testDatabase(t)
	loader := seedLoader(definitions)
	if _, err := loader.Seed(context.Background(), database, "alpha"); err != nil {
		t.Fatalf("first Seed(): %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "scenarios", "baseline.yml")); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Seed(context.Background(), database, "alpha"); err != nil {
		t.Fatalf("Seed() of an empty curriculum: %v", err)
	}
	if n := countRows(t, database, &models.Scenario{}, "package_id = ?", "alpha"); n != 0 {
		t.Errorf("scenarios after emptying the package = %d, want 0", n)
	}
	if n := countRows(t, database, &models.LabTemplate{}, "id = ?", "substation"); n != 1 {
		t.Errorf("template rows = %d, want the template kept", n)
	}
}

func TestSeedPrunesRemovedPackage(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
	betaDir := writePackage(t, definitions, "beta", "beta-topology", map[string]string{"beta-lab.yml": scenarioNamed("beta-lab")})
	database := testDatabase(t)
	loader := seedLoader(definitions)
	if _, err := loader.Seed(context.Background(), database, "alpha"); err != nil {
		t.Fatalf("first Seed(): %v", err)
	}
	if n := countRows(t, database, &models.Scenario{}, "package_id = ?", "beta"); n != 1 {
		t.Fatalf("beta scenarios after first seed = %d, want 1", n)
	}
	if err := os.RemoveAll(betaDir); err != nil {
		t.Fatal(err)
	}
	catalog, err := loader.Seed(context.Background(), database, "alpha")
	if err != nil {
		t.Fatalf("Seed() after removing beta: %v", err)
	}
	if _, ok := catalog.Info("beta"); ok {
		t.Error("catalog still lists the removed package")
	}
	if n := countRows(t, database, &models.Scenario{}, "package_id = ?", "beta"); n != 0 {
		t.Errorf("scenarios of removed package = %d, want 0", n)
	}
	if n := countRows(t, database, &models.LabTemplate{}, "id = ?", "beta-topology"); n != 0 {
		t.Errorf("templates of removed package = %d, want 0", n)
	}
	if n := countRows(t, database, &models.Scenario{}, "package_id = ?", "alpha"); n != 1 {
		t.Errorf("alpha scenarios = %d, want 1", n)
	}
}

func TestLoadRejectsDuplicateScenarioAcrossPackages(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
	writePackage(t, definitions, "beta", "beta-topology", map[string]string{"copy.yml": validScenarioYAML})
	_, err := seedLoader(definitions).Load()
	if err == nil || !strings.Contains(err.Error(), `package beta: scenario id "baseline" is already used by package alpha`) {
		t.Fatalf("Load() error = %v, want cross-package duplicate slug", err)
	}
}

func TestLoadRejectsSharedTopologyID(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "substation", nil)
	writePackage(t, definitions, "beta", "substation", nil)
	_, err := seedLoader(definitions).Load()
	if err == nil || !strings.Contains(err.Error(), `package beta: topology id "substation" is already used by package alpha`) {
		t.Fatalf("Load() error = %v, want duplicate topology id", err)
	}
}

func TestSeedWritesNothingWhenAnyPackageFails(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
	writePackage(t, definitions, "beta", "beta-topology", map[string]string{"invalid.yml": `id: invalid-check
name: Invalid Check
order: "2.1"
steps:
  - id: empty-check
    title: Empty check
    action:
      type: check
      expect: {}
`})
	database := testDatabase(t)
	_, err := seedLoader(definitions).Seed(context.Background(), database, "alpha")
	if err == nil {
		t.Fatal("Seed() error = nil, want validation failure")
	}
	for _, want := range []string{"package beta", "scenario invalid-check", "step 0", `"Empty check"`, "check expect must not be empty"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Seed() error = %q, want it to contain %q", err, want)
		}
	}
	if templates, scenarios := countRows(t, database, &models.LabTemplate{}, "1 = 1"), countRows(t, database, &models.Scenario{}, "1 = 1"); templates != 0 || scenarios != 0 {
		t.Fatalf("failed seed left writes: templates=%d scenarios=%d", templates, scenarios)
	}
}

func TestSeedRequiresActivePackage(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
	database := testDatabase(t)
	_, err := seedLoader(definitions).Seed(context.Background(), database, "missing")
	if err == nil || !strings.Contains(err.Error(), `active package "missing" not found`) {
		t.Fatalf("Seed() error = %v, want unknown active package", err)
	}
	if n := countRows(t, database, &models.Scenario{}, "1 = 1"); n != 0 {
		t.Errorf("unknown active package left %d scenario rows", n)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		edit  func(string) string
		field string
	}{
		{"package manifest", "package.yml", func(s string) string { return s + "owner: someone\n" }, "owner"},
		{"topology", "topology.yml", func(s string) string { return s + "region: eu\n" }, "region"},
		{"scenario", "scenarios/baseline.yml", func(s string) string { return s + "baseline_grid_state: peak\n" }, "baseline_grid_state"},
		{"scenario step", "scenarios/baseline.yml", func(s string) string {
			return strings.Replace(s, "    node: rtac-1\n", "    node: rtac-1\n    progress_key: x\n", 1)
		}, "progress_key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definitions := t.TempDir()
			dir := writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
			path := filepath.Join(dir, test.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeDefinition(t, path, test.edit(string(data)))
			_, err = seedLoader(definitions).Load()
			if err == nil || !strings.Contains(err.Error(), "field "+test.field+" not found") {
				t.Fatalf("Load() error = %v, want unknown field %q rejected", err, test.field)
			}
		})
	}
}

func TestLoadRejectsInvalidPackages(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		edit    func(string) string
		problem string
	}{
		{"schema", "package.yml", func(s string) string { return strings.Replace(s, "schema: 1", "schema: 2", 1) }, "schema must be 1, got 2"},
		{"id not a slug", "package.yml", func(s string) string { return strings.Replace(s, "id: alpha", "id: Alpha", 1) }, `id "Alpha" must be lowercase`},
		{"id differs from directory", "package.yml", func(s string) string { return strings.Replace(s, "id: alpha", "id: gamma", 1) }, `id "gamma" must match its directory name "alpha"`},
		{"title", "package.yml", func(s string) string { return strings.Replace(s, `title: "Package alpha"`, `title: ""`, 1) }, "title must not be empty"},
		{"revision", "package.yml", func(s string) string { return strings.Replace(s, "revision: 3", "revision: 0", 1) }, "revision must be at least 1"},
		{"unknown capability", "package.yml", func(s string) string {
			return strings.Replace(s, "[process.electrical]", "[process.electrical, protocol.iec104]", 1)
		}, `unknown capability "protocol.iec104"`},
		{"missing topology", "package.yml", func(s string) string { return strings.Replace(s, "topology.yml", "missing.yml", 1) }, "topology: open"},
		{"missing scenarios directory", "package.yml", func(s string) string {
			return strings.Replace(s, "scenarios: scenarios", "scenarios: missing", 1)
		}, "scenarios directory"},
		{"inline scenarios", "topology.yml", func(s string) string {
			return strings.Replace(s, "scenarios: []", "scenarios:\n  - id: inline\n", 1)
		}, "inline scenarios are not supported"},
		{"missing firewall config", "topology.yml", func(s string) string {
			return strings.Replace(s, "firewall/weak.json", "firewall/missing.json", 1)
		}, "firewall_config: stat"},
		{"firewall config outside definitions", "topology.yml", func(s string) string {
			return strings.Replace(s, "firewall/weak.json", "../../../outside.json", 1)
		}, "must stay inside the definitions directory"},
		{"missing validator key", "scenarios/baseline.yml", func(s string) string {
			return strings.Replace(s, "validator: electrical-check", "validator: unknown-check", 1)
		}, `unknown validator "unknown-check"`},
		{"missing step id", "scenarios/baseline.yml", func(s string) string {
			return strings.Replace(s, "  - id: inspect-policy\n    title:", "  - title:", 1)
		}, "step id must not be empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definitions := t.TempDir()
			dir := writePackage(t, definitions, "alpha", "substation", map[string]string{"baseline.yml": validScenarioYAML})
			writeDefinition(t, filepath.Join(definitions, "..", "outside.json"), "{}")
			path := filepath.Join(dir, test.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeDefinition(t, path, test.edit(string(data)))
			_, err = seedLoader(definitions).Load()
			if err == nil || !strings.Contains(err.Error(), test.problem) {
				t.Fatalf("Load() error = %v, want %q", err, test.problem)
			}
			if !strings.Contains(err.Error(), "package alpha") {
				t.Errorf("Load() error = %q, want the package named", err)
			}
		})
	}
}

func TestLoadRequiresValidatorCapability(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "substation", map[string]string{
		"baseline.yml": strings.Replace(validScenarioYAML, "validator: electrical-check", "validator: audited-policy", 1),
	})
	_, err := seedLoader(definitions).Load()
	if err == nil || !strings.Contains(err.Error(), `validator "audited-policy" requires capability "policy.containd"`) {
		t.Fatalf("Load() error = %v, want missing capability", err)
	}
}
