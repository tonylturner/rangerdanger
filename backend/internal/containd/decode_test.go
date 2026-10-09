package containd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// TestICSConfigDecodesContaindShape decodes ICS predicates as containd
// serialises them: pkg/cp/config/ics_json.go MarshalJSON writes the
// singular key functionCode as a numeric array.
func TestICSConfigDecodesContaindShape(t *testing.T) {
	tests := []struct {
		name string
		body string
		want ICSConfig
	}{
		{
			// containd TestICSPredicateMarshalJSONUsesNumericArray output.
			name: "full containd predicate",
			body: `{"protocol":"modbus","functionCode":[3,16],"unitId":7,"addresses":["0-10"],"objectClasses":[2,4],"readOnly":true,"mode":"enforce"}`,
			want: ICSConfig{Protocol: "modbus", FunctionCodes: []int{3, 16}, ReadOnly: true},
		},
		{
			// lab-definitions/firewall/substation-improved.json rtac-to-field-dnp3.
			name: "single code list",
			body: `{"protocol":"dnp3","functionCode":[1]}`,
			want: ICSConfig{Protocol: "dnp3", FunctionCodes: []int{1}},
		},
		{
			// lab-definitions/firewall/substation-weak.json: no ICS filter.
			name: "empty predicate",
			body: `{}`,
			want: ICSConfig{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ICSConfig
			if err := json.Unmarshal([]byte(tt.body), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestICSConfigIsReadOnly(t *testing.T) {
	tests := []struct {
		name string
		ics  ICSConfig
		want bool
	}{
		{"modbus read codes", ICSConfig{Protocol: "modbus", FunctionCodes: []int{1, 2, 3, 4}}, true},
		{"modbus single read", ICSConfig{Protocol: "modbus", FunctionCodes: []int{3}}, true},
		{"modbus single write", ICSConfig{Protocol: "modbus", FunctionCodes: []int{6}}, false},
		{"modbus four writes", ICSConfig{Protocol: "modbus", FunctionCodes: []int{5, 6, 15, 16}}, false},
		{"modbus reads plus writes", ICSConfig{Protocol: "modbus", FunctionCodes: []int{1, 2, 3, 4, 5, 6}}, false},
		{"dnp3 read", ICSConfig{Protocol: "dnp3", FunctionCodes: []int{1}}, true},
		{"dnp3 direct operate", ICSConfig{Protocol: "dnp3", FunctionCodes: []int{5}}, false},
		{"readOnly class without codes", ICSConfig{Protocol: "modbus", ReadOnly: true}, true},
		{"no codes matches everything", ICSConfig{Protocol: "modbus"}, false},
		{"unknown protocol", ICSConfig{Protocol: "s7comm", FunctionCodes: []int{4}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ics.IsReadOnly(); got != tt.want {
				t.Errorf("IsReadOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGetEventsDecodesContaindProto serves events in the shape of
// containd's GET /api/v1/events: a bare array of pkg/dp/events.Event,
// whose protocol key is `proto`. The first is a Modbus DPI request
// (pkg/dp/ics/modbus/decoder.go), the second an nflog rule hit
// (pkg/dp/capture/nflog_linux.go), which carries no proto.
func TestGetEventsDecodesContaindProto(t *testing.T) {
	const body = `[
  {"id":42,"flowId":"a1b2c3","proto":"modbus","kind":"request","attributes":{"transaction_id":1,"unit_id":1,"function_code":3,"is_write":false,"address":0,"quantity":10},"timestamp":"2026-10-09T12:00:01Z","srcIp":"10.30.30.20","dstIp":"10.40.40.20","srcPort":40312,"dstPort":502,"transport":"tcp"},
  {"id":41,"flowId":"","proto":"","kind":"firewall.rule.hit","attributes":{"ruleId":"rtac-to-field-modbus","action":"ALLOW","via":"nflog","proto":"tcp","port":"502"},"timestamp":"2026-10-09T12:00:00Z","srcIp":"10.30.30.20","dstIp":"10.40.40.20","srcPort":40312,"dstPort":502,"transport":"tcp"}
]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	events, err := newTestClient(srv.URL).GetEvents(context.Background(), "", 2)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	want := []struct {
		id, protocol, kind, src, dst string
		dstPort                      int
	}{
		{"42", "modbus", "request", "10.30.30.20", "10.40.40.20", 502},
		{"41", "", "firewall.rule.hit", "10.30.30.20", "10.40.40.20", 502},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d", len(events), len(want))
	}
	for i, w := range want {
		e := events[i]
		if string(e.ID) != w.id || e.Protocol != w.protocol || e.Kind != w.kind ||
			e.Source != w.src || e.Dest != w.dst || e.DstPort != w.dstPort {
			t.Errorf("event %d = {id:%s protocol:%q kind:%s src:%s dst:%s dstPort:%d}, want %+v",
				i, e.ID, e.Protocol, e.Kind, e.Source, e.Dest, e.DstPort, w)
		}
	}
	if !events[0].Timestamp.Equal(time.Date(2026, 10, 9, 12, 0, 1, 0, time.UTC)) {
		t.Errorf("timestamp = %v", events[0].Timestamp)
	}

	// The portal reads the backend's re-serialised event under `protocol`.
	out, err := json.Marshal(events[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var portal map[string]any
	if err := json.Unmarshal(out, &portal); err != nil {
		t.Fatalf("unmarshal portal JSON: %v", err)
	}
	if portal["protocol"] != "modbus" {
		t.Errorf("portal protocol = %v, want modbus", portal["protocol"])
	}
	if _, ok := portal["proto"]; ok {
		t.Errorf("portal JSON must not carry containd's proto key: %s", out)
	}
	for _, legacyField := range []string{"type", "source", "dest", "src_port", "dst_port", "details", "severity", "zone"} {
		if _, ok := portal[legacyField]; ok {
			t.Errorf("portal JSON must not carry legacy event field %q: %s", legacyField, out)
		}
	}
}

// TestZoneRuleSummariesImprovedPolicy serves the canned hardened policy
// as containd's running config and checks the RTAC-to-field edge label:
// the DNP3 rule allows only READ, the Modbus rule also allows writes.
func TestZoneRuleSummariesImprovedPolicy(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	policy, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "..",
		"lab-definitions", "firewall", "substation-improved.json"))
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(policy)
	}))
	defer srv.Close()

	rules, err := newTestClient(srv.URL).GetFirewallRules(context.Background())
	if err != nil {
		t.Fatalf("GetFirewallRules: %v", err)
	}
	modbus := findRuleByID(rules, "rtac-to-field-modbus")
	dnp3 := findRuleByID(rules, "rtac-to-field-dnp3")
	if modbus == nil || dnp3 == nil || modbus.ICS == nil || dnp3.ICS == nil {
		t.Fatalf("missing RTAC-to-field ICS rules: modbus=%+v dnp3=%+v", modbus, dnp3)
	}
	if !reflect.DeepEqual(modbus.ICS.FunctionCodes, []int{1, 2, 3, 4, 5, 6}) {
		t.Errorf("modbus function codes = %v", modbus.ICS.FunctionCodes)
	}
	if modbus.ICS.IsReadOnly() {
		t.Error("modbus rule allows writes 5 and 6 but is labelled read-only")
	}
	if !dnp3.ICS.IsReadOnly() {
		t.Error("dnp3 rule allows only READ but is not labelled read-only")
	}
}
