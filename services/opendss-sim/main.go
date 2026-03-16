package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/tturner/rangerdanger/services/shared"
)

// Simplified feeder model:
//
//   Substation Source (infinite bus at 12.47 kV / 120V secondary)
//       |
//   [Feeder Breaker / Relay]
//       |
//   Main Feeder Segment
//       |
//   [Mid-feeder Recloser]
//       |--- Branch A: General Load (500 kW)
//       |--- Branch B: Critical Load (200 kW) + Voltage Regulator

const (
	nominalVoltageKV    = 12.47
	nominalVoltageV     = 120.0
	generalLoadKW       = 500.0
	criticalLoadKW      = 200.0
	totalLoadKW         = generalLoadKW + criticalLoadKW
	// Simplified: current = power / (sqrt(3) * voltage)
	nominalCurrentA     = totalLoadKW / (1.732 * nominalVoltageKV)
)

type ElectricalState struct {
	mu sync.RWMutex

	// Inputs from device state
	BreakerClosed  bool
	RecloserClosed bool
	RecloseEnabled bool
	RegTapPosition int
	FaultOnFeeder  bool

	// Calculated outputs
	SubstationBusVoltageKV float64
	SubstationBusVoltageV  float64
	DownstreamVoltageV     float64
	CriticalLoadVoltageV   float64
	FeederCurrentA         float64
	GeneralLoadEnergized   bool
	CriticalLoadEnergized  bool
	GeneralLoadKW          float64
	CriticalLoadKW         float64
}

var elec = &ElectricalState{
	BreakerClosed:  true,
	RecloserClosed: true,
	RecloseEnabled: true,
}

func (e *ElectricalState) calculate() {
	// Substation bus is always energized (infinite source)
	e.SubstationBusVoltageKV = nominalVoltageKV
	e.SubstationBusVoltageV = nominalVoltageV

	if !e.BreakerClosed {
		// Feeder breaker open: everything downstream is de-energized
		e.DownstreamVoltageV = 0
		e.CriticalLoadVoltageV = 0
		e.FeederCurrentA = 0
		e.GeneralLoadEnergized = false
		e.CriticalLoadEnergized = false
		e.GeneralLoadKW = 0
		e.CriticalLoadKW = 0
		return
	}

	if !e.RecloserClosed {
		// Recloser open: section downstream of recloser is de-energized
		// Main feeder section up to recloser still energized but unloaded
		e.DownstreamVoltageV = 0
		e.CriticalLoadVoltageV = 0
		e.FeederCurrentA = 0 // Simplified: no load upstream of recloser
		e.GeneralLoadEnergized = false
		e.CriticalLoadEnergized = false
		e.GeneralLoadKW = 0
		e.CriticalLoadKW = 0
		return
	}

	// Both breaker and recloser closed: feeder energized
	e.GeneralLoadEnergized = true
	e.CriticalLoadEnergized = true
	e.GeneralLoadKW = generalLoadKW
	e.CriticalLoadKW = criticalLoadKW

	// Voltage at downstream bus (slight drop due to feeder impedance)
	// Simplified: 2% voltage drop along feeder
	e.DownstreamVoltageV = nominalVoltageV * 0.98

	// Regulator effect on critical load voltage
	// Each tap = 0.625% of nominal = 0.75V
	voltsPerTap := 0.75
	e.CriticalLoadVoltageV = e.DownstreamVoltageV + float64(e.RegTapPosition)*voltsPerTap

	// Current based on total load
	e.FeederCurrentA = nominalCurrentA

	// If regulator tap is extreme, may cause alarm conditions
	if e.CriticalLoadVoltageV < 108.0 || e.CriticalLoadVoltageV > 132.0 {
		// Voltage out of ANSI C84.1 Range A
		log.Printf("WARNING: critical load voltage %.1fV outside normal range", e.CriticalLoadVoltageV)
	}
}

func (e *ElectricalState) toMap() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.toMapLocked()
}

// toMapLocked returns the state map without acquiring the lock (caller must hold it).
func (e *ElectricalState) toMapLocked() map[string]any {
	return map[string]any{
		"substation_bus_voltage_kv": e.SubstationBusVoltageKV,
		"substation_bus_voltage_v":  e.SubstationBusVoltageV,
		"downstream_voltage_v":     e.DownstreamVoltageV,
		"critical_load_voltage_v":  e.CriticalLoadVoltageV,
		"feeder_current_a":         e.FeederCurrentA,
		"general_load_energized":   e.GeneralLoadEnergized,
		"critical_load_energized":  e.CriticalLoadEnergized,
		"general_load_kw":          e.GeneralLoadKW,
		"critical_load_kw":         e.CriticalLoadKW,
		"breaker_closed":           e.BreakerClosed,
		"recloser_closed":          e.RecloserClosed,
		"regulator_tap":            e.RegTapPosition,
	}
}

// handleUpdateState receives device state from rtac-sim, recalculates physics.
func handleUpdateState(w http.ResponseWriter, r *http.Request) {
	var devices map[string]map[string]any
	if err := shared.ReadJSON(r, &devices); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	elec.mu.Lock()

	// Extract device states
	if relay, ok := devices["relay"]; ok {
		if v, ok := relay["breaker_closed"].(bool); ok {
			elec.BreakerClosed = v
		}
	}
	if recloser, ok := devices["recloser"]; ok {
		if v, ok := recloser["closed"].(bool); ok {
			elec.RecloserClosed = v
		}
		if v, ok := recloser["reclose_enabled"].(bool); ok {
			elec.RecloseEnabled = v
		}
		if v, ok := recloser["fault_seen"].(bool); ok {
			elec.FaultOnFeeder = v
		}
	}
	if regulator, ok := devices["regulator"]; ok {
		if v, ok := regulator["tap_position"].(float64); ok {
			elec.RegTapPosition = int(v)
		}
	}

	elec.calculate()
	result := elec.toMapLocked()
	elec.mu.Unlock()

	shared.WriteJSON(w, result)
}

// handleElectrical returns current electrical state.
func handleElectrical(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, elec.toMap())
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, map[string]string{"status": "ok", "service": "opendss-sim"})
}

func main() {
	// Initialize with default calculation
	elec.mu.Lock()
	elec.calculate()
	elec.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/update-state", handleUpdateState)
	mux.HandleFunc("GET /api/electrical", handleElectrical)
	mux.HandleFunc("GET /api/health", handleHealth)
	shared.StartServer("opendss-sim", mux)
}
