package main

import (
    "context"
    "log"

    "github.com/tturner/rangerrocks/backend/internal/config"
    "github.com/tturner/rangerrocks/backend/internal/db"
    "github.com/tturner/rangerrocks/backend/internal/labs"
    "github.com/tturner/rangerrocks/backend/internal/orchestrator"
    "github.com/tturner/rangerrocks/backend/internal/server"
)

func main() {
    ctx := context.Background()

    cfg, err := config.Load()
    if err != nil {
        log.Fatalf("load config: %v", err)
    }

    database, err := db.Connect(cfg.DBPath)
    if err != nil {
        log.Fatalf("connect database: %v", err)
    }

    loader := labs.NewLoader(cfg.LabDefinitionsPath)
    if err := loader.SeedFromDisk(ctx, database); err != nil {
        log.Printf("warning: seed lab definitions failed: %v", err)
    }

    orchestrator := orchestrator.New()
    srv := server.New(cfg, database, loader, orchestrator)

    if err := srv.Run(ctx); err != nil {
        log.Fatalf("run server: %v", err)
    }
}
