package server

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateCourseParity = flag.Bool("update-course-parity", false, "capture representation-neutral course parity")

// The snapshot and this assertion survive representation changes unchanged.
// Only courseParityProjection's representation-specific accessors may change.
func TestCourseSemanticParity(t *testing.T) {
	for _, id := range []string{usPackageID, euPackageID} {
		t.Run(id, func(t *testing.T) {
			got, err := json.MarshalIndent(courseParityProjection(t, id), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join("testdata", "course-parity-"+id+".json")
			if *updateCourseParity {
				if err := os.WriteFile(path, got, 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s: course semantics changed", id)
			}
		})
	}
}
