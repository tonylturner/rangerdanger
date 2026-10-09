package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// reconcile settles the record the previous backend process left.
func (m *Manager) reconcile() {
	m.mu.Lock()
	loadErr := m.loadErr
	var rec Record
	if m.rec != nil {
		rec = *m.rec
	}
	m.mu.Unlock()

	switch {
	case loadErr != nil:
		m.recoverInterrupted(Record{Mode: m.opts.Mode, Root: m.opts.Root, Version: m.opts.Version},
			fmt.Sprintf("runtime record unreadable (%v)", loadErr))
	case rec.Phase == PhaseReady:
		if err := m.reconcileReady(rec); err != nil {
			log.Printf("range: reconcile of %s failed: %v", rec.Package, err)
			rec.Phase = PhaseFailed
			rec.Error = "range missing: " + err.Error()
			if err := m.persist(rec); err != nil {
				log.Printf("range: record failure: %v", err)
			}
		}
	default:
		m.recoverInterrupted(rec, "interrupted during "+string(rec.Phase))
	}
}

// recoverInterrupted removes whatever a transition cut short left behind
// and records the failure.
func (m *Manager) recoverInterrupted(rec Record, reason string) {
	log.Printf("range: %s; tearing the range down", reason)
	errs := []error{errors.New(reason)}
	if err := m.teardown(context.Background()); err != nil {
		errs = append(errs, fmt.Errorf("teardown: %w", err))
	}
	rec.Phase = PhaseFailed
	rec.Error = errors.Join(errs...).Error()
	if err := m.persist(rec); err != nil {
		log.Printf("range: record failure: %v", err)
	}
}

// reconcileReady adopts a range that was ready when the backend stopped:
// every manifest container must exist with the range project label (not
// necessarily healthy yet: after a host reboot the range restarts beside
// the backend) and the proxy must run this package's routes. It neither
// starts the range nor reseeds the policy.
func (m *Manager) reconcileReady(rec Record) error {
	if rec.Root != m.opts.Root || rec.Mode != m.opts.Mode {
		return fmt.Errorf("started from root %s in %s mode, backend now runs root %s in %s mode", rec.Root, rec.Mode, m.opts.Root, m.opts.Mode)
	}
	pkg, ok := findPackage(m.opts.Catalog(), rec.Package)
	if !ok {
		return fmt.Errorf("package %s is not in the catalog", rec.Package)
	}
	man, err := manifest.Load(m.opts.Root, pkg.ID)
	if err != nil {
		return err
	}
	api, ok := man.Endpoint(manifest.RoleFirewall, "api")
	if !ok {
		return errors.New("manifest has no firewall service with an api endpoint")
	}

	ctx := context.Background()
	present, _, err := m.opts.Engine.ProjectResources(ctx, manifest.RangeProject)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, name := range present {
		have[name] = true
	}
	var absent []string
	for _, name := range containerNames(man) {
		if !have[name] {
			absent = append(absent, name)
		}
	}
	if len(absent) > 0 {
		return fmt.Errorf("containers absent from project %s: %s", manifest.RangeProject, strings.Join(absent, ", "))
	}

	routes, err := os.ReadFile(filepath.Join(manifest.Dir(m.opts.Root, pkg.ID), manifest.RoutesFile))
	if err != nil {
		return fmt.Errorf("package routes: %w", err)
	}
	installed, err := m.routesInstalled(routes)
	if err != nil {
		return fmt.Errorf("installed routes: %w", err)
	}
	if !installed {
		return fmt.Errorf("proxy routes are not the routes of %s", pkg.ID)
	}

	gen := NewGeneration(rec.Generation, pkg, man, m.opts.NewContaind(api))
	m.opts.Activate(gen)
	m.mu.Lock()
	m.current = gen
	m.mu.Unlock()
	log.Printf("range: generation %d: %s reconciled", rec.Generation, pkg.ID)
	return nil
}
