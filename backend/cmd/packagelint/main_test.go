package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			} else if err != nil || len(packages) != 2 || len(packages[0].topology.Scenarios) != 7 {
				t.Fatalf("packages = %d, error = %v", len(packages), err)
			}
		})
	}
}
