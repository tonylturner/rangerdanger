package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/services/shared"
)

func resetRTACState() {
	agg = &AggregatedState{
		Devices:     make(map[string]map[string]any),
		Electrical:  make(map[string]any),
		DeviceComms: make(map[string]bool),
		Lab:         LabOverride{PowerFactor: 1.0},
	}
	lastCapAutoSwitch = time.Time{}
	audit = shared.NewAuditLog(500)
}

func newRTACMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/lab-control", handleLabControl)
	mux.HandleFunc("GET /api/audit", handleAudit)
	return mux
}

func postRTACLabControl(mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/lab-control", strings.NewReader(body)))
	return response
}

func TestDeriveProcessImpact(t *testing.T) {
	resetRTACState()
	agg.Electrical["general_load_kw"] = float64(41)
	agg.Electrical["critical_load_kw"] = float64(19)

	tests := []struct {
		name    string
		device  string
		command string
		result  string
		zone    string
		want    string
	}{
		{
			name:    "not executed",
			device:  "recloser",
			command: "open",
			result:  "blocked",
			zone:    "ot_ops",
			want:    "no process change — command blocked",
		},
		{
			name:    "relay trip from vendor zone",
			device:  "relay",
			command: "trip",
			result:  "executed",
			zone:    "vendor",
			want:    "feeder breaker OPENED — 60 kW load de-energized, all downstream customers without power [from vendor zone]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveProcessImpact(tt.device, tt.command, tt.result, tt.zone); got != tt.want {
				t.Errorf("deriveProcessImpact() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapConversionHelpers(t *testing.T) {
	values := map[string]any{
		"float":       12.5,
		"bool":        true,
		"int":         int(7),
		"float_int":   float64(8),
		"wrong_float": "12.5",
		"wrong_bool":  "true",
		"wrong_int":   true,
	}
	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "float valid", got: floatFromMap(values, "float"), want: float64(12.5)},
		{name: "float missing", got: floatFromMap(values, "missing"), want: float64(0)},
		{name: "float wrong type", got: floatFromMap(values, "wrong_float"), want: float64(0)},
		{name: "bool valid", got: boolFromMap(values, "bool"), want: true},
		{name: "bool missing", got: boolFromMap(values, "missing"), want: false},
		{name: "bool wrong type", got: boolFromMap(values, "wrong_bool"), want: false},
		{name: "int valid", got: intFromMap(values, "int"), want: int(7)},
		{name: "int from float", got: intFromMap(values, "float_int"), want: int(8)},
		{name: "int missing", got: intFromMap(values, "missing"), want: int(0)},
		{name: "int wrong type", got: intFromMap(values, "wrong_int"), want: int(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("value = %#v, want %#v", tt.got, tt.want)
			}
		})
	}
}

func TestAutoControlDecisionsRegulator(t *testing.T) {
	tests := []struct {
		name      string
		manual    bool
		energized bool
		want      []autoCmd
	}{
		{name: "auto raises tap", energized: true, want: []autoCmd{{device: "regulator", command: "raise_tap"}}},
		{name: "manual does not act", manual: true, energized: true},
		{name: "dead bus does not act", energized: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRTACState()
			agg.Devices["regulator"] = map[string]any{
				"manual_mode":        tt.manual,
				"voltage_setpoint_v": float64(120),
				"tap_position":       int(0),
			}
			agg.Electrical["critical_load_energized"] = tt.energized
			agg.Electrical["critical_load_voltage_v"] = float64(117)
			if got := autoControlDecisions(); !equalAutoCommands(got, tt.want) {
				t.Errorf("autoControlDecisions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAutoControlDecisionsCapbank(t *testing.T) {
	tests := []struct {
		name       string
		lockout    bool
		lastChange time.Time
		want       []autoCmd
	}{
		{name: "switches in", lastChange: time.Now().Add(-time.Hour), want: []autoCmd{{device: "capbank", command: "switch_in"}}},
		{name: "lockout prevents switching", lockout: true, lastChange: time.Now().Add(-time.Hour)},
		{name: "dwell has not elapsed", lastChange: time.Now()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRTACState()
			lastCapAutoSwitch = tt.lastChange
			agg.Devices["capbank"] = map[string]any{
				"auto_mode":             true,
				"lockout":               tt.lockout,
				"switched_in":           false,
				"voltage_thresh_high_v": float64(126),
			}
			agg.Electrical["general_load_energized"] = true
			agg.Electrical["power_factor"] = float64(0.9)
			agg.Electrical["downstream_voltage_v"] = float64(120)
			if got := autoControlDecisions(); !equalAutoCommands(got, tt.want) {
				t.Errorf("autoControlDecisions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func equalAutoCommands(got, want []autoCmd) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestAlarmPredicates(t *testing.T) {
	tests := []struct {
		name         string
		devices      map[string]map[string]any
		deviceComms  map[string]bool
		electrical   map[string]any
		wantComm     bool
		wantBreaker  bool
		wantReclose  bool
		wantLowVolt  bool
		wantHighVolt bool
	}{
		{name: "empty state has no alarms"},
		{
			name:        "communications loss only",
			deviceComms: map[string]bool{"relay": false},
			wantComm:    true,
		},
		{
			name: "open breaker only",
			devices: map[string]map[string]any{
				"relay": {"breaker_closed": false},
			},
			deviceComms: map[string]bool{"relay": true},
			wantBreaker: true,
		},
		{
			name: "disabled reclose only",
			devices: map[string]map[string]any{
				"recloser": {"reclose_enabled": false},
			},
			wantReclose: true,
		},
		{
			name:        "low critical voltage only",
			electrical:  map[string]any{"critical_load_voltage_v": float64(110)},
			wantLowVolt: true,
		},
		{
			name:         "high critical voltage only",
			electrical:   map[string]any{"critical_load_voltage_v": float64(127)},
			wantHighVolt: true,
		},
		{
			name:       "low threshold is not an alarm",
			electrical: map[string]any{"critical_load_voltage_v": float64(114)},
		},
		{
			name:       "high threshold is not an alarm",
			electrical: map[string]any{"critical_load_voltage_v": float64(126)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRTACState()
			agg.Devices = tt.devices
			agg.DeviceComms = tt.deviceComms
			agg.Electrical = tt.electrical
			if got := hasCommLoss(); got != tt.wantComm {
				t.Errorf("hasCommLoss() = %v, want %v", got, tt.wantComm)
			}
			if got := isBreakerOpenUnexpected(); got != tt.wantBreaker {
				t.Errorf("isBreakerOpenUnexpected() = %v, want %v", got, tt.wantBreaker)
			}
			if got := isRecloseDisabled(); got != tt.wantReclose {
				t.Errorf("isRecloseDisabled() = %v, want %v", got, tt.wantReclose)
			}
			if got := isLowVoltageCritical(); got != tt.wantLowVolt {
				t.Errorf("isLowVoltageCritical() = %v, want %v", got, tt.wantLowVolt)
			}
			if got := isHighVoltageCritical(); got != tt.wantHighVolt {
				t.Errorf("isHighVoltageCritical() = %v, want %v", got, tt.wantHighVolt)
			}
		})
	}
}

func TestHandleLabControl(t *testing.T) {
	resetRTACState()
	agg.Lab = LabOverride{PowerFactor: 1.0, GeneralPct: 40, CriticalPct: 60}
	mux := newRTACMux()
	response := postRTACLabControl(mux, `{"general_load_pct":75,"audit_command":"ramp_load","audit_target":"load-simulator","audit_detail":"load increased","source":"web-ui"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("lab-control status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Result string      `json:"result"`
		Lab    LabOverride `json:"lab"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode lab-control response: %v", err)
	}
	if body.Result != "ok" || body.Lab.GeneralPct != 75 || body.Lab.CriticalPct != 60 || body.Lab.PowerFactor != 1.0 || body.Lab.Active {
		t.Errorf("lab-control response = %+v, want only general load patched to 75", body)
	}
	if agg.Lab.GeneralPct != 75 || agg.Lab.CriticalPct != 60 || agg.Lab.PowerFactor != 1.0 || agg.Lab.Active {
		t.Errorf("lab state = %+v, want only general load patched to 75", agg.Lab)
	}
	entries := audit.Entries()
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(entries))
	}
	if entries[0].Command != "ramp_load" || entries[0].Target != "load-simulator" || entries[0].Source != "web-ui" || entries[0].Result != "executed" || entries[0].ProcessImpact != "load increased" {
		t.Errorf("audit entry = %+v, want audit_command details", entries[0])
	}

	resetRTACState()
	response = postRTACLabControl(mux, `{"active":`)
	if response.Code != http.StatusBadRequest {
		t.Errorf("malformed lab-control status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if agg.Lab != (LabOverride{PowerFactor: 1.0}) {
		t.Errorf("malformed request changed lab state: %+v", agg.Lab)
	}
	if got := len(audit.Entries()); got != 0 {
		t.Errorf("malformed request audit entries = %d, want 0", got)
	}
}
