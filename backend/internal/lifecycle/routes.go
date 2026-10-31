package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// installRoutes makes content the proxy's range routes: write it beside
// the active file, rename it into place, then nginx -t and reload in the
// proxy. If either nginx step fails the previous file comes back, so the
// directory keeps matching what nginx runs.
func (m *Manager) installRoutes(ctx context.Context, content []byte) error {
	target := filepath.Join(m.opts.RoutesDir, RoutesFile)
	previous, err := os.ReadFile(target)
	hadPrevious := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read installed routes: %w", err)
	}
	if err := m.replaceRoutes(content); err != nil {
		return err
	}
	for _, argv := range [][]string{{"nginx", "-t"}, {"nginx", "-s", "reload"}} {
		if err := m.proxyExec(ctx, argv); err != nil {
			if restoreErr := m.restoreRoutes(previous, hadPrevious); restoreErr != nil {
				return errors.Join(err, restoreErr)
			}
			return err
		}
	}
	return nil
}

func (m *Manager) replaceRoutes(content []byte) error {
	target := filepath.Join(m.opts.RoutesDir, RoutesFile)
	tmp := filepath.Join(m.opts.RoutesDir, "."+RoutesFile+".tmp")
	if err := writeSynced(tmp, content); err != nil {
		return fmt.Errorf("write routes: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("install routes: %w", err)
	}
	return nil
}

func (m *Manager) restoreRoutes(previous []byte, hadPrevious bool) error {
	if hadPrevious {
		if err := m.replaceRoutes(previous); err != nil {
			return fmt.Errorf("restore previous routes: %w", err)
		}
		return nil
	}
	if err := os.Remove(filepath.Join(m.opts.RoutesDir, RoutesFile)); err != nil {
		return fmt.Errorf("remove rejected routes: %w", err)
	}
	return nil
}

func (m *Manager) proxyExec(ctx context.Context, argv []string) error {
	code, output, err := m.opts.Engine.Exec(ctx, m.opts.ProxyContainer, argv)
	if err != nil {
		return fmt.Errorf("%v in %s: %w", argv, m.opts.ProxyContainer, err)
	}
	if code != 0 {
		return fmt.Errorf("%v in %s exited %d: %s", argv, m.opts.ProxyContainer, code, output)
	}
	return nil
}

// routesInstalled reports whether the proxy's active routes are content.
func (m *Manager) routesInstalled(content []byte) (bool, error) {
	installed, err := os.ReadFile(filepath.Join(m.opts.RoutesDir, RoutesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return string(installed) == string(content), nil
}
