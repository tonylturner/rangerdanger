package labs

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readPolicyFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "policies", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLintPolicyForbiddenICS(t *testing.T) {
	tests := []struct {
		fixture  string
		pointers []string
	}{
		{"clean.json", nil},
		{"ics-rule.json", []string{"/firewall/rules/1/ics"}},
		{"ics-empty.json", []string{"/firewall/rules/0/ics"}},
		{"ics-null.json", []string{"/firewall/rules/0/ics"}},
		{"ics-top.json", []string{"/ics"}},
		// Escaped reference tokens, arrays of arrays, and both copies of a
		// duplicated key.
		{"ics-deep.json", []string{"/services/a~1b/0/0/c~0d/ics", "/firewall/rules/0/ics", "/firewall/rules/0/ics"}},
	}
	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			file := "packages/eu/firewall/" + test.fixture
			err := LintPolicy(file, readPolicyFixture(t, test.fixture), ICSRulesForbidden)
			if test.pointers == nil {
				if err != nil {
					t.Fatalf("LintPolicy() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("LintPolicy() = nil, want forbidden ics keys")
			}
			problems := strings.Split(err.Error(), "; ")
			want := make([]string, len(test.pointers))
			for index, pointer := range test.pointers {
				want[index] = file + `: key "ics" at ` + pointer + " is forbidden (ics_rules: forbidden)"
			}
			if !reflect.DeepEqual(problems, want) {
				t.Errorf("LintPolicy() problems =\n%q\nwant\n%q", problems, want)
			}
		})
	}
}

func TestLintPolicyAllowedICS(t *testing.T) {
	for _, fixture := range []string{"clean.json", "ics-rule.json", "ics-empty.json", "ics-null.json", "ics-top.json", "ics-deep.json"} {
		if err := LintPolicy(fixture, readPolicyFixture(t, fixture), ICSRulesAllowed); err != nil {
			t.Errorf("LintPolicy(%s, allowed) = %v, want nil", fixture, err)
		}
	}
	// The shipped US policies carry ics blocks and stay valid.
	for _, name := range []string{"substation-weak.json", "substation-improved.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "lab-definitions", "firewall", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := LintPolicy(name, data, ICSRulesAllowed); err != nil {
			t.Errorf("LintPolicy(%s, allowed) = %v, want nil", name, err)
		}
		if err := LintPolicy(name, data, ICSRulesForbidden); err == nil || !strings.Contains(err.Error(), name+`: key "ics" at /firewall/rules/`) {
			t.Errorf("LintPolicy(%s, forbidden) = %v, want its ics blocks named", name, err)
		}
	}
}

func TestLintPolicyRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name, fixture, rules, problem string
	}{
		{"not an object", "not-object.json", ICSRulesAllowed, "x.json: policy must be a JSON object"},
		{"malformed", "malformed.json", ICSRulesForbidden, "x.json: invalid JSON: invalid character '}'"},
		{"unknown mode", "clean.json", "sometimes", `x.json: unknown ics_rules "sometimes"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := LintPolicy("x.json", readPolicyFixture(t, test.fixture), test.rules)
			if err == nil || !strings.HasPrefix(err.Error(), test.problem) {
				t.Fatalf("LintPolicy() = %v, want prefix %q", err, test.problem)
			}
		})
	}
	for _, document := range []string{"", "null", `"ics"`, `{} {}`} {
		if err := LintPolicy("x.json", []byte(document), ICSRulesForbidden); err == nil {
			t.Errorf("LintPolicy(%q) = nil, want an error", document)
		}
	}
}

// copyEUDefinitions copies the EU-shaped fixture tree into a temporary
// definitions directory the test may edit.
func copyEUDefinitions(t *testing.T) (string, string) {
	t.Helper()
	definitions := t.TempDir()
	if err := os.CopyFS(definitions, os.DirFS(filepath.Join("testdata", "eu-definitions"))); err != nil {
		t.Fatal(err)
	}
	return definitions, filepath.Join(definitions, "packages", "eu-iec104-substation")
}

func TestLoadEUPackage(t *testing.T) {
	definitions, _ := copyEUDefinitions(t)
	catalog, err := seedLoader(definitions).Load()
	if err != nil {
		t.Fatalf("Load(EU fixture): %v", err)
	}
	pkg := catalog.Packages[0]
	wantInfo := PackageInfo{
		ID: "eu-iec104-substation", Title: "EU telecontrol substation (IEC 104)", Revision: 1,
		Capabilities: []string{CapabilityPolicyContaind, CapabilityProtocolIEC104, CapabilityProcessTruth},
		OperatorView: OperatorViewEUIEC104,
	}
	if got := pkg.Info(); !reflect.DeepEqual(got, wantInfo) {
		t.Errorf("Info() = %#v, want %#v", got, wantInfo)
	}
	wantPolicies := map[string]string{
		"weak":     "packages/eu-iec104-substation/firewall/weak.json",
		"improved": "packages/eu-iec104-substation/firewall/improved.json",
	}
	if !reflect.DeepEqual(pkg.Policies, wantPolicies) || pkg.ICSRules != ICSRulesForbidden {
		t.Errorf("policies = %v, ics_rules = %q; want %v, forbidden", pkg.Policies, pkg.ICSRules, wantPolicies)
	}
	if name, ok := pkg.PolicyName(pkg.FirewallConfigPath); !ok || name != "weak" {
		t.Errorf("default policy name = %q, %v; want weak by exact path", name, ok)
	}
	if name, ok := pkg.PolicyName("packages/eu-iec104-substation/firewall/weak-improved.json"); ok {
		t.Errorf("PolicyName matched %q for an unknown path", name)
	}
}

func TestLoadEUPackageRejectsICSKeys(t *testing.T) {
	tests := []struct {
		name, fixture, pointer string
	}{
		{"non-default policy, rule ics", "ics-rule.json", "/firewall/rules/1/ics"},
		{"empty ics", "ics-empty.json", "/firewall/rules/0/ics"},
		{"null ics", "ics-null.json", "/firewall/rules/0/ics"},
		{"top-level ics", "ics-top.json", "/ics"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definitions, dir := copyEUDefinitions(t)
			writeDefinition(t, filepath.Join(dir, "firewall", "improved.json"), string(readPolicyFixture(t, test.fixture)))
			_, err := seedLoader(definitions).Load()
			want := `policy improved: packages/eu-iec104-substation/firewall/improved.json: key "ics" at ` + test.pointer + " is forbidden"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Load() = %v, want %q", err, want)
			}
			if !strings.Contains(err.Error(), "package eu-iec104-substation") {
				t.Errorf("Load() = %q, want the package named", err)
			}
		})
	}
}

func TestReadDefinitionNeedsNoValidators(t *testing.T) {
	definitions, dir := copyEUDefinitions(t)
	// A validator the loader would reject without a registry entry.
	scenario := filepath.Join(dir, "scenarios", "telecontrol.yml")
	data, err := os.ReadFile(scenario)
	if err != nil {
		t.Fatal(err)
	}
	writeDefinition(t, scenario, string(data)+"validator: eu-iec104-telecontrol\n")
	pkg, err := ReadDefinition(definitions, filepath.Join(dir, "package.yml"))
	if err != nil {
		t.Fatalf("ReadDefinition() = %v", err)
	}
	if pkg.Template.ID != "telecontrol-segmentation" || pkg.Scenarios != nil || len(pkg.Policies) != 2 {
		t.Errorf("ReadDefinition() = %q, %d scenarios, %d policies; want the topology and policies only", pkg.Template.ID, len(pkg.Scenarios), len(pkg.Policies))
	}

	writeDefinition(t, filepath.Join(dir, "firewall", "weak.json"), string(readPolicyFixture(t, "ics-null.json")))
	if _, err := ReadDefinition(definitions, filepath.Join(dir, "package.yml")); err == nil || !strings.Contains(err.Error(), `weak.json: key "ics" at /firewall/rules/0/ics`) {
		t.Errorf("ReadDefinition() = %v, want the default policy's ics key", err)
	}
}
