package lifecycle

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// RestartServices restarts services of the admitted generation using its
// package model and the installation's configured mode. Range-bound callers
// hold the generation's drain registration for the duration of this call.
func (m *Manager) RestartServices(ctx context.Context, gen *Generation, services []string) error {
	m.mu.Lock()
	current := gen != nil && m.current == gen
	m.mu.Unlock()
	if !current {
		return fmt.Errorf("restart generation is not serving")
	}
	if len(services) == 0 {
		return fmt.Errorf("restart requires named services")
	}
	for _, key := range services {
		found := false
		for _, service := range gen.Manifest.Services {
			found = found || service.Key == key
		}
		if !found {
			return fmt.Errorf("restart service %s is not part of package %s", key, gen.Package.ID)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	file := filepath.Join(manifest.Dir(m.opts.Root, gen.Package.ID), manifest.ComposeFile(m.opts.Mode))
	return m.opts.Compose.Restart(ctx, file, services)
}
