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
	// The EU fingerprint was then updated for the three intentional DPI prose edits.
	for id, want := range map[string]string{
		"us-dnp3-substation":   "ab4eeadfe8b19b515307ed71b3c694f67b46a088e8cbc76bf9a8ceee6e2261e0",
		"eu-iec104-substation": "de82ea894e8159a09cef18ed9dae987fa62571752b537d442c75f3a3280346ea",
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
			t.Fatalf("%s resolved model fingerprint = %s, want %s", id, got, want)
		}
	}
}
