package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

func TestRestartServicesUsesServingPackageAndMode(t *testing.T) {
	for _, mode := range []manifest.Mode{manifest.ModeSource, manifest.ModeRelease} {
		t.Run(string(mode), func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "argv")
			cli := &ComposeCLI{Binary: fakeComposeBinary(t, `printf '%s\n' "$@" > "`+out+`"`), Dir: t.TempDir(), Root: "/H", Project: "rangerdanger"}
			gen := &Generation{Package: labs.Package{ID: "pkg"}, Manifest: &manifest.Manifest{Services: []manifest.Service{{Key: "broker"}}}}
			m := &Manager{opts: Options{Root: "/H", Mode: mode, Compose: cli}, current: gen}
			for _, services := range [][]string{nil, {"foreign"}, {"broker", "foreign"}} {
				if err := m.RestartServices(t.Context(), gen, services); err == nil {
					t.Fatalf("accepted %v", services)
				}
			}
			if err := m.RestartServices(t.Context(), &Generation{}, []string{"broker"}); err == nil {
				t.Fatal("accepted foreign generation")
			}
			if err := m.RestartServices(t.Context(), nil, []string{"broker"}); err == nil {
				t.Fatal("accepted missing generation")
			}
			if err := m.RestartServices(t.Context(), gen, []string{"broker"}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "-p\nrangerdanger\n--project-directory\n/H\n-f\n/H/lab-definitions/packages/pkg/compose."+string(mode)+".yml\nrestart\n--no-deps\nbroker\n" {
				t.Fatalf("wrong restart argv: %s", data)
			}
		})
	}
}
