package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
	"gopkg.in/yaml.v3"
)

func TestDiscoverPackagesValidatesScenarioAndCurriculum(t *testing.T) {
	for _, tc := range []struct {
		name, file, old, replacement, want string
	}{
		{"valid", "", "", "", ""},
		{"scenario", "scenarios/baseline-assessment.yml", "validator: us-baseline-assessment", "validator: not-registered", "unknown validator"},
		{"curriculum", "packages/us-dnp3-substation/curriculum/unknown.yml", "", "schema: 1\n", "unknown curriculum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			definitions := filepath.Join(root, "lab-definitions")
			if err := os.CopyFS(definitions, os.DirFS("../../../lab-definitions")); err != nil {
				t.Fatal(err)
			}
			if tc.file != "" {
				path := filepath.Join(definitions, tc.file)
				data := ""
				if tc.old != "" {
					bytes, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					data = string(bytes)
					if !strings.Contains(data, tc.old) {
						t.Fatalf("fixture has no %q", tc.old)
					}
					data = strings.Replace(data, tc.old, tc.replacement, 1)
				} else {
					data = tc.replacement
				}
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			packages, err := discoverPackages(root)
			if tc.want != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
					t.Fatalf("error = %v, want %q", err, tc.want)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want := map[string]int{"eu-iec104-substation": 7, "us-dnp3-substation": 7, "us-sra-substation": 1}
				if len(packages) != len(want) {
					t.Fatalf("packages = %d, want %d", len(packages), len(want))
				}
				for _, pkg := range packages {
					count, exists := want[pkg.id]
					if !exists || len(pkg.topology.Scenarios) != count {
						t.Fatalf("package %s scenarios = %d, want %d", pkg.id, len(pkg.topology.Scenarios), count)
					}
				}
			}
		})
	}
}

// These checks use authored declarations, not a substitute Compose normalizer.
// Docker-backed CheckCompose and nginx validation remain the CLI's job.
func TestShippedPackageDeclarations(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	packages, err := discoverPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	builds := make(map[string][]byte)
	for _, pkg := range packages {
		t.Run(pkg.id, func(t *testing.T) {
			if err := manifest.Validate(pkg.manifest, root); err != nil {
				t.Fatal(err)
			}
			if err := manifest.CheckTopology(pkg.manifest, pkg.topology); err != nil {
				t.Fatal(err)
			}
			policies, err := (&lintRun{root: root}).readPolicies(&pkg)
			if err != nil {
				t.Fatal(err)
			}
			for _, mode := range []manifest.Mode{manifest.ModeSource, manifest.ModeRelease} {
				path := filepath.Join(pkg.dir, manifest.ComposeFile(mode))
				if err := manifest.CheckRawCompose(path); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var doc struct {
					Name     string                    `yaml:"name"`
					Services map[string]map[string]any `yaml:"services"`
				}
				if err := yaml.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				// CheckFirewallInterfaces consumes only firewall attachments.
				nics, err := json.Marshal(map[string]any{
					"name": doc.Name, "services": map[string]any{
						"firewall": map[string]any{"networks": doc.Services["firewall"]["networks"]},
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := manifest.CheckFirewallInterfaces(pkg.manifest, pkg.topology, policies, nics); err != nil {
					t.Fatal(err)
				}
				if mode == manifest.ModeSource {
					// Project only the fields consumed by CheckCrossPackage.
					projected := make(map[string]any)
					for key, service := range doc.Services {
						projected[key] = map[string]any{"image": service["image"]}
						if build, exists := service["build"]; exists {
							projected[key].(map[string]any)["build"] = build
						}
					}
					builds[pkg.id], err = json.Marshal(map[string]any{"name": doc.Name, "services": projected})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
	for a := 0; a < len(packages); a++ {
		for b := a + 1; b < len(packages); b++ {
			left, right := packages[a].id, packages[b].id
			if err := manifest.CheckCrossPackage(left, builds[left], right, builds[right]); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSRAPoliciesPreserveManagementAndRemoveDirectVendorAllows(t *testing.T) {
	for _, name := range []string{"weak", "improved"} {
		t.Run(name, func(t *testing.T) {
			read := func(path string) map[string]any {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var doc map[string]any
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				return doc
			}
			base := read("../../../lab-definitions/firewall/substation-" + name + ".json")
			sra := read("../../../lab-definitions/packages/us-sra-substation/firewall/" + name + ".json")
			ifaces := sra["interfaces"].([]any)
			if len(ifaces) != 6 || !reflect.DeepEqual(ifaces[:5], base["interfaces"]) {
				t.Fatal("SRA must preserve the five US interfaces including reserved management")
			}
			want := map[string]any{"name": "lan4", "zone": "lan4", "access": map[string]any{
				"mgmt": false, "http": false, "https": false, "ssh": false,
			}}
			if !reflect.DeepEqual(ifaces[5], want) {
				t.Fatalf("lan4 = %#v", ifaces[5])
			}
			rules := sra["firewall"].(map[string]any)["rules"].([]any)
			byID := make(map[string]any)
			for _, value := range rules {
				rule := value.(map[string]any)
				byID[rule["id"].(string)] = rule
				if name == "improved" && rule["action"] == "ALLOW" &&
					reflect.DeepEqual(rule["sourceZones"], []any{"dmz"}) &&
					(reflect.DeepEqual(rule["destZones"], []any{"lan1"}) ||
						reflect.DeepEqual(rule["destZones"], []any{"lan2"})) {
					t.Fatal("improved admits direct vendor access to OT")
				}
			}
			for _, value := range base["firewall"].(map[string]any)["rules"].([]any) {
				rule := value.(map[string]any)
				id := rule["id"].(string)
				if name == "improved" && (id == "vendor-to-ot-restricted" ||
					id == "deny-vendor-to-ot" || id == "deny-vendor-to-field") {
					continue
				}
				if !reflect.DeepEqual(byID[id], rule) {
					t.Fatalf("US conduit %q changed", id)
				}
			}
		})
	}
}
