package labs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

// Include the compiler model as well as the public DTO (which omits Plan).
func TestCurriculumResolvedModelFingerprint(t *testing.T) {
	// Captured from the resolved monolithic curricula before the directory cutover.
	// Snapshots include intentional catalog/first-match prose, presentation-data
	// removal, readiness-driven budgets, per-command source headers, and EU
	// probes without host discovery, commit-time established-flow re-evaluation,
	// containd 0.1.39 configuration-CLI and review navigation, and breaker
	// lockout telemetry in the EU acceptance inventory, and the EU portal's
	// valid/quality-flag wording (without changing protocol acceptance), and
	// catalog titles instead of action IDs in implementation prose.
	for id, want := range map[string]string{
		"us-dnp3-substation":   "d293e78dba6f52c4bc85e518c237d27950ffbb188031b03a3329835c8b98e6e4",
		"eu-iec104-substation": "05023057ced0dc60058c46ed657b824c2fc6201f7539dee6519cadce693c2944",
	} {
		root := filepath.Join("..", "..", "..", "lab-definitions")
		pkg, err := ReadDefinition(root, filepath.Join(root, "packages", id, "package.yml"))
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(struct {
			Model *Curriculum
			Plan  *CurriculumPlan
		}{pkg.Curriculum, pkg.Curriculum.Plan})
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			t.Errorf("%s resolved model fingerprint = %s, want %s", id, got, want)
		}
	}
}
