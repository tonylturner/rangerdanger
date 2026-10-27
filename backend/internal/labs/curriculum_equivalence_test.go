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
	// Snapshots were updated for intentional catalog prose and unused presentation-data removal.
	for id, want := range map[string]string{
		"us-dnp3-substation":   "c9776dbc7976bbc3956881cffe52d9bbebc84c7d35bfae753e1c475f16e1995e",
		"eu-iec104-substation": "cda81804dfa802c369065e4a2cfc441ba353e24b1eb620712b818a2e107226a5",
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
