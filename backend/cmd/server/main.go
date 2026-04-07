package main

import (
    "context"
    "log"

    "github.com/tturner/rangerdanger/backend/internal/config"
    "github.com/tturner/rangerdanger/backend/internal/containd"
    "github.com/tturner/rangerdanger/backend/internal/db"
    "github.com/tturner/rangerdanger/backend/internal/labs"
    "github.com/tturner/rangerdanger/backend/internal/orchestrator"
    "github.com/tturner/rangerdanger/backend/internal/server"
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

    // Seed containd config in background (containd may take time to start)
    containdClient := containd.NewClient(cfg.ContaindAPIURL)
    go containd.SeedConfigIfNeeded(containdClient, cfg.ContaindConfigPath)

    orch := orchestrator.New(containdClient, cfg.LabDefinitionsPath)

    // Provision container gateways to route all traffic through containd
    go orch.ProvisionGateways(ctx)

    srv := server.New(cfg, database, loader, orch, containdClient)

    if err := srv.Run(ctx); err != nil {
        log.Fatalf("run server: %v", err)
    }
}
