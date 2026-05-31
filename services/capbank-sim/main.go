package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/tturner/rangerdanger/services/shared"
)

// CapbankState models a switched capacitor bank for reactive power support.
// In distribution substations, cap banks are switched in/out to maintain
// power factor and voltage during heavy load periods.
type CapbankState struct {
	mu                sync.RWMutex
	SwitchedIn        bool    `json:"switched_in"`
	AutoMode          bool    `json:"auto_mode"`
	KvarRating        float64 `json:"kvar_rating"`
	VoltageThreshLow  float64 `json:"voltage_thresh_low_v"`
	VoltageThreshHigh float64 `json:"voltage_thresh_high_v"`
	CommsOK           bool    `json:"comms_ok"`
	Alarm             bool    `json:"alarm"`
	SwitchCount       int     `json:"switch_count"`
	Lockout           bool    `json:"lockout"`
	LastCommandSource string  `json:"last_command_source"`
}

const maxSwitchCount = 6 // lockout after 6 operations (contacts wear)

func (s *CapbankState) snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"switched_in":          s.SwitchedIn,
		"auto_mode":            s.AutoMode,
		"kvar_rating":          s.KvarRating,
		"voltage_thresh_low_v": s.VoltageThreshLow,
		"voltage_thresh_high_v": s.VoltageThreshHigh,
		"comms_ok":             s.CommsOK,
		"alarm":                s.Alarm,
		"switch_count":         s.SwitchCount,
		"lockout":              s.Lockout,
		"last_command_source":  s.LastCommandSource,
	}
}

var (
	state = &CapbankState{
		SwitchedIn:        false,
		AutoMode:          true,
		KvarRating:        300.0, // 300 kVAR bank
		VoltageThreshLow:  114.0,
		VoltageThreshHigh: 126.0,
		CommsOK:           true,
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
		Target:  "capbank-sim",
		Command: cmd.Command,
	}

	switch cmd.Command {
	case "switch_in":
		if state.Lockout {
			entry.Result = "rejected"
			entry.Detail = "capbank in lockout — reset required"
		} else if state.SwitchedIn {
			// Already in the commanded position. Report an idempotent
			// success (no physical switch, so switch count is unchanged)
			// rather than a rejection: a redundant "switch in" has already
			// reached its goal state. This keeps the workshop reset (which
			// energizes the bank to restore default state) from reporting
			// overall failure whenever the bank is already in -- the common
			// case on a freshly reset stack, where every other reset command
			// is idempotent but this one used to fail the whole Reset Lab.
			entry.Result = "executed"
			entry.Detail = "already switched in (no-op)"
		} else {
			state.SwitchedIn = true
			state.SwitchCount++
			state.LastCommandSource = source
			if state.SwitchCount >= maxSwitchCount {
				state.Lockout = true
				state.Alarm = true
				entry.ProcessImpact = "capbank locked out — switch count exceeded"
			}
			entry.Result = "executed"
			entry.Detail = fmt.Sprintf("capacitor bank energized (switch count: %d)", state.SwitchCount)
			log.Printf("SWITCH IN from %s (count: %d)", source, state.SwitchCount)
		}

	case "switch_out":
		if state.Lockout {
			entry.Result = "rejected"
			entry.Detail = "capbank in lockout — reset required"
		} else if !state.SwitchedIn {
			entry.Result = "rejected"
			entry.Detail = "already switched out"
		} else {
			state.SwitchedIn = false
			state.SwitchCount++
			state.LastCommandSource = source
			if state.SwitchCount >= maxSwitchCount {
				state.Lockout = true
				state.Alarm = true
				entry.ProcessImpact = "capbank locked out — switch count exceeded"
			}
			entry.Result = "executed"
			entry.Detail = fmt.Sprintf("capacitor bank de-energized (switch count: %d)", state.SwitchCount)
			log.Printf("SWITCH OUT from %s (count: %d)", source, state.SwitchCount)
		}

	case "set_auto":
		state.AutoMode = true
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "auto mode enabled"

	case "set_manual":
		state.AutoMode = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "manual mode enabled"

	case "reset_lockout":
		state.Lockout = false
		state.Alarm = false
		state.SwitchCount = 0
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "lockout reset, switch count cleared"
		log.Printf("LOCKOUT RESET from %s", source)

	case "set_thresh_low":
		state.VoltageThreshLow = cmd.Value
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = fmt.Sprintf("low voltage threshold set to %.1fV", cmd.Value)

	case "set_thresh_high":
		state.VoltageThreshHigh = cmd.Value
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = fmt.Sprintf("high voltage threshold set to %.1fV", cmd.Value)

	default:
		entry.Result = "rejected"
		entry.Detail = "unknown command"
	}

	audit.Add(entry)
	shared.WriteJSON(w, map[string]any{"result": entry.Result, "detail": entry.Detail})
}

func handleAudit(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, map[string]any{"entries": audit.Entries()})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, map[string]string{"status": "ok", "service": "capbank-sim"})
}

func main() {
	go startModbusServer()
	go startDNP3Server()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	shared.StartServer("capbank-sim", mux)
}
