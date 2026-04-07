package main

// gps-sim models a GPS-synchronized time server (e.g., SEL-2407,
// Arbiter 1088B, Meinberg LANTIME) in a distribution substation.
//
// GPS clocks provide precise time via IRIG-B (hardwired) and NTP/PTP
// (networked) for:
//   - Sequence of Events (SOE) recording — ms-resolution fault timestamps
//   - Synchrophasor measurement (IEEE C37.118)
//   - Log correlation across devices
//
// Security training relevance:
//   - Time spoofing: shifting the clock corrupts SOE records, making
//     post-incident forensics unreliable
//   - NTP amplification: misconfigured NTP can be used for DDoS
//   - Satellite jamming: GPS signal loss causes holdover degradation
//   - Forensic evasion: attacker offsets time before attack, then
//     restores it — event logs show wrong timestamps

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/tturner/rangerdanger/services/shared"
)

type GPSState struct {
	mu                sync.RWMutex
	SyncStatus        string  `json:"sync_status"`        // "locked", "holdover", "freerun"
	SatelliteCount    int     `json:"satellite_count"`
	TimeOffsetSec     float64 `json:"time_offset_sec"`    // applied offset (spoofing)
	HoldoverStarted   string  `json:"holdover_started"`   // when GPS signal was lost
	HoldoverDriftPPM  float64 `json:"holdover_drift_ppm"` // oscillator drift in holdover
	IRIGB             bool    `json:"irig_b_output"`      // IRIG-B hardwired output active
	NTPEnabled        bool    `json:"ntp_enabled"`
	PTPEnabled        bool    `json:"ptp_enabled"`
	CommsOK           bool    `json:"comms_ok"`
	Alarm             bool    `json:"alarm"`
	LastCommandSource string  `json:"last_command_source"`
}

func (s *GPSState) snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().Add(time.Duration(s.TimeOffsetSec * float64(time.Second)))

	return map[string]any{
		"sync_status":       s.SyncStatus,
		"satellite_count":   s.SatelliteCount,
		"time_offset_sec":   s.TimeOffsetSec,
		"reported_time":     now.Format(time.RFC3339Nano),
		"actual_time":       time.Now().Format(time.RFC3339Nano),
		"holdover_started":  s.HoldoverStarted,
		"holdover_drift_ppm": s.HoldoverDriftPPM,
		"irig_b_output":     s.IRIGB,
		"ntp_enabled":       s.NTPEnabled,
		"ptp_enabled":       s.PTPEnabled,
		"comms_ok":          s.CommsOK,
		"alarm":             s.Alarm,
		"last_command_source": s.LastCommandSource,
	}
}

var (
	state = &GPSState{
		SyncStatus:     "locked",
		SatelliteCount: 9,
		IRIGB:          true,
		NTPEnabled:     true,
		PTPEnabled:     false,
		CommsOK:        true,
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
		Target:  "gps-sim",
		Command: cmd.Command,
	}

	switch cmd.Command {
	case "set_offset":
		// Time spoofing — shift the reported time by N seconds
		state.TimeOffsetSec = cmd.Value
		state.LastCommandSource = source
		state.Alarm = math.Abs(cmd.Value) > 1.0
		entry.Result = "executed"
		entry.Detail = fmt.Sprintf("time offset set to %.3f seconds", cmd.Value)
		if math.Abs(cmd.Value) > 1.0 {
			entry.ProcessImpact = "SOE timestamps will be incorrect — forensic data corrupted"
		}
		log.Printf("TIME OFFSET SET to %.3fs by %s", cmd.Value, source)

	case "reset_offset":
		state.TimeOffsetSec = 0
		state.Alarm = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "time offset reset to 0"

	case "jam_gps":
		// Simulate GPS signal loss — device enters holdover
		state.SyncStatus = "holdover"
		state.SatelliteCount = 0
		state.HoldoverStarted = time.Now().Format(time.RFC3339)
		state.HoldoverDriftPPM = 0.1 // typical OCXO holdover drift
		state.Alarm = true
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "GPS signal jammed — entering holdover mode"
		entry.ProcessImpact = "time accuracy degrading — SOE records may drift"
		log.Printf("GPS JAMMED by %s — holdover started", source)

	case "restore_gps":
		state.SyncStatus = "locked"
		state.SatelliteCount = 9
		state.HoldoverStarted = ""
		state.HoldoverDriftPPM = 0
		state.Alarm = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "GPS signal restored — reacquired lock"

	case "set_satellites":
		count := int(cmd.Value)
		if count < 0 || count > 12 {
			entry.Result = "rejected"
			entry.Detail = "satellite count must be 0-12"
		} else {
			state.SatelliteCount = count
			state.LastCommandSource = source
			if count == 0 {
				state.SyncStatus = "holdover"
				state.HoldoverStarted = time.Now().Format(time.RFC3339)
				state.Alarm = true
			} else if count < 4 {
				state.SyncStatus = "freerun"
				state.Alarm = true
			} else {
				state.SyncStatus = "locked"
				state.Alarm = false
			}
			entry.Result = "executed"
			entry.Detail = fmt.Sprintf("satellite count set to %d, status: %s", count, state.SyncStatus)
		}

	case "enable_ntp":
		state.NTPEnabled = true
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "NTP output enabled"

	case "disable_ntp":
		state.NTPEnabled = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "NTP output disabled"

	case "enable_ptp":
		state.PTPEnabled = true
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "PTP (IEEE 1588) output enabled"

	case "disable_ptp":
		state.PTPEnabled = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "PTP output disabled"

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
	shared.WriteJSON(w, map[string]string{"status": "ok", "service": "gps-sim"})
}

func main() {
	go startModbusServer()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	shared.StartServer("gps-sim", mux)
}
