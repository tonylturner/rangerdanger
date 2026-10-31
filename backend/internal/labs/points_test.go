package labs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadIEC104Inventory(t *testing.T) {
	for _, tc := range []struct {
		name, body, problem string
	}{
		{"valid", `{"schema":2,"points":[{"id":"a","ioa":5001},{"id":"b","ioa":5002}]}`, ""},
		{"missing", "", "station inventory: open"},
		{"malformed", `{`, "station inventory"},
		{"empty", `{"schema":2,"points":[]}`, "points must not be empty"},
		{"null", `null`, "schema must be 2"},
		{"old schema", `{"schema":1,"points":[{"id":"a","ioa":5001}]}`, "schema must be 2"},
		{"missing ID", `{"schema":2,"points":[{"ioa":5001}]}`, "invalid identity"},
		{"blank ID", `{"schema":2,"points":[{"id":" ","ioa":5001}]}`, "invalid identity"},
		{"missing IOA", `{"schema":2,"points":[{"id":"a"}]}`, "invalid identity"},
		{"IOA out of range", `{"schema":2,"points":[{"id":"a","ioa":16777216}]}`, "invalid identity"},
		{"duplicate ID", `{"schema":2,"points":[{"id":"a","ioa":5001},{"id":"a","ioa":5002}]}`, "duplicate identity"},
		{"duplicate IOA", `{"schema":2,"points":[{"id":"a","ioa":5001},{"id":"b","ioa":5001}]}`, "duplicate identity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definitions, dir := copyEUDefinitions(t)
			path := filepath.Join(dir, "protocols", "points.json")
			if tc.body == "" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeDefinition(t, path, tc.body)
			}
			catalog, err := seedLoader(definitions).Load()
			if tc.problem != "" {
				if err == nil || !strings.Contains(err.Error(), tc.problem) {
					t.Fatalf("Load() = %v, want %q", err, tc.problem)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			points := catalog.Packages[0].StationPoints
			if len(points) != 2 || points[0].ID != "a" || points[0].IOA != 5001 || points[1].ID != "b" || points[1].IOA != 5002 {
				t.Fatalf("inventory not loaded from this package: %+v", points)
			}
		})
	}
}

func TestNonIEC104PackageNeedsNoInventory(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "substation", nil)
	catalog, err := seedLoader(definitions).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Packages[0].StationPoints) != 0 {
		t.Fatal("non-IEC104 package acquired a station inventory")
	}
}
