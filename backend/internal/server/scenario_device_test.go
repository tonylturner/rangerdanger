package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

func seedEUScenario(t *testing.T, s *Server) []labs.ScenarioStep {
	t.Helper()
	database, err := db.Connect(filepath.Join(t.TempDir(), "steps.db"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := labs.NewLoader(s.cfg.LabDefinitionsPath, ValidatorRequirements()).Seed(context.Background(), database, euPackageID)
	if err != nil {
		t.Fatal(err)
	}
	s.db, s.catalog = database, catalog
	scenario, err := s.findActiveScenario("iec104-telecontrol-boundary")
	if err != nil {
		t.Fatal(err)
	}
	var steps []labs.ScenarioStep
	if err := json.Unmarshal([]byte(scenario.Steps), &steps); err != nil {
		t.Fatal(err)
	}
	return steps
}

func executeAuthoredStep(t *testing.T, s *Server, steps []labs.ScenarioStep, id string) StepExecutionResult {
	t.Helper()
	for index, step := range steps {
		if step.ID != id {
			continue
		}
		rec := callRoute(s, s.handleExecuteStep, http.MethodPost,
			"/api/scenarios/:id/steps/:stepIdx/execute",
			"/api/scenarios/iec104-telecontrol-boundary/steps/"+strconv.Itoa(index)+"/execute", "")
		var result StepExecutionResult
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil {
			t.Fatalf("execute %s = %d %s", id, rec.Code, rec.Body)
		}
		return result
	}
	t.Fatalf("no authored step %s", id)
	return StepExecutionResult{}
}

func TestEUAuthoredDeviceSteps(t *testing.T) {
	stub, device := newUpstreamStub(t, map[string]stubResponse{
		"POST /api/sim/fault": {http.StatusOK, deviceStateFixture},
		"POST /api/sim/reset": {http.StatusOK, deviceStateFixture},
	})
	s := rangeServer(euGeneration(t, nil, map[string]string{"feeder_breaker": device.URL}), "improved")
	steps := seedEUScenario(t, s)
	for _, id := range []string{"local-trip", "clear-fault", "restore-device"} {
		if result := executeAuthoredStep(t, s, steps, id); !result.Success || result.ActionType != "command" {
			t.Errorf("%s = %+v", id, result)
		}
	}
	if !reflect.DeepEqual(stub.requests, []string{"POST /api/sim/fault", "POST /api/sim/fault", "POST /api/sim/reset"}) ||
		!reflect.DeepEqual(stub.bodies, []string{`{"active":true}`, `{"active":false}`, `{}`}) {
		t.Fatalf("device requests = %q %q", stub.requests, stub.bodies)
	}
}

func TestEUDeviceDispatchRefusesInvalidActions(t *testing.T) {
	tests := []struct {
		name, device, command, source, answer string
		status                                int
		value                                 *float64
		wantRequests                          int
	}{
		{name: "US device", device: "relay", command: "inject_fault"},
		{name: "unknown operation", device: "breaker", command: "close"},
		{name: "source override", device: "breaker", command: "reset", source: "operator"},
		{name: "value", device: "breaker", command: "reset", value: new(float64)},
		{name: "ack", device: "breaker", command: "reset", answer: `{"accepted":true}`, status: 200, wantRequests: 1},
		{name: "non object", device: "breaker", command: "reset", answer: `[]`, status: 200, wantRequests: 1},
		{name: "malformed", device: "breaker", command: "reset", answer: `{"schema":`, status: 200, wantRequests: 1},
		{name: "device failure", device: "breaker", command: "reset", answer: `failed`, status: 500, wantRequests: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub, device := newUpstreamStub(t, map[string]stubResponse{
				"POST /api/sim/reset": {test.status, test.answer},
			})
			s := rangeServer(euGeneration(t, nil, map[string]string{"feeder_breaker": device.URL}), "improved")
			result := s.executeCommand(context.Background(), s.rng.(*fakeRange).gen, test.device, test.command, test.source, test.value)
			if result.Success || result.Detail == "" || len(stub.requests) != test.wantRequests {
				t.Fatalf("result = %+v, requests = %q", result, stub.requests)
			}
		})
	}
}

func TestEUAuthoredSessionCut(t *testing.T) {
	cc := `{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":"40312","dport":"2404"}`
	tests := []struct {
		name, inventory string
		status          int
		success         bool
	}{
		{"exact", conntrackFixture, 200, true},
		{"none", `[]`, 200, false},
		{"ambiguous", `[` + cc + `,` + cc + `]`, 200, false},
		{"missing source port", `[` + strings.Replace(cc, `"40312"`, `""`, 1) + `]`, 200, false},
		{"zero source port", `[` + strings.Replace(cc, `"40312"`, `"0"`, 1) + `]`, 200, false},
		{"wrong protocol", `[` + strings.Replace(cc, `"tcp"`, `"udp"`, 1) + `]`, 200, false},
		{"wrong source", `[` + strings.Replace(cc, `"10.60.60.20"`, `"10.10.10.50"`, 1) + `]`, 200, false},
		{"wrong destination", `[` + strings.Replace(cc, `"10.30.30.20"`, `"10.30.30.21"`, 1) + `]`, 200, false},
		{"wrong port", `[` + strings.Replace(cc, `"2404"`, `"502"`, 1) + `]`, 200, false},
		{"malformed inventory", `{}`, 200, false},
		{"upstream error", `unavailable`, 503, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub, fw := newUpstreamStub(t, map[string]stubResponse{
				"GET /api/v1/conntrack?limit=5000": {test.status, test.inventory},
				"POST /api/v1/conntrack/kill":      {200, `{"status":"deleted"}`},
			})
			s := rangeServer(euGeneration(t, containd.NewClient(fw.URL), nil), "custom")
			result := executeAuthoredStep(t, s, seedEUScenario(t, s), "cut-session")
			if result.Success != test.success || result.ActionType != "session-cut" {
				t.Fatalf("cut = %+v", result)
			}
			if test.success {
				want := `{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":40312,"dport":2404}`
				if !reflect.DeepEqual(stub.requests, []string{"GET /api/v1/conntrack?limit=5000", "POST /api/v1/conntrack/kill"}) || !reflect.DeepEqual(stub.bodies, []string{"", want}) {
					t.Fatalf("kill bodies = %q", stub.bodies)
				}
			} else if !reflect.DeepEqual(stub.requests, []string{"GET /api/v1/conntrack?limit=5000"}) {
				t.Fatalf("refused lookup still killed: %q", stub.bodies)
			}
		})
	}
}

func TestStepActionsArePackageScoped(t *testing.T) {
	s := rangeServer(euGeneration(t, nil, nil), "weak")
	steps := seedEUScenario(t, s)
	if err := s.db.Model(&models.Scenario{}).Where("id = ?", "iec104-telecontrol-boundary").Update("package_id", usPackageID).Error; err != nil {
		t.Fatal(err)
	}
	for index, step := range steps {
		if step.ID != "local-trip" {
			continue
		}
		rec := callRoute(s, s.handleExecuteStep, http.MethodPost, "/api/scenarios/:id/steps/:stepIdx/execute",
			"/api/scenarios/iec104-telecontrol-boundary/steps/"+strconv.Itoa(index)+"/execute", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("cross-package execution = %d %s", rec.Code, rec.Body)
		}
	}
}
