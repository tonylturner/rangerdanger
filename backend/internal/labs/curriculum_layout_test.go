package labs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurriculumDirectoryLayout(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"empty", map[string]string{}, "must not be empty"},
		{"unknown file", map[string]string{"notes.yml": "schema: 1\n"}, "unknown curriculum file"},
		{"backup", map[string]string{"catalog.yml~": "schema: 1\n"}, "unknown curriculum file"},
		{"nested directory", map[string]string{"plan.yml/nested.yml": "schema: 1\n"}, "unknown curriculum file"},
		{"duplicate across files", map[string]string{"catalog.yml": "schema: 1\n", "plan.yml": "schema: 1\n"}, "already defined"},
		{"duplicate within file", map[string]string{"catalog.yml": "schema: 1\nschema: 1\n"}, "already defined"},
		{"empty file", map[string]string{"catalog.yml": ""}, "document is empty"},
		{"null", map[string]string{"catalog.yml": "null\n"}, "nonempty mapping"},
		{"sequence", map[string]string{"catalog.yml": "- schema\n"}, "nonempty mapping"},
		{"empty map", map[string]string{"catalog.yml": "{}\n"}, "nonempty mapping"},
		{"nonstring key", map[string]string{"catalog.yml": "1: true\n"}, "string keys"},
		{"extra document", map[string]string{"catalog.yml": "schema: 1\n---\nschema: 1\n"}, "exactly one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "curriculum"), 0o700); err != nil {
				t.Fatal(err)
			}
			for name, data := range tc.files {
				writeDefinition(t, filepath.Join(dir, "curriculum", name), data)
			}
			_, err := readCurriculumDocument(dir, &CurriculumBinding{Dir: "curriculum"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestCurriculumDirectoryBinding(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../curriculum", "/curriculum", "nested/curriculum", `nested\curriculum`} {
		if _, err := readCurriculumDocument(t.TempDir(), &CurriculumBinding{Dir: name}); err == nil {
			t.Fatalf("accepted directory %q", name)
		}
	}
	var manifest PackageYAML
	if err := decodeStrict([]byte("curriculum:\n  file: curriculum.yml\n"), &manifest); err == nil || !strings.Contains(err.Error(), "field file") {
		t.Fatalf("retired file binding: %v", err)
	}
	dir := t.TempDir()
	if _, err := readCurriculumDocument(dir, &CurriculumBinding{Dir: "missing"}); err == nil {
		t.Fatal("accepted missing directory")
	}
	writeDefinition(t, filepath.Join(dir, "curriculum"), "schema: 1\n")
	if _, err := readCurriculumDocument(dir, &CurriculumBinding{Dir: "curriculum"}); err == nil {
		t.Fatal("accepted file as directory")
	}
}

func TestCurriculumDirectoryRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeDefinition(t, filepath.Join(target, "catalog.yml"), "schema: 1\n")
	if err := os.Symlink(target, filepath.Join(dir, "curriculum")); err != nil {
		t.Fatal(err)
	}
	if _, err := readCurriculumDocument(dir, &CurriculumBinding{Dir: "curriculum"}); err == nil {
		t.Fatal("accepted directory symlink")
	}
	if err := os.Symlink(filepath.Join(target, "catalog.yml"), filepath.Join(target, "plan.yml")); err != nil {
		t.Fatal(err)
	}
	if _, err := readCurriculumDocument(dir, &CurriculumBinding{Dir: "target"}); err == nil {
		t.Fatal("accepted section symlink")
	}
}
