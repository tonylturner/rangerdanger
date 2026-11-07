package labs

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkMetadataReadDefinition(t *testing.T) {
	for _, test := range []struct {
		name, fields, problem string
	}{
		{"missing purdue", "", "purdue must not be empty"},
		{"blank purdue", "    purdue: '  '\n", "purdue must not be empty"},
		{"negative target", "    purdue: Level 3\n    target_sl: -1\n", "target_sl must be 1-4"},
		{"zero target", "    purdue: Level 3\n    target_sl: 0\n", "target_sl must be 1-4"},
		{"large target", "    purdue: Level 3\n    target_sl: 5\n", "target_sl must be 1-4"},
		{"fractional target", "    purdue: Level 3\n    target_sl: 1.5\n", "target_sl must be an integer"},
		{"quoted target", "    purdue: Level 3\n    target_sl: '2'\n", "target_sl must be an integer"},
		{"omitted target", "    purdue: Level 3\n", ""},
		{"minimum target", "    purdue: Level 3\n    target_sl: 1\n", ""},
		{"maximum target", "    purdue: Level 3\n    target_sl: 4\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			dir := writePackage(t, root, "example", "substation", nil)
			topology := strings.Replace(validTopologyYAML, "    purdue: Level 3\n", test.fields, 1)
			writeDefinition(t, filepath.Join(dir, "topology.yml"), topology)
			// ReadDefinition is also packagelint's topology validation boundary.
			pkg, err := ReadDefinition(root, filepath.Join(dir, "package.yml"))
			if test.problem != "" {
				if err == nil || !strings.Contains(err.Error(), test.problem) {
					t.Fatalf("ReadDefinition() = %v, want %q", err, test.problem)
				}
				if _, err := seedLoader(root).Load(); err == nil {
					t.Fatal("loader accepted invalid network metadata")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(pkg.Template.Networks[0])
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				t.Fatal(err)
			}
			if body["purdue"] != "Level 3" {
				t.Fatalf("missing Purdue label in JSON: %s", data)
			}
			target := pkg.Template.Networks[0].TargetSL
			if target == nil {
				if _, ok := body["target_sl"]; ok {
					t.Fatalf("absent target must be omitted from JSON: %s", data)
				}
			} else if body["target_sl"] != float64(*target) {
				t.Fatalf("target not preserved in JSON: %s", data)
			}
		})
	}
}
