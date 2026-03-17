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
	agg   = &AggregatedState{
		Devices:     make(map[string]map[string]any),
		Electrical:  make(map[string]any),
		DeviceComms: make(map[string]bool),
	}
	audit = shared.NewAuditLog(500)

	fieldDevices   []DeviceEndpoint
	physicsURL     string
)

func init() {
	// Device endpoints configured via environment or defaults.
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

		// Poll each field device
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

	// Flatten device state into tag namespace
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

	// Electrical measurements
	for k, v := range agg.Electrical {
		tags["electrical."+k] = v
	}

	// Comms status
	for dev, ok := range agg.DeviceComms {
		tags["comms."+dev+".ok"] = ok
	}

	// Alarms derived from state
	tags["alarm.comm_loss.field"] = hasCommLoss()
	tags["alarm.breaker_open_unexpected"] = isBreakerOpenUnexpected()
	tags["alarm.reclose_disabled"] = isRecloseDisabled()
	tags["alarm.low_voltage_critical_load"] = isLowVoltageCritical()

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
func handleCommand(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("device")
	var cmd shared.CommandRequest
	if err := shared.ReadJSON(r, &cmd); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	source := shared.SourceFromRequest(r, cmd)
	cmd.Source = source

	// Find target device
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
		Source:  source,
		Target:  device,
		Command: cmd.Command,
	}

	// Forward command
	cmdJSON, _ := json.Marshal(cmd)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(targetURL+"/api/command", "application/json", bytes.NewReader(cmdJSON))
	if err != nil {
		entry.Result = "error"
		entry.Detail = err.Error()
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
	audit.Add(entry)

	log.Printf("CMD %s -> %s/%s = %s (%s)", source, device, cmd.Command, entry.Result, entry.Detail)

	shared.WriteJSON(w, result)
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
			return vf < 114.0 // < 95% of 120V
		}
	}
	return false
}

func main() {
	go pollDevices()
	go startModbusServer()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tags", handleTags)
	mux.HandleFunc("GET /api/state", handleRawState)
	mux.HandleFunc("POST /api/command/{device}", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	shared.StartServer("rtac-sim", mux)
}
