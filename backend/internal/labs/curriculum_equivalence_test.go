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
	// Updated for the course cutover only after TestCourseSemanticParity and
	// the frontend course presentation snapshot passed without changing their baselines.
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
		"us-dnp3-substation":   "375cda0f0b0c4c34ee1bc4e20e4a36511aaf101e2266aa4e6ae95617c04b3b2b",
		"eu-iec104-substation": "a042df142e964b2ecd5d3469bb768b0c2839ca9d35bb646c2ce88ced7ad28f2c",
	} {
		root := filepath.Join("..", "..", "..", "lab-definitions")
		pkg, err := ReadDefinition(root, filepath.Join(root, "packages", id, "package.yml"))
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(struct {
			Model *Curriculum
			Plan  *CurriculumPlan
		}{pkg.Course.Features.SegmentationPlan.Model, pkg.SegmentationPlan().Plan})
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			t.Errorf("%s resolved model fingerprint = %s, want %s", id, got, want)
		}
	}
}
