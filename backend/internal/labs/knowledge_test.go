package labs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/models"
)

func TestKnowledgeUSMigration(t *testing.T) {
	sections, err := loadKnowledge("../../../lab-definitions/packages/us-dnp3-substation", "knowledge/index.yml", "../../scenarios")
	if err != nil {
		t.Fatal(err)
	}
	var want []struct {
		ID, Heading, Description, Icon, Accent string
		Articles                               []struct{ ID, Title, SHA256 string }
	}
	data, err := os.ReadFile("testdata/knowledge-us.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if len(sections) != len(want) {
		t.Fatalf("sections = %d, want %d", len(sections), len(want))
	}
	count := 0
	for i, s := range sections {
		w := want[i]
		if !reflect.DeepEqual([]string{s.ID, s.Heading, s.Description, s.Icon, s.Accent}, []string{w.ID, w.Heading, w.Description, w.Icon, w.Accent}) || len(s.Articles) != len(w.Articles) {
			t.Fatalf("section %d metadata/order changed", i)
		}
		for j, a := range s.Articles {
			hash := sha256.Sum256([]byte(a.Body))
			if a.ID != w.Articles[j].ID || a.Title != w.Articles[j].Title || hex.EncodeToString(hash[:]) != w.Articles[j].SHA256 {
				t.Fatalf("article %d/%d not byte-exact", i, j)
			}
			count++
		}
	}
	if count != 41 {
		t.Fatalf("articles = %d", count)
	}
}

func TestKnowledgeValidation(t *testing.T) {
	const valid = `sections:
  - id: basics
    heading: Basics
    articles:
      - id: baseline
        title: Baseline
        file: baseline.md
`
	tests := []struct {
		name, index, body, reference, scenario string
		symlink                                bool
	}{
		{name: "valid", index: valid, body: "[self](#baseline) [titled]( #baseline \"Title\" )\n[reference]: /knowledge#baseline"},
		{name: "missing index", index: valid, body: "ok", reference: "knowledge/missing.yml"},
		{name: "directory index", index: valid, body: "ok", reference: "knowledge/."},
		{name: "absolute index", index: valid, body: "ok", reference: "/etc/passwd"},
		{name: "index traversal", index: valid, body: "ok", reference: "knowledge/../index.yml"},
		{name: "empty index", index: " "},
		{name: "empty sections", index: "sections: []"},
		{name: "unknown field", index: valid + "extra: true\n", body: "ok"},
		{name: "body field", index: strings.Replace(valid, "file: baseline.md", "body: no\n        file: baseline.md", 1), body: "ok"},
		{name: "duplicate section", index: valid + strings.TrimPrefix(valid, "sections:\n"), body: "ok"},
		{name: "duplicate article", index: valid + "      - id: baseline\n        title: Again\n        file: baseline.md\n", body: "ok"},
		{name: "bad icon", index: strings.Replace(valid, "heading: Basics", "heading: Basics\n    icon: unknown", 1), body: "ok"},
		{name: "bad accent", index: strings.Replace(valid, "heading: Basics", "heading: Basics\n    accent: unknown", 1), body: "ok"},
		{name: "empty body", index: valid, body: " "},
		{name: "missing body", index: strings.Replace(valid, "baseline.md", "missing.md", 1), body: "ok"},
		{name: "directory body", index: strings.Replace(valid, "baseline.md", ".", 1), body: "ok"},
		{name: "absolute", index: strings.Replace(valid, "baseline.md", "/etc/passwd", 1), body: "ok"},
		{name: "traversal", index: strings.Replace(valid, "baseline.md", "../outside.md", 1), body: "ok"},
		{name: "symlink escape", index: valid, body: "ok", symlink: true},
		{name: "dangling body link", index: valid, body: "[unknown](#other)"},
		{name: "dangling spaced link", index: valid, body: "[unknown]( #other )"},
		{name: "dangling reference link", index: valid, body: "[reference]: /knowledge#other"},
		{name: "cross package link", index: valid, body: "[DNP3](/knowledge#dnp3)"},
		{name: "dangling scenario", index: valid, body: "ok", scenario: "[unknown](/knowledge#other)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeDefinition(t, filepath.Join(root, "knowledge/index.yml"), tt.index)
			writeDefinition(t, filepath.Join(root, "knowledge/baseline.md"), tt.body)
			writeDefinition(t, filepath.Join(root, "scenarios/one.yml"), strings.Replace(validScenarioYAML, "Review the active firewall rules.", tt.scenario, 1))
			if tt.symlink {
				outside := filepath.Join(t.TempDir(), "outside.md")
				writeDefinition(t, outside, "outside")
				if err := os.Remove(filepath.Join(root, "knowledge/baseline.md")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(root, "knowledge/baseline.md")); err != nil {
					t.Fatal(err)
				}
			}
			index := "knowledge/index.yml"
			if tt.reference != "" {
				index = tt.reference
			}
			_, err := loadKnowledge(root, index, "scenarios")
			if tt.name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid knowledge")
			}
		})
	}
}

func TestKnowledgeReadDefinitionRequired(t *testing.T) {
	root := t.TempDir()
	dir := writePackage(t, root, "alpha", "substation", nil)
	manifest := filepath.Join(dir, "package.yml")
	writeDefinition(t, manifest, strings.Replace(packageYAML("alpha"), "knowledge: knowledge/index.yml\n", "", 1))
	if _, err := ReadDefinition(root, manifest); err == nil || !strings.Contains(err.Error(), "knowledge") {
		t.Fatalf("missing knowledge = %v", err)
	}
}

func TestKnowledgeIndexConfinement(t *testing.T) {
	for _, target := range []string{"outside-package", "outside-knowledge"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			dir := writePackage(t, root, "alpha", "substation", nil)
			index := filepath.Join(dir, "knowledge/index.yml")
			data, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(t.TempDir(), "index.yml")
			if target == "outside-knowledge" {
				dest = filepath.Join(dir, "other-index.yml")
			}
			writeDefinition(t, dest, string(data))
			if err := os.Remove(index); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(dest, index); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadDefinition(root, filepath.Join(dir, "package.yml")); err == nil {
				t.Fatal("accepted index symlink escape")
			}
		})
	}
}

func TestKnowledgeSeedInvalidPackageWritesNothing(t *testing.T) {
	definitions := t.TempDir()
	writePackage(t, definitions, "alpha", "alpha-topology", map[string]string{"one.yml": scenarioNamed("alpha-scenario")})
	beta := writePackage(t, definitions, "beta", "beta-topology", map[string]string{"one.yml": scenarioNamed("beta-scenario")})
	writeDefinition(t, filepath.Join(beta, "knowledge/baseline.md"), "[missing](/knowledge#not-in-beta)")
	database := testDatabase(t)
	if _, err := seedLoader(definitions).Seed(context.Background(), database, "alpha"); err == nil {
		t.Fatal("accepted invalid knowledge in second package")
	}
	if count := countRows(t, database, &models.Scenario{}, "1 = 1"); count != 0 {
		t.Fatalf("wrote %d scenarios before validating every package", count)
	}
}
