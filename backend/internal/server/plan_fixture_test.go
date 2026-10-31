package server

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// Like -update-eu-flow-graph, -update-plan-fixtures regenerates frontend
// goldens from the served response; normal runs compare the formatted bytes.
var updatePlanFixtures = flag.Bool("update-plan-fixtures", false, "regenerate frontend compiled-plan fixtures")

func TestPlanFixtures(t *testing.T) {
	for _, tc := range []struct {
		name string
		gen  *lifecycle.Generation
	}{
		{"us", usGeneration(nil)},
		{"eu", euGeneration(t, nil, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Compilation is offline: no containd client or fake is needed.
			s := newTestServer(t, "")
			s.rng = rangeServer(tc.gen, "weak").rng
			all := []string{}
			for _, action := range tc.gen.Package.Curriculum.Catalog {
				all = append(all, action.ID)
			}
			for _, selection := range []struct {
				name string
				ids  []string
			}{
				{"empty", []string{}},
				{"partial", []string{"restrict-enterprise-to-vendor"}},
				{"full", all},
			} {
				t.Run(selection.name, func(t *testing.T) {
					rec, _ := invoke(s, s.handlePlanCompile, http.MethodPost, "/api/plan/compile", planRequest(s, selection.ids...))
					if rec.Code != http.StatusOK {
						t.Fatalf("compile = %d %s", rec.Code, rec.Body)
					}
					var formatted bytes.Buffer
					if err := json.Indent(&formatted, rec.Body.Bytes(), "", "  "); err != nil {
						t.Fatal(err)
					}
					formatted.WriteByte('\n')
					path := filepath.Join(repoRoot(), "frontend", "lib", "testdata", "plans", tc.name+"-"+selection.name+".json")
					if *updatePlanFixtures {
						if err := os.WriteFile(path, formatted.Bytes(), 0644); err != nil {
							t.Fatal(err)
						}
					}
					want, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(want, formatted.Bytes()) {
						t.Fatalf("%s differs from compile response; regenerate with go -C backend test ./internal/server -run '^TestPlanFixtures$' -update-plan-fixtures", path)
					}
				})
			}
		})
	}
}
