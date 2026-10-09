package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// plan is everything preflight established about the target package.
type plan struct {
	pkg         labs.Package
	manifest    *manifest.Manifest
	composeFile string
	routes      []byte // the package's nginx.routes.conf
	policy      []byte // the package's default containd policy
	firewallAPI string
}

// transition switches the range to pkg. previous is the status before the
// request, which a failed preflight returns to.
func (m *Manager) transition(pkg labs.Package, previous Status) {
	ctx := context.Background()
	p, err := m.preflight(ctx, pkg)
	if err != nil {
		log.Printf("range: preflight of %s failed: %v", pkg.ID, err)
		now := m.opts.Now()
		m.mu.Lock()
		m.status = previous
		m.status.Error = fmt.Sprintf("preflight %s: %v", pkg.ID, err)
		m.status.UpdatedAt = &now
		m.mu.Unlock()
		return
	}
	gen, err := m.switchTo(ctx, p)
	if err != nil {
		m.fail(p, gen, err)
	}
}

// preflight checks the target without touching the running range.
func (m *Manager) preflight(ctx context.Context, pkg labs.Package) (plan, error) {
	man, err := manifest.Load(m.opts.Root, pkg.ID)
	if err != nil {
		return plan{}, err
	}
	dir := manifest.Dir(m.opts.Root, pkg.ID)
	p := plan{pkg: pkg, manifest: man, composeFile: filepath.Join(dir, manifest.ComposeFile(m.opts.Mode))}

	configCtx, cancel := context.WithTimeout(ctx, m.timeouts.Config)
	defer cancel()
	normalized, err := m.opts.Compose.Config(configCtx, p.composeFile)
	if err != nil {
		return plan{}, err
	}
	if err := checkPackage(man, pkg, m.opts.Mode, m.opts.Root, normalized); err != nil {
		return plan{}, err
	}
	images, err := m.opts.Compose.Images(configCtx, p.composeFile)
	if err != nil {
		return plan{}, err
	}
	missing, err := m.opts.Engine.MissingImages(ctx, images)
	if err != nil {
		return plan{}, err
	}
	if len(missing) > 0 {
		return plan{}, fmt.Errorf("%d image(s) missing for %s mode: %s", len(missing), m.opts.Mode, strings.Join(missing, ", "))
	}

	if p.routes, err = os.ReadFile(filepath.Join(dir, manifest.RoutesFile)); err != nil {
		return plan{}, fmt.Errorf("proxy routes: %w", err)
	}
	api, ok := man.Endpoint(manifest.RoleFirewall, "api")
	if !ok {
		return plan{}, errors.New("manifest has no firewall service with an api endpoint")
	}
	p.firewallAPI = api
	if pkg.FirewallConfigPath == "" {
		return plan{}, errors.New("package declares no default firewall policy (topology firewall_config)")
	}
	if p.policy, err = os.ReadFile(filepath.Join(m.opts.DefinitionsDir, pkg.FirewallConfigPath)); err != nil {
		return plan{}, fmt.Errorf("default firewall policy: %w", err)
	}
	if !json.Valid(p.policy) {
		return plan{}, fmt.Errorf("default firewall policy %s is not valid JSON", pkg.FirewallConfigPath)
	}
	return p, nil
}

// checkPackage is the package-lint preflight step: the manifest on its own,
// against the curriculum topology, and against the normalized Compose model
// of the mode. CheckPlatform stays with the package lint: the platform is
// already running when a range starts.
func checkPackage(man *manifest.Manifest, pkg labs.Package, mode manifest.Mode, root string, normalized []byte) error {
	if err := manifest.Validate(man, root); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if err := manifest.CheckTopology(man, pkg); err != nil {
		return fmt.Errorf("manifest against topology: %w", err)
	}
	if err := manifest.CheckCompose(man, mode, root, normalized); err != nil {
		return fmt.Errorf("manifest against compose %s model: %w", mode, err)
	}
	return nil
}

// switchTo runs steps 2-5. It returns the new generation once one exists,
// so a failure can stop it.
func (m *Manager) switchTo(ctx context.Context, p plan) (*Generation, error) {
	rec := m.record()
	rec.Generation++
	rec.Package = p.pkg.ID
	rec.Revision = p.pkg.Revision
	rec.Mode = m.opts.Mode
	rec.Root = m.opts.Root
	rec.Version = m.opts.Version
	rec.Error = ""

	// stopping: from here on no request enters the old generation.
	m.mu.Lock()
	old := m.current
	m.current = nil
	m.mu.Unlock()
	rec.Phase = PhaseStopping
	log.Printf("range: generation %d: stopping for %s", rec.Generation, p.pkg.ID)
	if err := m.persist(rec); err != nil {
		return nil, fmt.Errorf("stopping: %w", err)
	}
	if old != nil {
		if err := old.Stop(m.timeouts.Drain); err != nil {
			return nil, fmt.Errorf("stopping: %w", err)
		}
	}
	if err := m.teardown(ctx); err != nil {
		return nil, fmt.Errorf("stopping: %w", err)
	}
	held, err := m.opts.Engine.ContainersNamed(ctx, containerNames(p.manifest))
	if err != nil {
		return nil, fmt.Errorf("stopping: %w", err)
	}
	if len(held) > 0 {
		return nil, fmt.Errorf("stopping: container names of %s are held outside project %s: %s", p.pkg.ID, manifest.RangeProject, strings.Join(held, ", "))
	}

	rec.Phase = PhaseStarting
	log.Printf("range: generation %d: starting %s", rec.Generation, p.pkg.ID)
	if err := m.persist(rec); err != nil {
		return nil, fmt.Errorf("starting: %w", err)
	}
	upCtx, cancel := context.WithTimeout(ctx, m.timeouts.Up)
	err = m.opts.Compose.Up(upCtx, p.composeFile)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("starting: %w", err)
	}

	rec.Phase = PhaseConfiguring
	log.Printf("range: generation %d: configuring %s", rec.Generation, p.pkg.ID)
	if err := m.persist(rec); err != nil {
		return nil, fmt.Errorf("configuring: %w", err)
	}
	if err := m.installRoutes(ctx, p.routes); err != nil {
		return nil, fmt.Errorf("configuring: %w", err)
	}
	gen := NewGeneration(rec.Generation, p.pkg, p.manifest, m.opts.NewContaind(p.firewallAPI))
	if err := m.seedPolicy(ctx, gen.Containd(), p); err != nil {
		return gen, fmt.Errorf("configuring: %w", err)
	}
	m.opts.Activate(gen)

	rec.Phase = PhaseReady
	if err := m.persist(rec); err != nil {
		return gen, fmt.Errorf("ready: %w", err)
	}
	m.mu.Lock()
	m.current = gen
	m.mu.Unlock()
	log.Printf("range: generation %d: %s ready", rec.Generation, p.pkg.ID)
	return gen, nil
}

// fail handles a failure in steps 2-5: stop the new generation, remove
// whatever the attempt left with a fresh bounded context, and record the
// failure. The previous package is not restored.
func (m *Manager) fail(p plan, gen *Generation, cause error) {
	log.Printf("range: switch to %s failed: %v", p.pkg.ID, cause)
	errs := []error{cause}
	if gen != nil {
		if err := gen.Stop(m.timeouts.Drain); err != nil {
			errs = append(errs, err)
		}
	}
	if err := m.teardown(context.Background()); err != nil {
		errs = append(errs, fmt.Errorf("teardown after failure: %w", err))
	}
	rec := m.record()
	rec.Phase = PhaseFailed
	rec.Error = errors.Join(errs...).Error()
	if err := m.persist(rec); err != nil {
		log.Printf("range: record failure: %v", err)
	}
}

// teardown removes the range project by label and verifies that nothing
// with its label, and none of the volumes its containers mounted, is left.
func (m *Manager) teardown(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, m.timeouts.Down)
	defer cancel()
	volumes, err := m.opts.Engine.MountedVolumes(ctx, manifest.RangeProject)
	if err != nil {
		return err
	}
	if err := m.opts.Compose.Down(ctx); err != nil {
		return err
	}
	leftVolumes, err := m.opts.Engine.ExistingVolumes(ctx, volumes)
	if err != nil {
		return err
	}
	if len(leftVolumes) > 0 {
		return fmt.Errorf("project %s volumes [%s] remain after down", manifest.RangeProject, strings.Join(leftVolumes, ", "))
	}
	containers, networks, err := m.opts.Engine.ProjectResources(ctx, manifest.RangeProject)
	if err != nil {
		return err
	}
	if len(containers) > 0 || len(networks) > 0 {
		return fmt.Errorf("project %s still has containers [%s] and networks [%s] after down",
			manifest.RangeProject, strings.Join(containers, ", "), strings.Join(networks, ", "))
	}
	return nil
}

// seedPolicy waits for the firewall API and imports the package's default
// policy, as every range start does.
func (m *Manager) seedPolicy(ctx context.Context, client *containd.Client, p plan) error {
	if err := client.WaitReady(ctx, m.timeouts.ContaindReady); err != nil {
		return err
	}
	warnings, err := client.ImportConfig(ctx, p.policy)
	if err != nil {
		return fmt.Errorf("import default policy %s: %w", p.pkg.FirewallConfigPath, err)
	}
	for _, warning := range warnings {
		log.Printf("range: containd commit warning for %s: %s", p.pkg.FirewallConfigPath, warning)
	}
	return nil
}

func containerNames(man *manifest.Manifest) []string {
	names := make([]string, 0, len(man.Services))
	for _, svc := range man.Services {
		names = append(names, svc.Container)
	}
	return names
}
