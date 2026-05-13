package containd

import (
	"context"
	"log"
	"os"
	"time"
)

// SeedConfigIfNeeded reads a containd config JSON file and imports it via the API.
// It waits for containd to become ready before importing.
func SeedConfigIfNeeded(client *Client, configPath string) {
	if configPath == "" {
		log.Println("containd seed: no config path set, skipping")
		return
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("containd seed: cannot read %s: %v", configPath, err)
		return
	}

	ctx := context.Background()
	log.Printf("containd seed: waiting for containd to be ready...")

	if err := client.WaitReady(ctx, 60*time.Second); err != nil {
		log.Printf("containd seed: %v", err)
		return
	}

	warnings, err := client.ImportConfig(data)
	if err != nil {
		log.Printf("containd seed: import failed: %v", err)
		return
	}
	for _, w := range warnings {
		log.Printf("containd seed: WARNING: %s", w)
	}

	log.Printf("containd seed: config imported from %s", configPath)
}
