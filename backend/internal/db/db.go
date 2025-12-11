package db

import (
    "fmt"
    "os"
    "path/filepath"

    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
)

// Connect opens a SQLite database, creating directories if needed.
func Connect(path string) (*gorm.DB, error) {
    dir := filepath.Dir(path)
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return nil, fmt.Errorf("create db dir: %w", err)
    }

    database, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
    if err != nil {
        return nil, fmt.Errorf("open db: %w", err)
    }

    return database, nil
}
