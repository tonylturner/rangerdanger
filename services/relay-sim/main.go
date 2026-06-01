package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/tturner/rangerdanger/services/shared"
)

type RelayState struct {
	mu                   sync.RWMutex
	BreakerClosed        bool    `json:"breaker_closed"`
	RemoteControlEnabled bool    `json:"remote_control_enabled"`
	Lockout              bool    `json:"lockout"`
	FaultSeen            bool    `json:"fault_seen"`
	MeasuredCurrent      float64 `json:"measured_current_a"`
	MeasuredVoltage      float64 `json:"measured_voltage_kv"`
	CommsOK              bool    `json:"comms_ok"`
	LastCommandSource    string  `json:"last_command_source"`
}

func (s *RelayState) snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"breaker_closed":         s.BreakerClosed,
		"remote_control_enabled": s.RemoteControlEnabled,
		"lockout":                s.Lockout,
		"fault_seen":             s.FaultSeen,
		"measured_current_a":     s.MeasuredCurrent,
		"measured_voltage_kv":    s.MeasuredVoltage,
		"comms_ok":               s.CommsOK,
		"last_command_source":    s.LastCommandSource,
	}
}

var (
	state = &RelayState{
		BreakerClosed:        true,
		RemoteControlEnabled: true,
		CommsOK:              true,
		MeasuredCurrent:      120.0,
		MeasuredVoltage:      12.47,
	}
	audit = shared.NewAuditLog(100)
)

func handleState(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, state.snapshot())
}

func handleCommand(w http.ResponseWriter, r *http.Request) {
	var cmd shared.CommandRequest
	if err := shared.ReadJSON(r, &cmd); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	source := shared.SourceFromRequest(r, cmd)

	state.mu.Lock()
	defer state.mu.Unlock()

	entry := shared.AuditEntry{
		Source:  source,
		Target:  "relay-sim",
		Command: cmd.Command,
	}

	switch cmd.Command {
	case "trip":
		if !state.RemoteControlEnabled {
			entry.Result = "rejected"
			entry.Detail = "remote control disabled"
		} else if state.Lockout {
			entry.Result = "rejected"
			entry.Detail = "lockout active"
		} else {
			state.BreakerClosed = false
			state.LastCommandSource = source
			entry.Result = "executed"
			entry.Detail = "breaker opened"
			log.Printf("TRIP executed from %s - breaker OPEN", source)
		}
	case "close":
		if !state.RemoteControlEnabled {
			entry.Result = "rejected"
			entry.Detail = "remote control disabled"
		} else if state.Lockout {
			entry.Result = "rejected"
			entry.Detail = "lockout active"
		} else {
			state.BreakerClosed = true
			state.LastCommandSource = source
			entry.Result = "executed"
			entry.Detail = "breaker closed"
			log.Printf("CLOSE executed from %s - breaker CLOSED", source)
		}
	case "lockout":
		state.Lockout = true
		state.BreakerClosed = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "lockout engaged, breaker open"
	case "unlock":
		state.Lockout = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "lockout cleared"
	case "inject_fault":
		// A feeder fault is seen by the 50/51 overcurrent element, which trips
		// the 52 breaker instantaneously (protection is local — it operates
		// regardless of remote-control state). The breaker has no auto-reclose,
		// so the feeder stays dark until the fault is cleared and the breaker is
		// manually closed: a sustained, total outage, distinct from the
		// recloser's mid-feeder trip-and-reclose sequence.
		state.FaultSeen = true
		state.LastCommandSource = source
		entry.Result = "executed"
		if state.BreakerClosed {
			state.BreakerClosed = false
			entry.Detail = "fault injected — overcurrent trip, breaker opened"
			log.Printf("INJECT FAULT from %s — overcurrent trip, breaker OPEN", source)
		} else {
			entry.Detail = "fault injected (breaker already open)"
		}
	case "clear_fault":
		state.FaultSeen = false
		entry.Result = "executed"
		entry.Detail = "fault cleared"
	case "set_current":
		state.MeasuredCurrent = cmd.Value
		entry.Result = "executed"
	case "set_voltage":
		state.MeasuredVoltage = cmd.Value
		entry.Result = "executed"
	default:
		entry.Result = "rejected"
		entry.Detail = "unknown command"
	}

	audit.Add(entry)

	shared.WriteJSON(w, map[string]any{
		"result": entry.Result,
		"detail": entry.Detail,
	})
}

func handleAudit(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, map[string]any{"entries": audit.Entries()})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, map[string]string{"status": "ok", "service": "relay-sim"})
}

func main() {
	go startModbusServer()
	go startDNP3Server()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	shared.StartServer("relay-sim", mux)
}
