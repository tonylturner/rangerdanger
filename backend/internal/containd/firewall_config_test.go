package containd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// containdConfig represents the full containd configuration file structure.
type containdConfig struct {
	Interfaces []struct {
		Name   string `json:"name"`
		Device string `json:"device"`
		Zone   string `json:"zone"`
	} `json:"interfaces"`
	Zones []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"zones"`
	Firewall FirewallConfig `json:"firewall"`
}

func loadConfig(t *testing.T, filename string) containdConfig {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	path := filepath.Join(dir, "..", "..", "..", "lab-definitions", "firewall", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filename, err)
	}
	var cfg containdConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse %s: %v", filename, err)
	}
	return cfg
}

func findRuleByID(rules []FirewallRule, id string) *FirewallRule {
	for i, r := range rules {
		if r.ID == id {
			return &rules[i]
		}
	}
	return nil
}

func findRulesByZonePair(rules []FirewallRule, srcZone, dstZone string) []FirewallRule {
	var matched []FirewallRule
	for _, r := range rules {
		srcMatch := len(r.SourceZones) == 0
		for _, sz := range r.SourceZones {
			if sz == srcZone {
				srcMatch = true
				break
			}
		}
		dstMatch := len(r.DestZones) == 0
		for _, dz := range r.DestZones {
			if dz == dstZone {
				dstMatch = true
				break
			}
		}
		if srcMatch && dstMatch {
			matched = append(matched, r)
		}
	}
	return matched
}

// --- Shared structure tests ---

func TestBothConfigsHave4Zones(t *testing.T) {
	for _, file := range []string{"substation-weak.json", "substation-improved.json"} {
		cfg := loadConfig(t, file)
		if len(cfg.Zones) != 4 {
			t.Errorf("%s: expected 4 zones, got %d", file, len(cfg.Zones))
		}
	}
}

func TestBothConfigsHave4Interfaces(t *testing.T) {
	for _, file := range []string{"substation-weak.json", "substation-improved.json"} {
		cfg := loadConfig(t, file)
		if len(cfg.Interfaces) != 4 {
			t.Errorf("%s: expected 4 interfaces, got %d", file, len(cfg.Interfaces))
		}
	}
}

func TestBothConfigsZoneNames(t *testing.T) {
	expectedZones := map[string]bool{"wan": true, "dmz": true, "lan1": true, "lan2": true}

	for _, file := range []string{"substation-weak.json", "substation-improved.json"} {
		cfg := loadConfig(t, file)
		found := make(map[string]bool)
		for _, z := range cfg.Zones {
			found[z.Name] = true
		}
		for zone := range expectedZones {
			if !found[zone] {
				t.Errorf("%s: missing zone %q", file, zone)
			}
		}
	}
}

// --- Weak config tests ---

func TestWeakConfigEnterpriseToFieldAllow(t *testing.T) {
	cfg := loadConfig(t, "substation-weak.json")
	rule := findRuleByID(cfg.Firewall.Rules, "enterprise-to-field-weak")
	if rule == nil {
		t.Fatal("missing rule enterprise-to-field-weak")
	}
	if rule.Action != "ALLOW" {
		t.Errorf("expected ALLOW, got %s", rule.Action)
	}
}

func TestWeakConfigEnterpriseToOTAllow(t *testing.T) {
	cfg := loadConfig(t, "substation-weak.json")
	rule := findRuleByID(cfg.Firewall.Rules, "enterprise-to-ot-weak")
	if rule == nil {
		t.Fatal("missing rule enterprise-to-ot-weak")
	}
	if rule.Action != "ALLOW" {
		t.Errorf("expected ALLOW, got %s", rule.Action)
	}
}

func TestWeakConfigVendorToFieldAllow(t *testing.T) {
	cfg := loadConfig(t, "substation-weak.json")
	rule := findRuleByID(cfg.Firewall.Rules, "vendor-to-field-weak")
	if rule == nil {
		t.Fatal("missing rule vendor-to-field-weak")
	}
	if rule.Action != "ALLOW" {
		t.Errorf("expected ALLOW, got %s", rule.Action)
	}
}

// --- Improved config tests ---

func TestImprovedConfigEnterpriseToFieldDeny(t *testing.T) {
	cfg := loadConfig(t, "substation-improved.json")
	rule := findRuleByID(cfg.Firewall.Rules, "deny-enterprise-to-field")
	if rule == nil {
		t.Fatal("missing rule deny-enterprise-to-field")
	}
	if rule.Action != "DENY" {
		t.Errorf("expected DENY, got %s", rule.Action)
	}
}

func TestImprovedConfigEnterpriseToOTDeny(t *testing.T) {
	cfg := loadConfig(t, "substation-improved.json")
	rule := findRuleByID(cfg.Firewall.Rules, "deny-enterprise-to-ot")
	if rule == nil {
		t.Fatal("missing rule deny-enterprise-to-ot")
	}
	if rule.Action != "DENY" {
		t.Errorf("expected DENY, got %s", rule.Action)
	}
}

func TestImprovedConfigVendorToFieldDeny(t *testing.T) {
	cfg := loadConfig(t, "substation-improved.json")
	rule := findRuleByID(cfg.Firewall.Rules, "deny-vendor-to-field")
	if rule == nil {
		t.Fatal("missing rule deny-vendor-to-field")
	}
	if rule.Action != "DENY" {
		t.Errorf("expected DENY, got %s", rule.Action)
	}
}

func TestImprovedConfigRTACToFieldAllow(t *testing.T) {
	cfg := loadConfig(t, "substation-improved.json")
	rule := findRuleByID(cfg.Firewall.Rules, "rtac-to-field-control")
	if rule == nil {
		t.Fatal("missing rule rtac-to-field-control")
	}
	if rule.Action != "ALLOW" {
		t.Errorf("expected ALLOW, got %s", rule.Action)
	}
	// Verify source constraint
	foundSource := false
	for _, s := range rule.Sources {
		if s == "10.40.40.10/32" {
			foundSource = true
			break
		}
	}
	if !foundSource {
		t.Errorf("expected source constraint 10.40.40.10/32, got sources: %v", rule.Sources)
	}
	// Verify zone pair
	srcMatch := false
	for _, z := range rule.SourceZones {
		if z == "lan1" {
			srcMatch = true
		}
	}
	dstMatch := false
	for _, z := range rule.DestZones {
		if z == "lan2" {
			dstMatch = true
		}
	}
	if !srcMatch || !dstMatch {
		t.Errorf("expected source=lan1 dest=lan2, got sourceZones=%v destZones=%v", rule.SourceZones, rule.DestZones)
	}
}

func TestImprovedConfigOtherOTToFieldDeny(t *testing.T) {
	cfg := loadConfig(t, "substation-improved.json")
	rule := findRuleByID(cfg.Firewall.Rules, "deny-ot-to-field-direct")
	if rule == nil {
		t.Fatal("missing rule deny-ot-to-field-direct")
	}
	if rule.Action != "DENY" {
		t.Errorf("expected DENY, got %s", rule.Action)
	}
}
