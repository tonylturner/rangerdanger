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
	capbankHost := envOr("CAPBANK_SIM_HOST", "capbank-sim:8080")
	physicsURL = "http://" + envOr("OPENDSS_SIM_HOST", "opendss-sim:8080")

	fieldDevices = []DeviceEndpoint{
		{Name: "relay", URL: fmt.Sprintf("http://%s", relayHost)},
		{Name: "recloser", URL: fmt.Sprintf("http://%s", recloserHost)},
		{Name: "regulator", URL: fmt.Sprintf("http://%s", regulatorHost)},
		{Name: "capbank", URL: fmt.Sprintf("http://%s", capbankHost)},
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
//
// HTTP keep-alive is deliberately disabled: we want each poll to open a
// new TCP connection, close it, and move on. This matches the behavior of
// the Modbus and DNP3 pollers (which also open fresh connections per
// poll) so the resulting wire traffic has consistent per-poll flow
// semantics across all three protocols. Without this, Go's default http
// transport reuses connections and the capture shows a handful of huge
// persistent HTTP flows that drown out Modbus and DNP3 in the flow
// table.
func pollDevices() {
	transport := &http.Transport{
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: transport,
	}
	// 2-second interval balances HTTP volume with Modbus (3s) and DNP3 (5s)
	// so no single protocol drowns out the others in a firewall capture.
	ticker := time.NewTicker(2 * time.Second)
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

		// Close the AUTO-mode control loops using the freshly solved state.
		runAutoControls(client)
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
	if capbank, ok := agg.Devices["capbank"]; ok {
		for k, v := range capbank {
			tags["feeder.capbank."+k] = v
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
		return fmt.Sprintf("feeder fault — overcurrent relay TRIPPED the breaker, %.0f kW de-energized, manual close required to restore%s", totalKW, zoneLabel)
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
	case "capbank/switch_in":
		return fmt.Sprintf("capacitor bank SWITCHED IN — reactive power injected, voltage rises%s", zoneLabel)
	case "capbank/switch_out":
		return fmt.Sprintf("capacitor bank SWITCHED OUT — reactive power removed, voltage drops%s", zoneLabel)
	case "capbank/set_auto":
		return "capacitor bank AUTO — switches automatically to correct power factor / support voltage"
	case "capbank/set_manual":
		return fmt.Sprintf("capacitor bank MANUAL — automatic correction disabled, operator controls switching%s", zoneLabel)
	case "capbank/inject_fault":
		return "fault injected on capacitor bank — protection will operate"
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

func boolFromMap(m map[string]any, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func intFromMap(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

// ── Closed-loop auto control ────────────────────────────────────────
//
// The voltage regulator and capacitor bank each have an operator-selectable
// AUTO / MANUAL mode. In AUTO, the RTAC closes the control loop the field
// devices were built for but never had wired: each poll cycle it reads the
// OpenDSS power-flow result and nudges the device toward its target.
//
//   - Regulator (AVR): holds the regulated (critical-load) bus at its voltage
//     setpoint, stepping the tap one position per cycle. The deadband is wider
//     than a single tap step so it settles instead of hunting.
//   - Capacitor bank: switches in to correct a lagging power factor (local VAR
//     support) and switches out if its bus runs high (over-voltage cutout).
//
// In MANUAL the loop leaves the device alone — so an operator or attacker who
// flips a device to MANUAL defeats the automatic correction. That is the
// intended cyber-to-physical lesson. Auto actions go straight to the device
// sim tagged with an "rtac-auto" source, so they are traceable in the device
// audit without cluttering the RTAC command audit (reserved for external
// operator/attacker commands). The loop is a no-op whenever the controlled bus
// is de-energized, so it never chases a dead feeder or runs the tap away.
const (
	regSetpointDefaultV = 120.0
	regDeadbandV        = 1.5 // wider than one 0.75V tap step, so the AVR settles
	regTapMin           = -16
	regTapMax           = 16
	capPFTarget         = 0.95
	capVHighDefaultV    = 126.0
)

type autoCmd struct {
	device  string
	command string
}

// autoControlDecisions reads the latest aggregated state and returns the
// auto-control commands to issue this cycle (at most one per device).
func autoControlDecisions() []autoCmd {
	agg.mu.RLock()
	defer agg.mu.RUnlock()

	var cmds []autoCmd
	elec := agg.Electrical

	// Regulator AVR — only when the regulated (critical) bus is energized.
	if reg, ok := agg.Devices["regulator"]; ok &&
		!boolFromMap(reg, "manual_mode") && boolFromMap(elec, "critical_load_energized") {
		v := floatFromMap(elec, "critical_load_voltage_v")
		setpoint := floatFromMap(reg, "voltage_setpoint_v")
		if setpoint == 0 {
			setpoint = regSetpointDefaultV
		}
		tap := intFromMap(reg, "tap_position")
		switch {
		case v > 0 && v < setpoint-regDeadbandV && tap < regTapMax:
			cmds = append(cmds, autoCmd{"regulator", "raise_tap"})
		case v > 0 && v > setpoint+regDeadbandV && tap > regTapMin:
			cmds = append(cmds, autoCmd{"regulator", "lower_tap"})
		}
	}

	// Capacitor bank — only when its bus (load bus) is energized and not in
	// lockout. Switch in to correct a lagging PF; switch out on over-voltage.
	if cb, ok := agg.Devices["capbank"]; ok &&
		boolFromMap(cb, "auto_mode") && !boolFromMap(cb, "lockout") &&
		boolFromMap(elec, "general_load_energized") {
		pf := floatFromMap(elec, "power_factor")
		vload := floatFromMap(elec, "downstream_voltage_v")
		vhigh := floatFromMap(cb, "voltage_thresh_high_v")
		if vhigh == 0 {
			vhigh = capVHighDefaultV
		}
		in := boolFromMap(cb, "switched_in")
		switch {
		case !in && pf > 0 && pf < capPFTarget && vload < vhigh:
			cmds = append(cmds, autoCmd{"capbank", "switch_in"})
		case in && vload > vhigh:
			cmds = append(cmds, autoCmd{"capbank", "switch_out"})
		}
	}

	return cmds
}

// runAutoControls issues this cycle's auto-control commands straight to the
// field device sims, bypassing the RTAC command audit (reserved for external
// commands). Best-effort — the next cycle retries on any failure.
func runAutoControls(client *http.Client) {
	for _, c := range autoControlDecisions() {
		url := deviceURL(c.device)
		if url == "" {
			continue
		}
		body, _ := json.Marshal(shared.CommandRequest{Command: c.command, Source: "rtac-auto"})
		resp, err := client.Post(url+"/api/command", "application/json", bytes.NewReader(body))
		if err != nil {
			continue
		}
		resp.Body.Close()
		log.Printf("AUTO %s/%s", c.device, c.command)
	}
}

func deviceURL(name string) string {
	for _, dev := range fieldDevices {
		if dev.Name == name {
			return dev.URL
		}
	}
	return ""
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
	go pollDNP3Devices()
	go pollModbusDevices()
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
