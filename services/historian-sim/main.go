package main

// historian-sim models an OT data historian in a distribution substation.
//
// In real substations, historians (PI System, eDNA, SEL-3530 built-in)
// collect time-series data from RTUs/RTACs and store it for trending,
// post-event analysis, and compliance reporting. They typically have
// read-only access to SCADA data — but some are configured with
// "write-back" capability for setpoint injection, which is a significant
// security concern.
//
// Security training relevance:
//   - Read-only access: attacker sees all operational data (voltage,
//     current, breaker state) without touching field devices
//   - Write-back mode: if enabled, historian becomes a pivot point —
//     attacker can inject false setpoints through it
//   - Data integrity: attacker can corrupt historical records to
//     hide evidence of manipulation

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/tturner/rangerdanger/services/shared"
)

type HistorianState struct {
	mu               sync.RWMutex
	Recording        bool    `json:"recording"`
	WriteBackEnabled bool    `json:"write_back_enabled"`
	PointCount       int     `json:"point_count"`
	RtacEndpoint     string  `json:"rtac_endpoint"`
	PollIntervalSec  int     `json:"poll_interval_sec"`
	CommsOK          bool    `json:"comms_ok"`
	LastPollTime     string  `json:"last_poll_time"`
	LastReading      any     `json:"last_reading"`
	History          []Point `json:"history"`
}

type Point struct {
	Timestamp           string  `json:"timestamp"`
	SubstationVoltageV  float64 `json:"substation_voltage_v"`
	DownstreamVoltageV  float64 `json:"downstream_voltage_v"`
	FeederCurrentA      float64 `json:"feeder_current_a"`
	GeneralLoadKw       float64 `json:"general_load_kw"`
	CriticalLoadKw      float64 `json:"critical_load_kw"`
	BreakerClosed       bool    `json:"breaker_closed"`
	RecloserClosed      bool    `json:"recloser_closed"`
	TotalLossesKw       float64 `json:"total_losses_kw"`
	PowerFactor         float64 `json:"power_factor"`
}

const maxHistory = 500 // ring buffer size

func (s *HistorianState) snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"recording":          s.Recording,
		"write_back_enabled": s.WriteBackEnabled,
		"point_count":        s.PointCount,
		"rtac_endpoint":      s.RtacEndpoint,
		"poll_interval_sec":  s.PollIntervalSec,
		"comms_ok":           s.CommsOK,
		"last_poll_time":     s.LastPollTime,
		"last_reading":       s.LastReading,
	}
}

var (
	state = &HistorianState{
		Recording:       true,
		PollIntervalSec: 5,
		RtacEndpoint:    "http://10.30.30.20:8080",
		CommsOK:         true,
	}
	audit = shared.NewAuditLog(100)
)

func init() {
	if ep := os.Getenv("RTAC_ENDPOINT"); ep != "" {
		state.RtacEndpoint = ep
	}
}

func handleState(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, state.snapshot())
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	shared.WriteJSON(w, map[string]any{
		"point_count": state.PointCount,
		"points":      state.History,
	})
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
		Target:  "historian-sim",
		Command: cmd.Command,
	}

	switch cmd.Command {
	case "start_recording":
		state.Recording = true
		entry.Result = "executed"
		entry.Detail = "recording started"

	case "stop_recording":
		state.Recording = false
		entry.Result = "executed"
		entry.Detail = "recording stopped"

	case "enable_write_back":
		state.WriteBackEnabled = true
		entry.Result = "executed"
		entry.Detail = "WRITE-BACK ENABLED — historian can now inject setpoints"
		entry.ProcessImpact = "historian becomes a control path — security risk"
		log.Printf("WARNING: write-back enabled by %s", source)

	case "disable_write_back":
		state.WriteBackEnabled = false
		entry.Result = "executed"
		entry.Detail = "write-back disabled — read-only mode"

	case "clear_history":
		state.History = nil
		state.PointCount = 0
		entry.Result = "executed"
		entry.Detail = "history cleared — all records deleted"
		entry.ProcessImpact = "historical data destroyed — forensic evidence lost"
		log.Printf("WARNING: history cleared by %s", source)

	case "set_poll_interval":
		sec := int(cmd.Value)
		if sec < 1 || sec > 60 {
			entry.Result = "rejected"
			entry.Detail = "interval must be 1-60 seconds"
		} else {
			state.PollIntervalSec = sec
			entry.Result = "executed"
			entry.Detail = fmt.Sprintf("poll interval set to %ds", sec)
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
	shared.WriteJSON(w, map[string]string{"status": "ok", "service": "historian-sim"})
}

// pollRTAC periodically fetches substation state from the RTAC.
func pollRTAC() {
	client := &http.Client{Timeout: 3 * time.Second}

	for {
		state.mu.RLock()
		interval := state.PollIntervalSec
		recording := state.Recording
		endpoint := state.RtacEndpoint
		state.mu.RUnlock()

		if recording {
			resp, err := client.Get(endpoint + "/api/substation/state")
			if err != nil {
				state.mu.Lock()
				state.CommsOK = false
				state.mu.Unlock()
			} else {
				var data map[string]any
				json.NewDecoder(resp.Body).Decode(&data)
				resp.Body.Close()

				state.mu.Lock()
				state.CommsOK = true
				state.LastPollTime = time.Now().Format(time.RFC3339)
				state.LastReading = data

				// Extract electrical values for time-series storage
				if elec, ok := data["electrical"].(map[string]any); ok {
					pt := Point{
						Timestamp: state.LastPollTime,
					}
					if v, ok := elec["substation_bus_voltage_v"].(float64); ok {
						pt.SubstationVoltageV = v
					}
					if v, ok := elec["downstream_voltage_v"].(float64); ok {
						pt.DownstreamVoltageV = v
					}
					if v, ok := elec["feeder_current_a"].(float64); ok {
						pt.FeederCurrentA = v
					}
					if v, ok := elec["general_load_kw"].(float64); ok {
						pt.GeneralLoadKw = v
					}
					if v, ok := elec["critical_load_kw"].(float64); ok {
						pt.CriticalLoadKw = v
					}
					if v, ok := elec["breaker_closed"].(bool); ok {
						pt.BreakerClosed = v
					}
					if v, ok := elec["recloser_closed"].(bool); ok {
						pt.RecloserClosed = v
					}
					if v, ok := elec["total_losses_kw"].(float64); ok {
						pt.TotalLossesKw = v
					}
					if v, ok := elec["power_factor"].(float64); ok {
						pt.PowerFactor = v
					}

					state.History = append(state.History, pt)
					if len(state.History) > maxHistory {
						state.History = state.History[len(state.History)-maxHistory:]
					}
					state.PointCount = len(state.History)
				}
				state.mu.Unlock()
			}
		}

		time.Sleep(time.Duration(interval) * time.Second)
	}
}

func main() {
	go pollRTAC()
	go startModbusServer()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("GET /api/history", handleHistory)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	shared.StartServer("historian-sim", mux)
}
