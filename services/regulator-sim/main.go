package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/tturner/rangerdanger/services/shared"
)

type RegulatorState struct {
	mu                sync.RWMutex
	TapPosition       int     `json:"tap_position"`
	VoltageSetpoint   float64 `json:"voltage_setpoint_v"`
	ManualMode        bool    `json:"manual_mode"`
	CommsOK           bool    `json:"comms_ok"`
	Alarm             bool    `json:"alarm"`
	LastCommandSource string  `json:"last_command_source"`
}

const (
	minTap = -16
	maxTap = 16
	// Each tap step changes voltage by ~0.625% of nominal 120V ≈ 0.75V
	voltsPerTap = 0.75
)

func (s *RegulatorState) snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"tap_position":        s.TapPosition,
		"voltage_setpoint_v":  s.VoltageSetpoint,
		"manual_mode":         s.ManualMode,
		"comms_ok":            s.CommsOK,
		"alarm":               s.Alarm,
		"last_command_source": s.LastCommandSource,
		"voltage_offset_v":    float64(s.TapPosition) * voltsPerTap,
	}
}

var (
	state = &RegulatorState{
		TapPosition:     0,
		VoltageSetpoint: 120.0,
		CommsOK:         true,
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
		Target:  "regulator-sim",
		Command: cmd.Command,
	}

	switch cmd.Command {
	case "raise_tap":
		if state.TapPosition >= maxTap {
			entry.Result = "rejected"
			entry.Detail = "already at max tap"
		} else {
			state.TapPosition++
			state.LastCommandSource = source
			entry.Result = "executed"
			entry.Detail = "tap raised"
			log.Printf("RAISE TAP from %s -> position %d", source, state.TapPosition)
		}
	case "lower_tap":
		if state.TapPosition <= minTap {
			entry.Result = "rejected"
			entry.Detail = "already at min tap"
		} else {
			state.TapPosition--
			state.LastCommandSource = source
			entry.Result = "executed"
			entry.Detail = "tap lowered"
			log.Printf("LOWER TAP from %s -> position %d", source, state.TapPosition)
		}
	case "set_manual":
		state.ManualMode = true
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "manual mode enabled"
	case "set_auto":
		state.ManualMode = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "auto mode enabled"
	case "set_setpoint":
		state.VoltageSetpoint = cmd.Value
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "setpoint updated"
	case "set_tap":
		tap := int(cmd.Value)
		if tap < minTap || tap > maxTap {
			entry.Result = "rejected"
			entry.Detail = "tap out of range"
		} else {
			state.TapPosition = tap
			state.LastCommandSource = source
			entry.Result = "executed"
			entry.Detail = "tap set directly"
			log.Printf("SET TAP from %s -> position %d", source, state.TapPosition)
		}
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
	shared.WriteJSON(w, map[string]string{"status": "ok", "service": "regulator-sim"})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	shared.StartServer("regulator-sim", mux)
}
