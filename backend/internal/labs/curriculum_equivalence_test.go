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
	// Snapshots were updated for intentional catalog prose and unused presentation-data removal, readiness-driven budgets, and per-command source headers.
	for id, want := range map[string]string{
		"us-dnp3-substation":   "1283db69bebb3f6604575c8b0b5e031b12925d978f8b5bc7c6ebb0c454367a74",
		"eu-iec104-substation": "8daa13994e7798b0075bc4a0a3bb3586ff427758fbb931757b1b9163723d831d",
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
