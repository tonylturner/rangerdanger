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
	// catalog titles instead of action IDs in implementation prose, and the
	// single-path rule-authoring / optional catch-up directive cutover, and
	// hand-authored change-request implementation with single-commit cutover.
	for id, want := range map[string]string{
		"us-dnp3-substation":   "9a29248dc353efe2fee02efc2f55eeca22b48ceea6aedcdf770efba095712594",
		"eu-iec104-substation": "e04c121adf586f46ee1e7d4bde7571c611edc50e1e479856f56f8447b80e4cda",
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
