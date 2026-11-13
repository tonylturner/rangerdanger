package lifecycle

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestInstalledPackageAdmissionAndHostAdd(t *testing.T) {
	h := newHarness(t)
	opts := h.options()
	m := h.manager(opts)
	file := filepath.Join(h.root, "data", "installed-packages")
	mustWrite(t, file, []byte("rangerdanger-installed-packages-v1\npkg-a\n"))
	before := m.Status()
	if _, err := m.Request("pkg-b"); !errors.Is(err, ErrPackageNotInstalled) {
		t.Fatalf("uninstalled request error = %v", err)
	}
	if got := m.Status(); got.Phase != before.Phase || got.Target != "" || len(h.compose.callLog()) != 0 {
		t.Fatalf("uninstalled request changed range: %+v, calls %v", got, h.compose.callLog())
	}
	if _, err := m.Request("unknown"); !errors.Is(err, ErrUnknownPackage) {
		t.Fatalf("unknown request = %v", err)
	}
	mustWrite(t, file, []byte("rangerdanger-installed-packages-v1\npkg-a\npkg-b\n"))
	if _, err := m.Request("pkg-b"); err != nil {
		t.Fatal(err)
	}
	m.wait()
	if got := m.Status(); got.Phase != PhaseReady || got.Package != "pkg-b" {
		t.Fatalf("host add was not reloaded: %+v", got)
	}
}

func TestStartupRejectsUninstalledDefaultAndRuntime(t *testing.T) {
	h := newHarness(t)
	opts := h.options()
	mustWrite(t, filepath.Join(h.root, "data", "installed-packages"), []byte("rangerdanger-installed-packages-v1\npkg-b\n"))
	if _, err := New(opts); !errors.Is(err, ErrPackageNotInstalled) {
		t.Fatalf("uninstalled default accepted: %v", err)
	}
	opts = h.options()
	mustWrite(t, filepath.Join(h.root, "data", "installed-packages"), []byte("rangerdanger-installed-packages-v1\npkg-a\npkg-b\n"))
	h.ready(opts, "pkg-b")
	mustWrite(t, filepath.Join(h.root, "data", "installed-packages"), []byte("rangerdanger-installed-packages-v1\npkg-a\n"))
	if _, err := New(opts); !errors.Is(err, ErrPackageNotInstalled) {
		t.Fatalf("uninstalled runtime accepted: %v", err)
	}
}

func TestReconcileRejectsUninstalledRuntimeWithoutTeardown(t *testing.T) {
	h := newHarness(t)
	opts := h.options()
	h.ready(opts, "pkg-b")
	m := h.manager(opts)
	mustWrite(t, filepath.Join(h.root, "data", "installed-packages"), []byte("rangerdanger-installed-packages-v1\npkg-a\n"))
	m.Start()
	m.wait()
	if got := m.Status(); got.Phase != PhaseFailed || got.Error == "" {
		t.Fatalf("reconcile admitted uninstalled runtime: %+v", got)
	}
	if got := h.compose.callLog(); len(got) != 0 {
		t.Fatalf("reconcile touched running range: %v", got)
	}
	select {
	case <-h.activated:
		t.Fatal("reconcile activated an uninstalled generation")
	default:
	}
}
