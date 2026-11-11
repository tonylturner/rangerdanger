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
	// with action-first instructions and expandable reference depth.
	for id, want := range map[string]string{
		"us-dnp3-substation":   "c6dc6c752b597d01ac0cec6b40e914cadef30394265b65fec8822af364d2e20b",
		"eu-iec104-substation": "aa19ecaf83a5b3e0c1716d306229037d51122471a1f4297bd592146b456e927a",
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
