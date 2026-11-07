package server

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestReportCapturePathsMatchPackageComposeMounts(t *testing.T) {
	for id, recipe := range map[string]*packageRecipe{usPackageID: usRecipe, euPackageID: euRecipe} {
		pkg, err := labs.ReadDefinition(filepath.Join(repoRoot(), "lab-definitions"), filepath.Join(repoRoot(), "lab-definitions", "packages", id, "package.yml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"source", "release"} {
			t.Run(id+"/"+mode, func(t *testing.T) {
				data, err := os.ReadFile(filepath.Join(repoRoot(), "lab-definitions", "packages", id, "compose."+mode+".yml"))
				if err != nil {
					t.Fatal(err)
				}
				var compose struct {
					Services map[string]struct {
						Volumes []string `yaml:"volumes"`
					} `yaml:"services"`
				}
				if err := yaml.Unmarshal(data, &compose); err != nil {
					t.Fatal(err)
				}
				matches := 0
				for _, volume := range compose.Services["firewall"].Volumes {
					parts := strings.Split(volume, ":")
					if len(parts) < 2 {
						t.Fatalf("expected a bind mount, got %q", volume)
					}
					relative, ok := strings.CutPrefix(recipe.report.captureDir, path.Clean(parts[1])+"/")
					if !ok {
						continue
					}
					matches++
					want := path.Join(parts[0], relative)
					if recipe.report.hostCaptureDir != want {
						t.Errorf("host capture directory = %q; compose mount %q requires %q",
							recipe.report.hostCaptureDir, volume, want)
					}
					pcap := path.Join(want, "validation-test.pcap")
					md := recipe.renderValidationMarkdown(pkg, "improved", "improved", "hash", pcap, true, "capture",
						nil, 0, 0, 0, 0, 0, false)
					if !strings.Contains(md, "| **PCAP evidence** | `"+pcap+"` |") {
						t.Errorf("report does not publish compose-derived PCAP path %q", pcap)
					}
				}
				if matches != 1 {
					t.Fatalf("capture directory %q matches %d firewall mounts, want one", recipe.report.captureDir, matches)
				}
			})
		}
	}
}
