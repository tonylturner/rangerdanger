// SPDX-License-Identifier: Apache-2.0
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/services/devices/feederbreaker"
)

func newTestAPI(t *testing.T) (*feederbreaker.Model, *Server) {
	t.Helper()
	zones, err := feederbreaker.ParseZones("field=10.40.40.0/24,station=10.30.30.0/24,process=10.50.50.0/24,mgmt=10.99.99.0/24")
	if err != nil {
		t.Fatal(err)
	}
	model, err := feederbreaker.NewModel(feederbreaker.Config{
		DeviceID: "feeder-breaker", Generation: 321,
		Now:            func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
		LocalContext:   feederbreaker.AuditContext{Peer: "protection", Protocol: "local-protection", Zone: zones.Name("field")},
		ProcessContext: feederbreaker.AuditContext{Peer: "measurement-expiry", Protocol: "process", Zone: zones.Name("process")},
	})
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

func TestListenerSeparationAndHealthShape(t *testing.T) {
	_, api := newTestAPI(t)
	processHealth := perform(api.ProcessHandler(), http.MethodGet, "/api/health", "", "10.50.50.20:44321")
	if processHealth.Code != http.StatusNotFound {
		t.Fatalf("process listener exposed instructor health: %d", processHealth.Code)
	}
	instructorTruth := perform(api.InstructorHandler(), http.MethodGet, "/api/truth", "", "10.99.99.22:44321")
	if instructorTruth.Code != http.StatusNotFound {
		t.Fatalf("instructor listener exposed process truth route: %d", instructorTruth.Code)
	}
	operatorCommand := perform(api.InstructorHandler(), http.MethodPost, "/api/command", `{}`, "10.99.99.22:44321")
	if operatorCommand.Code != http.StatusNotFound {
		t.Fatalf("instructor listener exposed a command endpoint: %d", operatorCommand.Code)
	}

	health := perform(api.InstructorHandler(), http.MethodGet, "/api/health", "", "10.99.99.10:44321")
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d: %s", health.Code, health.Body)
	}
	var result map[string]any
	if err := json.Unmarshal(health.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" || result["measurements_valid"] != false || result["measurement_age_ms"] != nil {
		t.Fatalf("health body = %#v", result)
	}
}

func TestMeasurementsHTTPAcceptanceReplayStaleAndValidation(t *testing.T) {
	model, api := newTestAPI(t)
	handler := api.ProcessHandler()
	truth := model.Truth()
	input := feederbreaker.MeasurementInput{
		Schema: 1, DeviceID: truth.DeviceID, Generation: truth.Generation, Revision: truth.Revision,
		SolverEpoch: "solver-one", SolveSeq: 1, SolvedAt: "2026-10-09T12:00:00.123456789Z",
		CurrentA: 28.9, VoltageKV: 19.9,
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	first := perform(handler, http.MethodPost, "/api/measurements", string(body), "10.50.50.20:43123")
	if first.Code != http.StatusOK {
		t.Fatalf("first measurement status=%d body=%s", first.Code, first.Body)
	}
	var accepted map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil || accepted["accepted"] != true || accepted["revision"] != float64(1) {
		t.Fatalf("accepted response=%#v err=%v", accepted, err)
	}

	replay := perform(handler, http.MethodPost, "/api/measurements", string(body), "10.50.50.20:43123")
	if replay.Code != http.StatusConflict {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body)
	}
	var conflict map[string]any
	if err := json.Unmarshal(replay.Body.Bytes(), &conflict); err != nil || conflict["reason"] != "replay" || conflict["accepted"] != false {
		t.Fatalf("replay response=%#v err=%v", conflict, err)
	}

	stale := input
	stale.Generation++
	stale.SolveSeq++
	staleBody, _ := json.Marshal(stale)
	staleResponse := perform(handler, http.MethodPost, "/api/measurements", string(staleBody), "10.50.50.20:43123")
	if staleResponse.Code != http.StatusConflict || !bytes.Contains(staleResponse.Body.Bytes(), []byte(`"stale_truth"`)) {
		t.Fatalf("stale response=%d %s", staleResponse.Code, staleResponse.Body)
	}

	invalid := input
	invalid.SolveSeq++
	invalid.CurrentA = -1
	invalidBody, _ := json.Marshal(invalid)
	invalidResponse := perform(handler, http.MethodPost, "/api/measurements", string(invalidBody), "10.50.50.20:43123")
	if invalidResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid values status=%d body=%s", invalidResponse.Code, invalidResponse.Body)
	}
	missingValue := bytes.TrimSuffix(body, []byte("}"))
	missingValue = bytes.Replace(missingValue, []byte(`,"current_a":28.9`), nil, 1)
	missingResponse := perform(handler, http.MethodPost, "/api/measurements", string(missingValue)+`}`, "10.50.50.20:43123")
	if missingResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing measurement field status=%d body=%s", missingResponse.Code, missingResponse.Body)
	}
	nullValue := bytes.Replace(body, []byte(`"current_a":28.9`), []byte(`"current_a":null`), 1)
	nullResponse := perform(handler, http.MethodPost, "/api/measurements", string(nullValue), "10.50.50.20:43123")
	if nullResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("null measurement field status=%d body=%s", nullResponse.Code, nullResponse.Body)
	}
	unknown := bytes.TrimSuffix(body, []byte("}"))
	unknown = append(unknown, []byte(`,"extra":true}`)...)
	unknownResponse := perform(handler, http.MethodPost, "/api/measurements", string(unknown), "10.50.50.20:43123")
	if unknownResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", unknownResponse.Code, unknownResponse.Body)
	}
	unknownOnly := perform(handler, http.MethodPost, "/api/measurements", `{"extra":true}`, "10.50.50.20:43123")
	if unknownOnly.Code != http.StatusBadRequest {
		t.Fatalf("unknown-only field status=%d body=%s", unknownOnly.Code, unknownOnly.Body)
	}

	state := perform(api.InstructorHandler(), http.MethodGet, "/api/state", "", "10.99.99.22:43123")
	var stateBody map[string]any
	if err := json.Unmarshal(state.Body.Bytes(), &stateBody); err != nil {
		t.Fatal(err)
	}
	measurements := stateBody["measurements"].(map[string]any)
	if stateBody["revision"] != float64(1) || measurements["valid"] != true || measurements["solver_epoch"] != "solver-one" {
		t.Fatalf("state response = %#v", stateBody)
	}
}

func TestFaultResetAuditRoutesAndStrictBodies(t *testing.T) {
	model, api := newTestAPI(t)
	instructor := api.InstructorHandler()
	missingActive := perform(instructor, http.MethodPost, "/api/instructor/fault", `{}`, "10.99.99.22:54321")
	if missingActive.Code != http.StatusBadRequest {
		t.Fatalf("missing active status=%d", missingActive.Code)
	}
	unknown := perform(instructor, http.MethodPost, "/api/instructor/fault", `{"active":true,"extra":1}`, "10.99.99.22:54321")
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown fault field status=%d", unknown.Code)
	}
	fault := perform(instructor, http.MethodPost, "/api/instructor/fault", `{"active":true}`, "10.99.99.22:54321")
	if fault.Code != http.StatusOK {
		t.Fatalf("fault status=%d body=%s", fault.Code, fault.Body)
	}
	truth := model.Truth()
	if truth.Position != feederbreaker.PositionOpen || !truth.Lockout || !truth.FaultActive || !truth.ProtectionTripped {
		t.Fatalf("fault state = %+v", truth)
	}
	resetNull := perform(instructor, http.MethodPost, "/api/instructor/reset", `null`, "10.99.99.22:54321")
	if resetNull.Code != http.StatusBadRequest {
		t.Fatalf("null reset body status=%d", resetNull.Code)
	}
	resetUnknown := perform(instructor, http.MethodPost, "/api/instructor/reset", `{"extra":1}`, "10.99.99.22:54321")
	if resetUnknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown reset field status=%d", resetUnknown.Code)
	}
	reset := perform(instructor, http.MethodPost, "/api/instructor/reset", `{}`, "10.99.99.22:54321")
	if reset.Code != http.StatusOK || model.Truth().Position != feederbreaker.PositionClosed {
		t.Fatalf("reset status=%d body=%s truth=%+v", reset.Code, reset.Body, model.Truth())
	}

	badAfter := perform(instructor, http.MethodGet, "/api/audit?after=nope", "", "10.99.99.22:54321")
	if badAfter.Code != http.StatusBadRequest {
		t.Fatalf("bad audit cursor status=%d", badAfter.Code)
	}
	audit := perform(instructor, http.MethodGet, "/api/audit?after=0", "", "10.99.99.22:54321")
	var auditBody struct {
		Generation uint32                     `json:"generation"`
		Entries    []feederbreaker.AuditEntry `json:"entries"`
		Latest     *uint64                    `json:"latest_seq"`
		Oldest     *uint64                    `json:"oldest_seq"`
	}
	if err := json.Unmarshal(audit.Body.Bytes(), &auditBody); err != nil {
		t.Fatal(err)
	}
	if audit.Code != http.StatusOK || auditBody.Generation != 321 || auditBody.Latest == nil || auditBody.Oldest == nil {
		t.Fatalf("audit body = %+v status=%d", auditBody, audit.Code)
	}
	foundTrip := false
	for _, entry := range auditBody.Entries {
		if entry.Operation == "local_protection_trip" {
			foundTrip = entry.Context.Protocol == "local-protection" && entry.Context.Zone == "field"
		}
	}
	if !foundTrip {
		t.Fatalf("local protection context missing from audit: %+v", auditBody.Entries)
	}
}

func TestListenAddressRequiresExplicitIPv4(t *testing.T) {
	for _, address := range []string{":8080", "0.0.0.0:8080", "localhost:8080", "10.0.0.1:0"} {
		if _, err := ListenAddress(address); err == nil {
			t.Errorf("ListenAddress(%q) unexpectedly accepted", address)
		}
	}
	if got, err := ListenAddress("10.50.50.30:8080"); err != nil || got != "10.50.50.30:8080" {
		t.Fatalf("ListenAddress explicit IPv4 = %q, %v", got, err)
	}
}

func TestCanceledMeasurementDoesNotChangeState(t *testing.T) {
	model, _ := newTestAPI(t)
	input := feederbreaker.MeasurementInput{Schema: 1, DeviceID: "feeder-breaker"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := model.ApplyMeasurements(ctx, input, feederbreaker.AuditContext{})
	if err == nil || model.Snapshot().Measurements.Valid {
		t.Fatalf("canceled measurement err=%v snapshot=%+v", err, model.Snapshot())
	}
}
