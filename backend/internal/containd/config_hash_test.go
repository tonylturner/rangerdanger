package containd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFirewallHashFromBytesIgnoresEmptyICSInLabPolicies(t *testing.T) {
	tests := []struct {
		filename      string
		emptyICSCount int
	}{
		{filename: "substation-weak.json", emptyICSCount: 9},
		{filename: "substation-improved.json", emptyICSCount: 9},
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			config := readHashTestPolicy(t, tt.filename)
			withoutEmptyICS, removed := removeEmptyRuleICS(t, config)
			if removed != tt.emptyICSCount {
				t.Fatalf("removed %d empty rule ICS objects, want %d", removed, tt.emptyICSCount)
			}

			got, err := FirewallHashFromBytes(config)
			if err != nil {
				t.Fatalf("hash policy with empty ICS: %v", err)
			}
			want, err := FirewallHashFromBytes(withoutEmptyICS)
			if err != nil {
				t.Fatalf("hash policy without empty ICS: %v", err)
			}
			if got != want {
				t.Errorf("hash with empty rule ICS %s, want same as omitted ICS %s", got, want)
			}
		})
	}
}

func TestFirewallHashFromBytesPreservesMeaningfulRuleDifferences(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "non-empty ICS difference",
			left:  `{"rules":[{"id":"r","ics":{"protocol":"modbus"}}]}`,
			right: `{"rules":[{"id":"r","ics":{"protocol":"dnp3"}}]}`,
		},
		{
			name:  "action difference",
			left:  `{"rules":[{"id":"r","ics":{},"action":"ALLOW"}]}`,
			right: `{"rules":[{"id":"r","ics":{},"action":"DENY"}]}`,
		},
		{
			name:  "other rule field difference",
			left:  `{"rules":[{"id":"r","metadata":{"ics":{}}}]}`,
			right: `{"rules":[{"id":"r"}]}`,
		},
		{
			name:  "rule order",
			left:  `{"rules":[{"id":"first","ics":{}},{"id":"second","ics":{}}]}`,
			right: `{"rules":[{"id":"second","ics":{}},{"id":"first","ics":{}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left, err := FirewallHashFromBytes([]byte(tt.left))
			if err != nil {
				t.Fatalf("hash left document: %v", err)
			}
			right, err := FirewallHashFromBytes([]byte(tt.right))
			if err != nil {
				t.Fatalf("hash right document: %v", err)
			}
			if left == right {
				t.Errorf("distinct rule configurations produced the same hash %s", left)
			}
		})
	}
}

func TestFirewallHashFromBytesFullConfigAndBareFirewallAgree(t *testing.T) {
	for _, filename := range []string{"substation-weak.json", "substation-improved.json"} {
		t.Run(filename, func(t *testing.T) {
			config := readHashTestPolicy(t, filename)
			var full map[string]any
			if err := json.Unmarshal(config, &full); err != nil {
				t.Fatalf("decode full config: %v", err)
			}
			firewall, ok := full["firewall"]
			if !ok {
				t.Fatal("full config has no firewall")
			}
			bare, err := json.Marshal(firewall)
			if err != nil {
				t.Fatalf("marshal bare firewall: %v", err)
			}

			fullHash, err := FirewallHashFromBytes(config)
			if err != nil {
				t.Fatalf("hash full config: %v", err)
			}
			bareHash, err := FirewallHashFromBytes(bare)
			if err != nil {
				t.Fatalf("hash bare firewall: %v", err)
			}
			if fullHash != bareHash {
				t.Errorf("full-config hash %s differs from bare-firewall hash %s", fullHash, bareHash)
			}
		})
	}
}

func TestGetFirewallHashIgnoresEmptyRuleICS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"firewall":{"rules":[{"id":"r","ics":{},"action":"ALLOW"}]}}`))
	}))
	defer server.Close()

	got, err := newTestClient(server.URL).GetFirewallHash(context.Background())
	if err != nil {
		t.Fatalf("get firewall hash: %v", err)
	}
	want, err := FirewallHashFromBytes([]byte(`{"rules":[{"id":"r","action":"ALLOW"}]}`))
	if err != nil {
		t.Fatalf("hash firewall without empty ICS: %v", err)
	}
	if got != want {
		t.Errorf("GetFirewallHash hash %s, want omitted-ICS hash %s", got, want)
	}
}

func readHashTestPolicy(t *testing.T, filename string) []byte {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate config_hash_test.go")
	}
	path := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "lab-definitions", "firewall", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read policy %s: %v", filename, err)
	}
	return data
}

func removeEmptyRuleICS(t *testing.T, config []byte) ([]byte, int) {
	t.Helper()
	var full map[string]any
	if err := json.Unmarshal(config, &full); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	firewall, ok := full["firewall"].(map[string]any)
	if !ok {
		t.Fatal("policy has no firewall object")
	}
	rules, ok := firewall["rules"].([]any)
	if !ok {
		t.Fatal("firewall has no rules array")
	}

	removed := 0
	for _, value := range rules {
		rule, ok := value.(map[string]any)
		if !ok {
			continue
		}
		ics, ok := rule["ics"].(map[string]any)
		if ok && len(ics) == 0 {
			delete(rule, "ics")
			removed++
		}
	}
	withoutEmptyICS, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal policy without empty ICS: %v", err)
	}
	return withoutEmptyICS, removed
}
