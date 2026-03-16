package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newOpenDSSMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/update-state", handleUpdateState)
	mux.HandleFunc("GET /api/electrical", handleElectrical)
	mux.HandleFunc("GET /api/health", handleHealth)
	return mux
}

func resetElecState() {
	elec.mu.Lock()
	defer elec.mu.Unlock()
	elec.BreakerClosed = true
	elec.RecloserClosed = true
	elec.RecloseEnabled = true
	elec.RegTapPosition = 0
	elec.FaultOnFeeder = false
	elec.calculate()
}

func getElecMap() map[string]any {
	// Read state directly to avoid the RLock-inside-Lock deadlock in toMap()
	elec.mu.RLock()
	defer elec.mu.RUnlock()
	return map[string]any{
		"substation_bus_voltage_kv": elec.SubstationBusVoltageKV,
		"substation_bus_voltage_v":  elec.SubstationBusVoltageV,
		"downstream_voltage_v":      elec.DownstreamVoltageV,
		"critical_load_voltage_v":   elec.CriticalLoadVoltageV,
		"feeder_current_a":          elec.FeederCurrentA,
		"general_load_energized":    elec.GeneralLoadEnergized,
		"critical_load_energized":   elec.CriticalLoadEnergized,
		"general_load_kw":           elec.GeneralLoadKW,
		"critical_load_kw":          elec.CriticalLoadKW,
		"breaker_closed":            elec.BreakerClosed,
		"recloser_closed":           elec.RecloserClosed,
		"regulator_tap":             elec.RegTapPosition,
	}
}

func setAndCalculate(breakerClosed, recloserClosed bool, tapPosition int) map[string]any {
	elec.mu.Lock()
	elec.BreakerClosed = breakerClosed
	elec.RecloserClosed = recloserClosed
	elec.RegTapPosition = tapPosition
	elec.calculate()
	// Copy results while still holding the lock to avoid race
	result := map[string]any{
		"substation_bus_voltage_kv": elec.SubstationBusVoltageKV,
		"substation_bus_voltage_v":  elec.SubstationBusVoltageV,
		"downstream_voltage_v":      elec.DownstreamVoltageV,
		"critical_load_voltage_v":   elec.CriticalLoadVoltageV,
		"feeder_current_a":          elec.FeederCurrentA,
		"general_load_energized":    elec.GeneralLoadEnergized,
		"critical_load_energized":   elec.CriticalLoadEnergized,
		"general_load_kw":           elec.GeneralLoadKW,
		"critical_load_kw":          elec.CriticalLoadKW,
		"breaker_closed":            elec.BreakerClosed,
		"recloser_closed":           elec.RecloserClosed,
		"regulator_tap":             elec.RegTapPosition,
	}
	elec.mu.Unlock()
	return result
}

func TestOpenDSSHealth(t *testing.T) {
	resetElecState()
	ts := httptest.NewServer(newOpenDSSMux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOpenDSSInitialState(t *testing.T) {
	resetElecState()
	result := getElecMap()

	if result["breaker_closed"] != true {
		t.Errorf("expected breaker_closed=true, got %v", result["breaker_closed"])
	}
	if result["recloser_closed"] != true {
		t.Errorf("expected recloser_closed=true, got %v", result["recloser_closed"])
	}
	if result["general_load_energized"] != true {
		t.Errorf("expected general_load_energized=true, got %v", result["general_load_energized"])
	}
	if result["critical_load_energized"] != true {
		t.Errorf("expected critical_load_energized=true, got %v", result["critical_load_energized"])
	}
	if result["substation_bus_voltage_kv"] != 12.47 {
		t.Errorf("expected substation_bus_voltage_kv=12.47, got %v", result["substation_bus_voltage_kv"])
	}
}

func TestOpenDSSBreakerOpen(t *testing.T) {
	resetElecState()
	result := setAndCalculate(false, true, 0)

	if result["breaker_closed"] != false {
		t.Errorf("expected breaker_closed=false, got %v", result["breaker_closed"])
	}
	if result["general_load_energized"] != false {
		t.Errorf("expected general_load_energized=false when breaker open, got %v", result["general_load_energized"])
	}
	if result["critical_load_energized"] != false {
		t.Errorf("expected critical_load_energized=false when breaker open, got %v", result["critical_load_energized"])
	}
	if result["downstream_voltage_v"] != float64(0) {
		t.Errorf("expected downstream_voltage_v=0 when breaker open, got %v", result["downstream_voltage_v"])
	}
	if result["feeder_current_a"] != float64(0) {
		t.Errorf("expected feeder_current_a=0 when breaker open, got %v", result["feeder_current_a"])
	}
}

func TestOpenDSSRecloserOpen(t *testing.T) {
	resetElecState()
	result := setAndCalculate(true, false, 0)

	if result["general_load_energized"] != false {
		t.Errorf("expected general_load_energized=false when recloser open, got %v", result["general_load_energized"])
	}
	if result["critical_load_energized"] != false {
		t.Errorf("expected critical_load_energized=false when recloser open, got %v", result["critical_load_energized"])
	}
	if result["downstream_voltage_v"] != float64(0) {
		t.Errorf("expected downstream_voltage_v=0 when recloser open, got %v", result["downstream_voltage_v"])
	}
}

func TestOpenDSSRegulatorTapAffectsVoltage(t *testing.T) {
	resetElecState()

	// Baseline with tap=0
	baseline := setAndCalculate(true, true, 0)
	baselineVoltage := baseline["critical_load_voltage_v"].(float64)

	// Raise tap to +5
	raised := setAndCalculate(true, true, 5)
	raisedVoltage := raised["critical_load_voltage_v"].(float64)

	if raisedVoltage <= baselineVoltage {
		t.Errorf("expected critical load voltage to increase with positive tap, baseline=%.2f raised=%.2f", baselineVoltage, raisedVoltage)
	}

	// Each tap = 0.75V, so 5 taps = 3.75V increase
	expectedDiff := 5 * 0.75
	actualDiff := raisedVoltage - baselineVoltage
	if actualDiff < expectedDiff-0.01 || actualDiff > expectedDiff+0.01 {
		t.Errorf("expected voltage increase of %.2f, got %.2f", expectedDiff, actualDiff)
	}
}

func TestOpenDSSAllEnergizedNormal(t *testing.T) {
	resetElecState()
	result := setAndCalculate(true, true, 0)

	if result["general_load_energized"] != true {
		t.Errorf("expected general_load_energized=true")
	}
	if result["critical_load_energized"] != true {
		t.Errorf("expected critical_load_energized=true")
	}
	if result["general_load_kw"] != 500.0 {
		t.Errorf("expected general_load_kw=500.0, got %v", result["general_load_kw"])
	}
	if result["critical_load_kw"] != 200.0 {
		t.Errorf("expected critical_load_kw=200.0, got %v", result["critical_load_kw"])
	}
}
