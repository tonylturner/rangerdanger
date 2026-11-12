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

	if err := migrateScenarios(database); err != nil {
		return nil, fmt.Errorf("auto-migrate: %w", err)
	}

	return database, nil
}

// Scenarios are a seed projection. Rebuild only that table when upgrading
// the old global key, leaving all unrelated workshop state untouched.
func migrateScenarios(database *gorm.DB) error {
	return database.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable(&models.Scenario{}) {
			var columns []struct {
				Name    string
				PK      int
				Notnull int
			}
			if err := tx.Raw("PRAGMA table_info(scenarios)").Scan(&columns).Error; err != nil {
				return err
			}
			keys, packageKey, idKey, required := 0, false, false, false
			for _, column := range columns {
				if column.PK > 0 {
					keys++
					packageKey = packageKey || column.Name == "package_id"
					idKey = idKey || column.Name == "id"
				}
				if column.Name == "package_id" {
					required = column.Notnull == 1
				}
			}
			if keys != 2 || !packageKey || !idKey || !required {
				if err := tx.Migrator().DropTable(&models.Scenario{}); err != nil {
					return err
				}
			}
		}
		return tx.AutoMigrate(&models.Scenario{})
	})
}
