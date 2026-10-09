package containd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetFirewallRulesSuccess verifies config/rules parsing.
func TestGetFirewallRulesSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.URL.Path != "/api/v1/config" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"firewall": FirewallConfig{
				DefaultAction: "DENY",
				Rules: []FirewallRule{
					{ID: "it-to-dmz", Description: "IT to DMZ: SSH/HTTP/S", SourceZones: []string{"wan"}, DestZones: []string{"dmz"}, Action: "ALLOW",
						Protocols: []Protocol{{Name: "tcp", Port: "22"}, {Name: "tcp", Port: "443"}}},
					{ID: "deny-writes-safety", Description: "Block Modbus WRITE to Safety", DestZones: []string{"lan2"}, Action: "DENY",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{5, 6, 15, 16}}},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	rules, err := client.GetFirewallRules()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Action != "ALLOW" {
		t.Errorf("expected ALLOW, got %s", rules[0].Action)
	}
	if rules[1].ICS == nil {
		t.Fatal("expected ICS config on second rule")
	}
	if rules[1].ICS.Protocol != "modbus" {
		t.Errorf("expected modbus protocol, got %s", rules[1].ICS.Protocol)
	}
}

// TestGetZoneRuleSummariesGrouping verifies rules are grouped by zone pair correctly.
func TestGetZoneRuleSummariesGrouping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"firewall": FirewallConfig{
				DefaultAction: "DENY",
				Rules: []FirewallRule{
					{ID: "r1", Description: "IT to DMZ SSH", SourceZones: []string{"wan"}, DestZones: []string{"dmz"}, Action: "ALLOW",
						Protocols: []Protocol{{Name: "tcp", Port: "22"}}},
					{ID: "r2", Description: "IT to DMZ HTTPS", SourceZones: []string{"wan"}, DestZones: []string{"dmz"}, Action: "ALLOW",
						Protocols: []Protocol{{Name: "tcp", Port: "443"}}},
					{ID: "r3", Description: "HMI View Modbus R/O", SourceZones: []string{"dmz"}, DestZones: []string{"lan1"}, Action: "ALLOW",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{1, 2, 3, 4}}},
					{ID: "r4", Description: "Block writes to safety", DestZones: []string{"lan2"}, Action: "DENY",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{5, 6, 15, 16}}},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	summaries, err := client.GetZoneRuleSummaries()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build map for easier assertions
	smap := make(map[string]ZoneRuleSummary)
	for _, s := range summaries {
		key := s.SourceZone + "->" + s.DestZone
		smap[key] = s
	}

	// wan->dmz should have 2 rules grouped
	wanDmz, ok := smap["wan->dmz"]
	if !ok {
		t.Fatal("missing wan->dmz summary")
	}
	if len(wanDmz.RuleDetails) != 2 {
		t.Errorf("expected 2 rule details for wan->dmz, got %d", len(wanDmz.RuleDetails))
	}
	if wanDmz.Action != "ALLOW" {
		t.Errorf("expected ALLOW for wan->dmz, got %s", wanDmz.Action)
	}

	// dmz->lan1 should have modbus R/O
	dmzLan1, ok := smap["dmz->lan1"]
	if !ok {
		t.Fatal("missing dmz->lan1 summary")
	}
	if dmzLan1.Action != "ALLOW" {
		t.Errorf("expected ALLOW for dmz->lan1, got %s", dmzLan1.Action)
	}

	// any->lan2 should be DENY
	anyLan2, ok := smap["any->lan2"]
	if !ok {
		t.Fatal("missing any->lan2 summary")
	}
	if anyLan2.Action != "DENY" {
		t.Errorf("expected DENY for any->lan2, got %s", anyLan2.Action)
	}
}

// TestGetZoneRuleSummariesMixedAction verifies MIXED action detection.
func TestGetZoneRuleSummariesMixedAction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"firewall": FirewallConfig{
				Rules: []FirewallRule{
					{ID: "r1", Description: "Allow read", SourceZones: []string{"dmz"}, DestZones: []string{"lan2"}, Action: "ALLOW",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{1, 2, 3, 4}}},
					{ID: "r2", Description: "Deny write", SourceZones: []string{"dmz"}, DestZones: []string{"lan2"}, Action: "DENY",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{5, 6, 15, 16}}},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	summaries, err := client.GetZoneRuleSummaries()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Action != "MIXED" {
		t.Errorf("expected MIXED action, got %s", summaries[0].Action)
	}
}
