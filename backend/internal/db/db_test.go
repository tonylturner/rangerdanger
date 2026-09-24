package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/models"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	database, err := Connect(path)
	if err != nil {
		t.Fatalf("Connect(%q): %v", path, err)
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

func TestConnectCreatesNestedDatabaseAndMigratesModels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "database", "rangerdanger.sqlite")
	database := openTestDB(t, path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("database path is not a regular file: mode %v", info.Mode())
	}

	cases := []struct {
		name      string
		row       any
		got       any
		id        string
		wantValue string
		readValue func(any) string
	}{
		{"lab template", &models.LabTemplate{ID: "template-1", Name: "Substation"}, &models.LabTemplate{}, "template-1", "Substation", func(row any) string { return row.(*models.LabTemplate).Name }},
		{"lab instance", &models.LabInstance{ID: "instance-1", Name: "Range 1"}, &models.LabInstance{}, "instance-1", "Range 1", func(row any) string { return row.(*models.LabInstance).Name }},
		{"node definition", &models.NodeDefinition{ID: "node-1", Name: "RTAC"}, &models.NodeDefinition{}, "node-1", "RTAC", func(row any) string { return row.(*models.NodeDefinition).Name }},
		{"scenario", &models.Scenario{ID: "scenario-1", Name: "Baseline"}, &models.Scenario{}, "scenario-1", "Baseline", func(row any) string { return row.(*models.Scenario).Name }},
		{"scenario run", &models.ScenarioRun{ID: "run-1", Status: "running"}, &models.ScenarioRun{}, "run-1", "running", func(row any) string { return row.(*models.ScenarioRun).Status }},
		{"telemetry point", &models.TelemetryPoint{ID: "point-1", Metric: "voltage"}, &models.TelemetryPoint{}, "point-1", "voltage", func(row any) string { return row.(*models.TelemetryPoint).Metric }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := database.Create(tc.row).Error; err != nil {
				t.Fatalf("Create(): %v", err)
			}
			if err := database.First(tc.got, "id = ?", tc.id).Error; err != nil {
				t.Fatalf("query row %q: %v", tc.id, err)
			}
			if tc.readValue(tc.got) != tc.wantValue {
				t.Errorf("queried row value = %q, want %q", tc.readValue(tc.got), tc.wantValue)
			}
		})
	}
}

func TestConnectParentIsRegularFile(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", parent, err)
	}

	database, err := Connect(filepath.Join(parent, "database.sqlite"))
	if err == nil {
		t.Fatal("Connect() error = nil, want a directory creation error")
	}
	if database != nil {
		t.Errorf("Connect() database = %#v, want nil", database)
	}
	if !strings.Contains(err.Error(), "create db dir") {
		t.Errorf("Connect() error = %q, want it to contain %q", err, "create db dir")
	}
}

func TestConnectIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.sqlite")
	first := openTestDB(t, path)
	if err := first.Create(&models.LabTemplate{ID: "kept", Name: "Before reconnect"}).Error; err != nil {
		t.Fatalf("create before reconnect: %v", err)
	}

	second := openTestDB(t, path)
	var got models.LabTemplate
	if err := second.First(&got, "id = ?", "kept").Error; err != nil {
		t.Fatalf("query after reconnect: %v", err)
	}
	if got.Name != "Before reconnect" {
		t.Errorf("row after reconnect = %#v, want existing template", got)
	}
}
