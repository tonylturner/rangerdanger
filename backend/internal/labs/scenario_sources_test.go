package labs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

func TestScenarioDirectory(t *testing.T) {
	for _, tc := range []struct {
		name, directory, want string
	}{
		{"scalar", "scenarios", ""},
		{"list", "[{dir: scenarios}]", "cannot unmarshal"},
		{"mapping", "{dir: scenarios}", "cannot unmarshal"},
		{"empty", "''", "scenarios must not be empty"},
		{"null", "null", "scenarios must not be empty"},
		{"absolute", "/tmp", "relative"},
		{"windows absolute", "C:/temp", "relative"},
		{"backslash", `'..\scenarios'`, "relative"},
		{"escape", "../../../outside", "escapes definitions root"},
		{"file", "scenarios/one.yml", "not a directory"},
		{"literal directory", "'lessons[1]'", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := writePackage(t, root, "alpha", "substation", map[string]string{"one.yml": validScenarioYAML})
			writeDefinition(t, filepath.Join(dir, "lessons[1]/one.yml"), validScenarioYAML)
			writeDefinition(t, filepath.Join(dir, "package.yml"), strings.Replace(packageYAML("alpha"), "scenarios: scenarios", "scenarios: "+tc.directory, 1))
			pkg, err := seedLoader(root).LoadPackage(filepath.Join(dir, "package.yml"))
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want %q", err, tc.want)
				}
			} else if err != nil || len(pkg.Scenarios) != 1 || pkg.Scenarios[0].ID != "baseline" {
				t.Fatalf("package = %#v, error = %v", pkg, err)
			}
		})
	}
}

func TestScenarioDirectoryRejectsDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, "alpha", "substation", map[string]string{"one.yml": validScenarioYAML, "two.yml": validScenarioYAML})
	if _, err := seedLoader(root).Load(); err == nil || !strings.Contains(err.Error(), "duplicate scenario id") {
		t.Fatalf("error = %v", err)
	}
}

func TestScenarioDirectoryRejectsNonRegularFile(t *testing.T) {
	root := t.TempDir()
	dir := writePackage(t, root, "alpha", "substation", nil)
	if err := os.Mkdir(filepath.Join(dir, "scenarios/not-a-file.yml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := seedLoader(root).Load(); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error = %v", err)
	}
}

func TestScenarioDirectoryAllowsConfinedSymlinks(t *testing.T) {
	root := t.TempDir()
	dir := writePackage(t, root, "alpha", "substation", map[string]string{"one.yml": validScenarioYAML})
	if err := os.Rename(filepath.Join(dir, "scenarios/one.yml"), filepath.Join(dir, "lesson.yml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../lesson.yml", filepath.Join(dir, "scenarios/one.yml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("scenarios", filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	writeDefinition(t, filepath.Join(dir, "package.yml"), strings.Replace(packageYAML("alpha"), "scenarios: scenarios", "scenarios: linked", 1))
	pkg, err := seedLoader(root).LoadPackage(filepath.Join(dir, "package.yml"))
	if err != nil || len(pkg.Scenarios) != 1 || pkg.Scenarios[0].ID != "baseline" {
		t.Fatalf("package = %#v, error = %v", pkg, err)
	}
}

func TestScenarioDirectorySymlinkEscape(t *testing.T) {
	for _, fileLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "file"}[fileLink], func(t *testing.T) {
			root := t.TempDir()
			dir := writePackage(t, root, "alpha", "substation", nil)
			outside := t.TempDir()
			writeDefinition(t, filepath.Join(outside, "one.yml"), validScenarioYAML)
			link, target := filepath.Join(dir, "scenarios"), outside
			if fileLink {
				link, target = filepath.Join(link, "one.yml"), filepath.Join(outside, "one.yml")
			} else if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			_, err := seedLoader(root).Load()
			if err == nil || !strings.Contains(err.Error(), "escapes definitions root") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSeedTransactionRollsBackMidCatalogFailure(t *testing.T) {
	root := t.TempDir()
	alpha := writePackage(t, root, "alpha", "substation", map[string]string{"one.yml": validScenarioYAML})
	writePackage(t, root, "beta", "beta-topology", map[string]string{"one.yml": validScenarioYAML})
	database := testDatabase(t)
	loader := seedLoader(root)
	if _, err := loader.Seed(context.Background(), database, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.Scenario{ID: "stale", PackageID: "alpha", Name: "Preserve on failure"}).Error; err != nil {
		t.Fatal(err)
	}
	var before []models.Scenario
	if err := database.Order("package_id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	writeDefinition(t, filepath.Join(alpha, "scenarios/one.yml"), strings.Replace(validScenarioYAML, "Baseline Review", "Changed", 1))
	// A real SQLite failure after alpha's write exercises transaction rollback,
	// including its prune, rather than a validation failure before any write.
	if err := database.Exec(`CREATE TRIGGER fail_beta BEFORE INSERT ON scenarios
		WHEN NEW.package_id = 'beta' BEGIN SELECT RAISE(ABORT, 'injected failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Seed(context.Background(), database, "alpha"); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("Seed error = %v", err)
	}
	var after []models.Scenario
	if err := database.Order("package_id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed seed changed rows: before=%#v after=%#v", before, after)
	}
}

func TestOldScenarioSchemaMigratesAndReseeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	database, err := db.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP TABLE scenarios",
		"CREATE TABLE scenarios (id TEXT PRIMARY KEY, package_id TEXT, name TEXT)",
		"INSERT INTO scenarios VALUES ('baseline', 'alpha', 'Old projection')",
		"CREATE TABLE workshop_state (value TEXT)",
		"INSERT INTO workshop_state VALUES ('preserve me')",
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = db.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err = database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	root := t.TempDir()
	writePackage(t, root, "alpha", "substation", map[string]string{"one.yml": validScenarioYAML})
	writePackage(t, root, "beta", "beta-topology", map[string]string{"one.yml": validScenarioYAML})
	if _, err := seedLoader(root).Seed(context.Background(), database, "alpha"); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, database, &models.Scenario{}, "id = ?", "baseline"); n != 2 {
		t.Fatalf("reseeded rows = %d", n)
	}
	var state string
	if err := database.Raw("SELECT value FROM workshop_state").Scan(&state).Error; err != nil || state != "preserve me" {
		t.Fatalf("unrelated state = %q, error = %v", state, err)
	}
	if err := database.Create(&models.Scenario{ID: "baseline", PackageID: "alpha"}).Error; err == nil {
		t.Fatal("composite duplicate accepted")
	}
	if err := database.Exec("INSERT INTO scenarios (id, package_id) VALUES ('missing-package', NULL)").Error; err == nil {
		t.Fatal("NULL package accepted")
	}
	var row models.Scenario
	if err := database.Where("package_id = ? AND id = ?", "alpha", "baseline").First(&row).Error; err != nil || row.Name != "Baseline Review" {
		t.Fatalf("reseeded row = %#v, error = %v", row, err)
	}
}
