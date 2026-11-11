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
	// hand-authored change-request implementation with single-commit cutover,
	// and firewall-native DENY event correlation in both dynamic retest lessons
	// without requiring the later Lab 2.4 boundary capture, and worked rule
	// authoring plus source-restricted RESTRICT coverage, and removal of
	// unrendered tests/negativeEmpty fields, and single-source EU phase prose
	// with action-first instructions and expandable reference depth, and explicit
	// DENY/drop closure guidance distinct from RESET responses.
	for id, want := range map[string]string{
		"us-dnp3-substation":   "c40f6e6ed9f97a1047a625e746e30c647e82f710dce89ea6c94400d9825cbfad",
		"eu-iec104-substation": "8322ef6a847425acebc11ae09851fb70c68d5c0a90b9a21d346320ce1bb9d5fa",
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
