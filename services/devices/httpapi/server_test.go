// SPDX-License-Identifier: Apache-2.0
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/services/devices/fielddevice"
)

func newTestAPI(t *testing.T, kind fielddevice.Kind) (*fielddevice.Model, *Server) {
	t.Helper()
	zones, err := fielddevice.ParseZones("field=10.40.40.0/24,station=10.30.30.0/24,process=10.50.50.0/24,mgmt=10.99.99.0/24")
	if err != nil {
		t.Fatal(err)
	}
	id := map[fielddevice.Kind]string{fielddevice.Breaker: "feeder-breaker", fielddevice.RMU: "rmu-1", fielddevice.OLTC: "oltc-1"}[kind]
	model, err := fielddevice.NewModel(fielddevice.Config{Kind: kind, DeviceID: id, RMUNormal: fielddevice.PositionClosed, Generation: 321})
	if err != nil {
		t.Fatal(err)
	}
	return model, New(model, zones)
}
func perform(handler http.Handler, method, path, body, remote string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if remote != "" {
		request.RemoteAddr = remote
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func decode(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("JSON %d %s: %v", response.Code, response.Body, err)
	}
	return result
}
func inputFor(m *fielddevice.Model) fielddevice.MeasurementInput {
	truth := m.Truth()
	return fielddevice.MeasurementInput{Schema: 3, DeviceID: truth.DeviceID, Generation: truth.Generation, Revision: truth.Revision,
		SolverEpoch: "solver-a", SolveSeq: 1, SolvedAt: "2026-10-15T10:00:00Z", CurrentA: 41.2, VoltageKV: 19.93, SourceKV: 20, SectionLive: true, SourceLive: false}
}
func postMeasurement(t *testing.T, api *Server, input fielddevice.MeasurementInput) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return perform(api.ProcessHandler(), http.MethodPost, "/api/measurements", string(body), "10.50.50.20:43001")
}
func assertAck(t *testing.T, response *httptest.ResponseRecorder, solved, revision uint64) {
	t.Helper()
	got := decode(t, response)
	if response.Code != 200 || len(got) != 3 || got["accepted"] != true || got["solved_revision"] != float64(solved) || got["revision"] != float64(revision) {
		t.Fatalf("ack %d: %v", response.Code, got)
	}
}
func TestListenerSeparationAndRemovedRoutes(t *testing.T) {
	_, api := newTestAPI(t, fielddevice.Breaker)
	for _, tt := range []struct {
		name         string
		handler      http.Handler
		method, path string
	}{
		{"sim health on process", api.ProcessHandler(), "GET", "/api/health"},
		{"state on process", api.ProcessHandler(), "GET", "/api/state"},
		{"truth on sim", api.SimHandler(), "GET", "/api/truth"},
		{"measurements on sim", api.SimHandler(), "POST", "/api/measurements"},
		{"actions on process", api.ProcessHandler(), "POST", "/api/sim/actions"},
		{"operator command", api.SimHandler(), "POST", "/api/command"},
		{"old fault", api.SimHandler(), "POST", "/api/sim/fault"},
		{"old reset", api.SimHandler(), "POST", "/api/sim/reset"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if r := perform(tt.handler, tt.method, tt.path, `{}`, ""); r.Code != 404 {
				t.Fatalf("code=%d", r.Code)
			}
		})
	}
	r := perform(api.SimHandler(), "GET", "/api/health", "", "")
	got := decode(t, r)
	if r.Code != 200 || got["status"] != "ok" || got["measurements_valid"] != false || got["measurement_age_ms"] != nil {
		t.Fatalf("health=%v", got)
	}
}

func TestMeasurementsHTTPAcceptanceConflictsAndProtectionAck(t *testing.T) {
	for _, kind := range []fielddevice.Kind{fielddevice.Breaker, fielddevice.RMU, fielddevice.OLTC} {
		t.Run(string(kind), func(t *testing.T) {
			model, api := newTestAPI(t, kind)
			input := inputFor(model)
			assertAck(t, postMeasurement(t, api, input), 1, 1)
			for _, tt := range []struct {
				name   string
				edit   func(*fielddevice.MeasurementInput)
				reason string
			}{
				{"replay", func(i *fielddevice.MeasurementInput) {}, "replay"},
				{"stale revision", func(i *fielddevice.MeasurementInput) { i.Revision++ }, "stale_truth"},
				{"stale generation", func(i *fielddevice.MeasurementInput) { i.Generation++ }, "stale_truth"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					bad := input
					tt.edit(&bad)
					r := postMeasurement(t, api, bad)
					got := decode(t, r)
					if r.Code != 409 || len(got) != 2 || got["accepted"] != false || got["reason"] != tt.reason {
						t.Fatalf("response=%d %v", r.Code, got)
					}
				})
			}
			input.SolveSeq++
			input.SolvedAt = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC).Add(time.Duration(input.SolveSeq) * time.Second).Format(time.RFC3339Nano)
			input.CurrentA = 3500
			expected := uint64(1)
			if kind != fielddevice.OLTC {
				expected = 2
			}
			assertAck(t, postMeasurement(t, api, input), 1, expected)
			if kind != fielddevice.OLTC {
				if model.Snapshot().Measurements.Valid {
					t.Fatal("protection/FPI revision kept old measurements valid")
				}
				input.Revision = expected
				input.SolveSeq++
				input.SolvedAt = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC).Add(time.Duration(input.SolveSeq) * time.Second).Format(time.RFC3339Nano)
				assertAck(t, postMeasurement(t, api, input), expected, expected)
			}
			state := decode(t, perform(api.SimHandler(), "GET", "/api/state", "", ""))
			values := state["measurements"].(map[string]any)
			if state["schema"] != float64(3) || values["valid"] != true || values["source_kv_ll"] != float64(20) || state["protection"].(map[string]any)["picked_up"] != true {
				t.Fatalf("state=%v", state)
			}
			if _, old := state["fault_active"]; old {
				t.Fatal("schema1 field leaked")
			}
		})
	}
}

func TestMeasurementStrictSchemaThreeBodies(t *testing.T) {
	model, api := newTestAPI(t, fielddevice.Breaker)
	base, _ := json.Marshal(inputFor(model))
	for _, tt := range []struct {
		name string
		body string
	}{
		{"schema1", strings.Replace(string(base), `"schema":3`, `"schema":1`, 1)},
		{"schema2", strings.Replace(string(base), `"schema":3`, `"schema":2`, 1)},
		{"removed topology flag", strings.TrimSuffix(string(base), "}") + `,"fault_current":true}`},
		{"unknown", strings.TrimSuffix(string(base), "}") + `,"fault_active":true}`},
		{"missing", strings.Replace(string(base), `,"source_live":false`, "", 1)},
		{"null", strings.Replace(string(base), `"source_live":false`, `"source_live":null`, 1)},
		{"negative", strings.Replace(string(base), `"current_a":41.2`, `"current_a":-1`, 1)},
		{"overflow", strings.Replace(string(base), `"current_a":41.2`, `"current_a":1e309`, 1)},
		{"nonUTC", strings.Replace(string(base), `2026-10-15T10:00:00Z`, `2026-10-15T10:00:00+00:00`, 1)},
		{"trailing JSON", string(base) + `{}`}, {"array", `[]`}, {"null body", `null`}, {"empty", ``},
		{"oversized", `{"x":"` + strings.Repeat("x", maxRequestBody) + `"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := perform(api.ProcessHandler(), "POST", "/api/measurements", tt.body, "")
			got := decode(t, r)
			if r.Code != 400 || len(got) != 2 || got["accepted"] != false || got["reason"] != "invalid" {
				t.Fatalf("invalid response=%d %v", r.Code, got)
			}
		})
	}
	// Every schema-3 field, including false booleans, is required and non-null.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(base, &fields); err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		t.Run("missing-"+key, func(t *testing.T) {
			copyFields := map[string]json.RawMessage{}
			for k, v := range fields {
				if k != key {
					copyFields[k] = v
				}
			}
			body, _ := json.Marshal(copyFields)
			if r := perform(api.ProcessHandler(), "POST", "/api/measurements", string(body), ""); r.Code != 400 {
				t.Fatalf("missing %s accepted", key)
			}
		})
	}
}

func TestSimActionsAndAudit(t *testing.T) {
	for _, tt := range []struct {
		kind    fielddevice.Kind
		actions []string
	}{
		{fielddevice.Breaker, []string{"reset_lockout", "reset"}},
		{fielddevice.RMU, []string{"set_local", "set_remote", "earth_open", "reset_fault_passage", "reset"}},
		{fielddevice.OLTC, []string{"set_local", "set_remote", "block", "unblock", "reset"}},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			model, api := newTestAPI(t, tt.kind)
			for _, name := range tt.actions {
				r := perform(api.SimHandler(), "POST", "/api/sim/actions", `{"action":"`+name+`"}`, "10.99.99.10:43001")
				if r.Code != 200 {
					t.Fatalf("%s response=%d %s", name, r.Code, r.Body)
				}
				got := decode(t, r)
				if got["revision"] != float64(model.Truth().Revision) || got["measurements"] == nil {
					t.Fatal("action did not return state")
				}
			}
			audit := decode(t, perform(api.SimHandler(), "GET", "/api/audit?after=0", "", ""))
			if audit["generation"] != float64(321) {
				t.Fatal("audit generation")
			}
			entries := audit["entries"].([]any)
			if len(entries) != len(tt.actions) {
				t.Fatalf("audit=%v", entries)
			}
			for _, raw := range entries {
				ctx := raw.(map[string]any)["context"].(map[string]any)
				if ctx["protocol"] != "lab" || ctx["peer"] != "10.99.99.10:43001" || ctx["zone"] != "mgmt" {
					t.Fatalf("lost lab actor=%v", ctx)
				}
			}
		})
	}
	_, api := newTestAPI(t, fielddevice.RMU)
	r := perform(api.SimHandler(), "POST", "/api/sim/actions", `{"action":"earth_close"}`, "")
	if r.Code != 409 || decode(t, r)["reason"] != "way_closed" {
		t.Fatal("earth interlock not surfaced")
	}
	for _, body := range []string{`{}`, `null`, `{"action":null}`, `{"action":""}`, `{"action":"inject_fault"}`, `{"action":"reset","value":1}`, `{"action":"reset"}{}`} {
		r := perform(api.SimHandler(), "POST", "/api/sim/actions", body, "")
		if r.Code != 400 {
			t.Fatalf("bad action %s accepted=%d", body, r.Code)
		}
	}
	if r := perform(api.SimHandler(), "GET", "/api/audit?after=invalid", "", ""); r.Code != 400 {
		t.Fatal("bad cursor accepted")
	}
}

func TestListenAddressRequiresExplicitIPv4(t *testing.T) {
	for _, address := range []string{":8080", "0.0.0.0:8080", "localhost:8080", "10.0.0.1:0", "[::1]:8080"} {
		if _, err := ListenAddress(address); err == nil {
			t.Errorf("accepted %s", address)
		}
	}
	if got, err := ListenAddress("10.50.50.30:8080"); err != nil || got != "10.50.50.30:8080" {
		t.Fatalf("listen=%s %v", got, err)
	}
}
