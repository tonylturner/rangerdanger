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

	// A broken package or an unknown RANGERDANGER_PACKAGE stops startup:
	// serving a partial curriculum would hide the error from instructors.
	loader := labs.NewLoader(cfg.LabDefinitionsPath, server.ValidatorRequirements())
	catalog, err := loader.Seed(ctx, database, cfg.Package)
	if err != nil {
		log.Fatalf("seed lab packages: %v", err)
	}

	// Seed containd config in background (containd may take time to start)
	containdClient := containd.NewClient(cfg.ContaindAPIURL)
	go containd.SeedConfigIfNeeded(containdClient, cfg.ContaindConfigPath)

	orch := orchestrator.New(containdClient, cfg.LabDefinitionsPath)

	srv := server.New(cfg, database, loader, catalog, orch, containdClient)

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
