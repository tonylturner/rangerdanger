package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/tturner/rangerdanger/services/shared"
)

// DeviceEndpoint defines a field device to poll.
type DeviceEndpoint struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// AggregatedState holds all polled device states plus electrical calculations.
type AggregatedState struct {
	mu          sync.RWMutex
	Devices     map[string]map[string]any `json:"devices"`
	Electrical  map[string]any            `json:"electrical"`
	LastPoll    time.Time                 `json:"last_poll"`
	DeviceComms map[string]bool           `json:"device_comms"`
}

var (
	agg = &AggregatedState{
		Devices:     make(map[string]map[string]any),
		Electrical:  make(map[string]any),
		DeviceComms: make(map[string]bool),
	}
	audit = shared.NewAuditLog(500)

	fieldDevices []DeviceEndpoint
	physicsURL   string
)

func init() {
	relayHost := envOr("RELAY_SIM_HOST", "relay-sim:8080")
	recloserHost := envOr("RECLOSER_SIM_HOST", "recloser-sim:8080")
	regulatorHost := envOr("REGULATOR_SIM_HOST", "regulator-sim:8080")
	physicsURL = "http://" + envOr("OPENDSS_SIM_HOST", "opendss-sim:8080")

	fieldDevices = []DeviceEndpoint{
		{Name: "relay", URL: fmt.Sprintf("http://%s", relayHost)},
		{Name: "recloser", URL: fmt.Sprintf("http://%s", recloserHost)},
		{Name: "regulator", URL: fmt.Sprintf("http://%s", regulatorHost)},
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// pollDevices periodically fetches state from all field device simulators
// and the physics engine.
func pollDevices() {
	client := &http.Client{Timeout: 3 * time.Second}
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		agg.mu.Lock()

		for _, dev := range fieldDevices {
			resp, err := client.Get(dev.URL + "/api/state")
			if err != nil {
				agg.DeviceComms[dev.Name] = false
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var state map[string]any
			if json.Unmarshal(body, &state) == nil {
				agg.Devices[dev.Name] = state
				agg.DeviceComms[dev.Name] = true
			}
		}

		// Send state to physics engine and get electrical calculations
		statePayload, _ := json.Marshal(agg.Devices)
		resp, err := client.Post(physicsURL+"/api/update-state", "application/json", bytes.NewReader(statePayload))
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var elec map[string]any
			if json.Unmarshal(body, &elec) == nil {
				agg.Electrical = elec
			}
		}

		agg.LastPoll = time.Now()
		agg.mu.Unlock()
	}
}

// handleTags returns all aggregated tags in a flat namespace.
func handleTags(w http.ResponseWriter, r *http.Request) {
	agg.mu.RLock()
	defer agg.mu.RUnlock()

	tags := make(map[string]any)

	if relay, ok := agg.Devices["relay"]; ok {
		for k, v := range relay {
			tags["substation.breaker."+k] = v
		}
	}
	if recloser, ok := agg.Devices["recloser"]; ok {
		for k, v := range recloser {
			tags["feeder.recloser."+k] = v
		}
	}
	if regulator, ok := agg.Devices["regulator"]; ok {
		for k, v := range regulator {
			tags["feeder.regulator."+k] = v
		}
	}

	for k, v := range agg.Electrical {
		tags["electrical."+k] = v
	}
	for dev, ok := range agg.DeviceComms {
		tags["comms."+dev+".ok"] = ok
	}

	// Alarms
	tags["alarm.comm_loss"] = hasCommLoss()
	tags["alarm.breaker_open_unexpected"] = isBreakerOpenUnexpected()
	tags["alarm.reclose_disabled"] = isRecloseDisabled()
	tags["alarm.low_voltage_critical"] = isLowVoltageCritical()
	tags["alarm.high_voltage_critical"] = isHighVoltageCritical()

	shared.WriteJSON(w, map[string]any{
		"tags":      tags,
		"last_poll": agg.LastPoll,
	})
}

// handleRawState returns the raw device state (not flattened).
func handleRawState(w http.ResponseWriter, r *http.Request) {
	agg.mu.RLock()
	defer agg.mu.RUnlock()
	shared.WriteJSON(w, map[string]any{
		"devices":      agg.Devices,
		"electrical":   agg.Electrical,
		"device_comms": agg.DeviceComms,
		"last_poll":    agg.LastPoll,
	})
}

// handleCommand forwards a command to the appropriate field device.
// It also computes the process impact of the command for audit correlation.
func handleCommand(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("device")
	var cmd shared.CommandRequest
	if err := shared.ReadJSON(r, &cmd); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	source := shared.SourceFromRequest(r, cmd)
	cmd.Source = source

	var targetURL string
	for _, dev := range fieldDevices {
		if dev.Name == device {
			targetURL = dev.URL
			break
		}
	}
	if targetURL == "" {
		http.Error(w, "unknown device: "+device, http.StatusNotFound)
		return
	}

	entry := shared.AuditEntry{
		Source:     source,
		SourceZone: shared.ClassifyZone(source),
		Target:     device,
		Command:    cmd.Command,
	}

	// Forward command
	cmdJSON, _ := json.Marshal(cmd)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(targetURL+"/api/command", "application/json", bytes.NewReader(cmdJSON))
	if err != nil {
		entry.Result = "error"
		entry.Detail = err.Error()
		entry.ProcessImpact = "command delivery failed — no process change"
		audit.Add(entry)
		http.Error(w, "command forwarding failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result)

	entry.Result = fmt.Sprintf("%v", result["result"])
	entry.Detail = fmt.Sprintf("%v", result["detail"])
	entry.ProcessImpact = deriveProcessImpact(device, cmd.Command, entry.Result, entry.SourceZone)

	audit.Add(entry)

	log.Printf("CMD [%s] %s -> %s/%s = %s | impact: %s",
		entry.SourceZone, source, device, cmd.Command, entry.Result, entry.ProcessImpact)

	shared.WriteJSON(w, map[string]any{
		"result":         result["result"],
		"detail":         result["detail"],
		"process_impact": entry.ProcessImpact,
		"source_zone":    entry.SourceZone,
	})
}

// deriveProcessImpact describes what a command means for the physical process.
func deriveProcessImpact(device, command, result, sourceZone string) string {
	if result != "executed" {
		return "no process change — command " + result
	}

	// Read current electrical state for context
	agg.mu.RLock()
	genKW := floatFromMap(agg.Electrical, "general_load_kw")
	critKW := floatFromMap(agg.Electrical, "critical_load_kw")
	totalKW := genKW + critKW
	agg.mu.RUnlock()

	zoneLabel := ""
	if sourceZone != "ot_ops" && sourceZone != "operator" {
		zoneLabel = fmt.Sprintf(" [from %s zone]", sourceZone)
	}

	switch device + "/" + command {
	case "relay/trip":
		return fmt.Sprintf("feeder breaker OPENED — %.0f kW load de-energized, all downstream customers without power%s", totalKW, zoneLabel)
	case "relay/close":
		return "feeder breaker CLOSED — downstream loads re-energized"
	case "relay/lockout":
		return fmt.Sprintf("breaker LOCKED OUT — %.0f kW offline, manual intervention required to restore%s", totalKW, zoneLabel)
	case "relay/inject_fault":
		return "fault condition injected on feeder — protection will operate"
	case "recloser/disable_reclose":
		return fmt.Sprintf("auto-reclose DISABLED — next fault will cause sustained outage (%.0f kW at risk)%s", totalKW, zoneLabel)
	case "recloser/enable_reclose":
		return "auto-reclose ENABLED — transient faults will self-clear"
	case "recloser/open":
		return fmt.Sprintf("recloser OPENED — %.0f kW downstream load de-energized%s", totalKW, zoneLabel)
	case "recloser/close":
		return "recloser CLOSED — downstream loads re-energized"
	case "recloser/inject_fault":
		return "fault injected — recloser will trip and attempt auto-reclose sequence"
	case "regulator/set_tap":
		return fmt.Sprintf("voltage regulator tap changed — critical load voltage will shift%s", zoneLabel)
	case "regulator/raise_tap":
		return "tap RAISED — critical load voltage increases ~0.75V"
	case "regulator/lower_tap":
		return "tap LOWERED — critical load voltage decreases ~0.75V"
	case "regulator/set_manual":
		return fmt.Sprintf("regulator set to MANUAL — automatic voltage regulation disabled%s", zoneLabel)
	case "regulator/set_auto":
		return "regulator set to AUTO — automatic voltage regulation active"
	}

	return "command executed"
}

func floatFromMap(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

func handleAudit(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, map[string]any{"entries": audit.Entries()})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	agg.mu.RLock()
	comms := make(map[string]bool, len(agg.DeviceComms))
	for k, v := range agg.DeviceComms {
		comms[k] = v
	}
	lastPoll := agg.LastPoll
	agg.mu.RUnlock()

	shared.WriteJSON(w, map[string]any{
		"status":       "ok",
		"service":      "rtac-sim",
		"device_comms": comms,
		"last_poll":    lastPoll,
	})
}

// Alarm derivation helpers
func hasCommLoss() bool {
	for _, ok := range agg.DeviceComms {
		if !ok {
			return true
		}
	}
	return false
}

func isBreakerOpenUnexpected() bool {
	if relay, ok := agg.Devices["relay"]; ok {
		closed, _ := relay["breaker_closed"].(bool)
		return !closed
	}
	return false
}

func isRecloseDisabled() bool {
	if recloser, ok := agg.Devices["recloser"]; ok {
		enabled, _ := recloser["reclose_enabled"].(bool)
		return !enabled
	}
	return false
}

func isLowVoltageCritical() bool {
	if v, ok := agg.Electrical["critical_load_voltage_v"]; ok {
		if vf, ok := v.(float64); ok {
			return vf > 0 && vf < 114.0
		}
	}
	return false
}

func isHighVoltageCritical() bool {
	if v, ok := agg.Electrical["critical_load_voltage_v"]; ok {
		if vf, ok := v.(float64); ok {
			return vf > 126.0
		}
	}
	return false
}

func main() {
	go pollDevices()
	go startModbusServer()
	go startDNP3Server()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tags", handleTags)
	mux.HandleFunc("GET /api/state", handleRawState)
	mux.HandleFunc("POST /api/command/{device}", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	shared.StartServer("rtac-sim", mux)
}
