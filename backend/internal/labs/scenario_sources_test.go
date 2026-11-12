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

func TestScenarioSources(t *testing.T) {
	for _, tc := range []struct {
		name, sources, want string
	}{
		{"scalar", "scenarios", "cannot unmarshal"},
		{"empty sources", "[]", "scenarios must not be empty"},
		{"unknown field", "[{dir: scenarios, typo: true}]", "field typo not found"},
		{"empty dir", "[{dir: ''}]", "nonempty relative"},
		{"absolute", "[{dir: /tmp}]", "relative"},
		{"windows absolute", "[{dir: 'C:/temp'}]", "relative"},
		{"backslash", `[{dir: '..\scenarios'}]`, "relative"},
		{"escape", "[{dir: ../../../outside}]", "escapes definitions root"},
		{"unknown id", "[{dir: scenarios, include: [unknown]}]", "must resolve exactly once"},
		{"filename not id", "[{dir: scenarios, include: [one]}]", "must resolve exactly once"},
		{"null include", "[{dir: scenarios, include: null}]", "include must be a nonempty list"},
		{"empty include", "[{dir: scenarios, include: []}]", "include must not be empty"},
		{"repeated include", "[{dir: scenarios, include: [baseline, baseline]}]", "duplicate include"},
		{"duplicate across sources", "[{dir: scenarios}, {dir: scenarios}]", "duplicate scenario id"},
		{"literal directory", "[{dir: 'lessons[1]'}]", ""},
		{"valid subset", "[{dir: scenarios, include: [baseline]}]", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := writePackage(t, root, "alpha", "substation", map[string]string{"one.yml": validScenarioYAML})
			writeDefinition(t, filepath.Join(dir, "lessons[1]/one.yml"), validScenarioYAML)
			writeDefinition(t, filepath.Join(dir, "package.yml"), strings.Replace(packageYAML("alpha"), "[{dir: scenarios}]", tc.sources, 1))
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

func TestScenarioSourcesSymlinkEscape(t *testing.T) {
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

func TestScenarioSourcesConsumerValidation(t *testing.T) {
	for _, tc := range []struct{ name, old, new, want string }{
		{"topology", "id: rtac-1", "id: other", "unknown node"},
		{"capability", "capabilities: [process.electrical]", "capabilities: []", "capability"},
		{"knowledge", "id: baseline", "id: other", "unknown knowledge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			scenario := strings.Replace(validScenarioYAML, "Review the active firewall rules.", `"[Read](/knowledge#baseline)"`, 1)
			writePackage(t, root, "alpha", "substation", map[string]string{"one.yml": scenario})
			dir := writePackage(t, root, "beta", "beta-topology", nil)
			writeDefinition(t, filepath.Join(dir, "package.yml"), strings.Replace(packageYAML("beta"), "[{dir: scenarios}]", "[{dir: ../alpha/scenarios, include: [baseline]}]", 1))
			name := map[string]string{"topology": "topology.yml", "capability": "package.yml", "knowledge": "knowledge/index.yml"}[tc.name]
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeDefinition(t, path, strings.Replace(string(data), tc.old, tc.new, 1))
			_, err = seedLoader(root).Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want consumer %s failure", err, tc.name)
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
