package db

import (
    "fmt"
    "os"
    "path/filepath"

    "gorm.io/driver/sqlite"
    "gorm.io/gorm"

    "github.com/tturner/rangerdanger/backend/internal/models"
)

// Connect opens a SQLite database, creating directories if needed.
// It also auto-migrates all model tables.
func Connect(path string) (*gorm.DB, error) {
    dir := filepath.Dir(path)
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return nil, fmt.Errorf("create db dir: %w", err)
    }

    database, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
    if err != nil {
        return nil, fmt.Errorf("open db: %w", err)
    }

    if err := database.AutoMigrate(
        &models.LabTemplate{},
        &models.LabInstance{},
        &models.NodeDefinition{},
        &models.Scenario{},
        &models.ScenarioRun{},
        &models.TelemetryPoint{},
    ); err != nil {
        return nil, fmt.Errorf("auto-migrate: %w", err)
    }

    return database, nil
}
