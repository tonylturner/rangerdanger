package server

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// firewall_compare.go drives the lab UI's "what changed between weak
// and improved" panel. The pure-logic helpers (compareRuleSets,
// zonePairLabel, loadFirewallRules) are testable without a server.
// The HTTP handlers (handleFirewallApply, handleFirewallApplyCustom)
// route through s.containdClient.ImportConfig — those need a mocked
// containd to test cleanly and are deferred.
//
// NB: same external-data caveat as firewall_config_test.go — these
// tests read JSON from lab-definitions/firewall/ at runtime via
// os.ReadFile, so Go's test cache doesn't track edits to those
// files. CI runs `go test -count=1` for this module to defeat the
// cache; locally `go clean -testcache` reproduces fresh.

func policyPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(
		filepath.Dir(thisFile), "..", "..", "..",
		"lab-definitions", "firewall", name,
	)
}

func TestLoadFirewallRules_BothPolicies(t *testing.T) {
	for _, name := range []string{"substation-weak.json", "substation-improved.json"} {
		cfg, err := loadFirewallRules(policyPath(t, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(cfg.Firewall.Rules) == 0 {
			t.Errorf("%s: no rules parsed — JSON shape may have drifted", name)
		}
		// Every rule should have an action and at least one zone reference.
		// "Global" rules (no zones) are filtered by compareRuleSets, but
		// we still expect every rule to declare an action.
		for i, r := range cfg.Firewall.Rules {
			if r.Action == "" {
				t.Errorf("%s rule %d (%s): missing action", name, i, r.ID)
			}
		}
	}
}

func TestZonePairLabel_KnownAndUnknown(t *testing.T) {
	cases := []struct {
		src, dst, want string
	}{
		{"wan", "lan1", "Enterprise → OT Ops"},
		{"dmz", "lan2", "Vendor → Field"},
		{"lan1", "lan2", "OT Ops → Field"},
		{"wan", "dmz", "Enterprise → Vendor"},
		// Unknown zone names fall through verbatim.
		{"lan3", "wan", "lan3 → Enterprise"},
		{"foo", "bar", "foo → bar"},
	}
	for _, tc := range cases {
		got := zonePairLabel(tc.src, tc.dst)
		if got != tc.want {
			t.Errorf("zonePairLabel(%q, %q) = %q; want %q", tc.src, tc.dst, got, tc.want)
		}
	}
}

func TestCompareRuleSets_WeakVsImproved(t *testing.T) {
	weak, err := loadFirewallRules(policyPath(t, "substation-weak.json"))
	if err != nil {
		t.Fatalf("load weak: %v", err)
	}
	improved, err := loadFirewallRules(policyPath(t, "substation-improved.json"))
	if err != nil {
		t.Fatalf("load improved: %v", err)
	}

	diffs := compareRuleSets(weak, improved)
	if len(diffs) == 0 {
		t.Fatal("expected at least one diff between weak and improved")
	}

	byPair := make(map[string]PolicyRuleDiff, len(diffs))
	for _, d := range diffs {
		byPair[d.ZonePair] = d
	}

	// Workshop-narrative invariants — these are the four canonical
	// cross-zone pairs the labs teach against. If any of them
	// disappears or flips direction, students will see a confusing
	// "what changed" panel and the lessons stop matching the policy.
	for _, pair := range []string{
		"Enterprise → Field",
		"Enterprise → OT Ops",
		"Vendor → Field",
		"Vendor → OT Ops",
	} {
		d, ok := byPair[pair]
		if !ok {
			t.Errorf("missing diff entry for %q", pair)
			continue
		}
		if d.Change == "unchanged" {
			t.Errorf("%s: weak vs improved should differ but compare reports unchanged", pair)
		}
	}

	// Enterprise → Field is the headline tighten — weak ALLOW,
	// improved DENY. Pin both actions explicitly because the UI
	// "before/after" badges read directly from these fields.
	if d, ok := byPair["Enterprise → Field"]; ok {
		if d.WeakAction != "ALLOW" {
			t.Errorf("Enterprise → Field weak action = %q; want ALLOW", d.WeakAction)
		}
		if d.ImprovedAction != "DENY" {
			t.Errorf("Enterprise → Field improved action = %q; want DENY", d.ImprovedAction)
		}
		if d.Change != "tightened" {
			t.Errorf("Enterprise → Field change = %q; want tightened", d.Change)
		}
	}
}

func TestCompareRuleSets_IdentityComparison(t *testing.T) {
	// Comparing improved against itself should yield no "tightened"
	// or "added" diffs — every pair either unchanged or absent.
	improved, err := loadFirewallRules(policyPath(t, "substation-improved.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	diffs := compareRuleSets(improved, improved)
	for _, d := range diffs {
		if d.Change == "tightened" || d.Change == "added" || d.Change == "removed" {
			t.Errorf("identity compare on %s reported change=%q (weak=%q improved=%q); expected unchanged",
				d.ZonePair, d.Change, d.WeakAction, d.ImprovedAction)
		}
	}
}

func TestCompareRuleSets_OutputOrderIsStable(t *testing.T) {
	// The diff order is fixed in compareRuleSets via orderedPairs.
	// The UI renders rows in this order; reordering would shuffle
	// the "before/after" panel between renders. Pin it.
	weak, err := loadFirewallRules(policyPath(t, "substation-weak.json"))
	if err != nil {
		t.Fatalf("load weak: %v", err)
	}
	improved, err := loadFirewallRules(policyPath(t, "substation-improved.json"))
	if err != nil {
		t.Fatalf("load improved: %v", err)
	}

	diffs1 := compareRuleSets(weak, improved)
	diffs2 := compareRuleSets(weak, improved)

	if len(diffs1) != len(diffs2) {
		t.Fatalf("compareRuleSets returned different lengths on repeat call: %d vs %d", len(diffs1), len(diffs2))
	}
	for i := range diffs1 {
		if diffs1[i].ZonePair != diffs2[i].ZonePair {
			t.Errorf("position %d: zone pair varies between calls (%q vs %q)",
				i, diffs1[i].ZonePair, diffs2[i].ZonePair)
		}
	}
}

func TestReadPolicyJSONWithRetryValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(`{"firewall":{"rules":[]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := readPolicyJSONWithRetry(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty data")
	}
}

func TestReadPolicyJSONWithRetryInvalidIncludesSnippet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	// Deliberately invalid JSON — simulates a permanent syntax error,
	// not a transient truncation. After 3 reads the helper should
	// return an error message with a head/tail snippet so callers can
	// see this is a real authoring bug, not a bind-mount race.
	if err := os.WriteFile(path, []byte(`{not json at all`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readPolicyJSONWithRetry(path)
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not valid JSON") {
		t.Errorf("expected 'not valid JSON' in error, got %q", msg)
	}
	if !strings.Contains(msg, "head=") {
		t.Errorf("expected head= snippet in error to aid diagnosis, got %q", msg)
	}
}

func TestReadPolicyJSONWithRetryMissingFile(t *testing.T) {
	_, err := readPolicyJSONWithRetry(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "failed to read config") {
		t.Errorf("expected wrapped read error, got %q", err.Error())
	}
}
