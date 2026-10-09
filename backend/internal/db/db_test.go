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

	if err := database.Create(&models.Scenario{ID: "scenario-1", Name: "Baseline"}).Error; err != nil {
		t.Fatalf("Create(): %v", err)
	}
	var got models.Scenario
	if err := database.First(&got, "id = ?", "scenario-1").Error; err != nil {
		t.Fatalf("query row: %v", err)
	}
	if got.Name != "Baseline" {
		t.Errorf("queried scenario name = %q, want Baseline", got.Name)
	}
	// Topology lives in the package catalog, never in the database.
	if database.Migrator().HasTable("lab_templates") {
		t.Error("a fresh database has a lab_templates table")
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
	if err := first.Create(&models.Scenario{ID: "kept", Name: "Before reconnect"}).Error; err != nil {
		t.Fatalf("create before reconnect: %v", err)
	}

	second := openTestDB(t, path)
	var got models.Scenario
	if err := second.First(&got, "id = ?", "kept").Error; err != nil {
		t.Fatalf("query after reconnect: %v", err)
	}
	if got.Name != "Before reconnect" {
		t.Errorf("row after reconnect = %#v, want the existing scenario", got)
	}
}
