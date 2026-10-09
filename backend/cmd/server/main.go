package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/tturner/rangerdanger/backend/internal/config"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
	"github.com/tturner/rangerdanger/backend/internal/server"
	"github.com/tturner/rangerdanger/backend/internal/version"
)

// Platform paths and names fixed by the platform Compose files.
const (
	// proxyRoutesDir is the backend's bind of data/proxy-routes, which the
	// proxy mounts read-only as its included routes directory.
	proxyRoutesDir = "/proxy-routes"
	proxyContainer = "rangerdanger-proxy"
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

	orch := orchestrator.New()
	dockerClient := orch.DockerClient()
	if dockerClient == nil {
		log.Fatalf("docker client unavailable: the range lifecycle needs the Docker socket")
	}
	// Compose runs from the working directory, which holds no Compose file
	// (see lifecycle.ComposeCLI.Dir).
	workdir, err := os.Getwd()
	if err != nil {
		log.Fatalf("working directory: %v", err)
	}

	srv, err := server.New(cfg, database, loader, catalog, orch, lifecycle.Options{
		Root:           cfg.Root,
		Mode:           cfg.Mode,
		DefinitionsDir: cfg.LabDefinitionsPath,
		RoutesDir:      proxyRoutesDir,
		ProxyContainer: proxyContainer,
		DefaultPackage: cfg.Package,
		Version:        version.Version,
		Store:          lifecycle.RecordStore{Path: filepath.Join(filepath.Dir(cfg.DBPath), lifecycle.RecordFile)},
		Compose: &lifecycle.ComposeCLI{
			Binary:  lifecycle.ComposeBinary,
			Project: manifest.RangeProject,
			Root:    cfg.Root,
			Dir:     workdir,
			Env:     lifecycle.ComposeEnv(containd.JWTSecret()),
		},
		Engine:      lifecycle.NewDockerEngine(dockerClient, orch.ExecCommand),
		NewContaind: containd.NewClient,
	})
	if err != nil {
		log.Fatalf("build server: %v", err)
	}

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
