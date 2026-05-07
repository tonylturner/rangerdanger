package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// ValidationCheck is a single pass/fail condition.
type ValidationCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "pass", "fail", "warn"
	Detail string `json:"detail"`
}

// ValidationResult is the full result of validating a scenario.
type ValidationResult struct {
	ScenarioID string            `json:"scenario_id"`
	Outcome    string            `json:"outcome"` // "PASS", "FAIL", "PARTIAL"
	Checks     []ValidationCheck `json:"checks"`
	Timestamp  string            `json:"timestamp"`
}

// handleValidateScenario checks current substation state against scenario pass criteria.
func (s *Server) handleValidateScenario(c *gin.Context) {
	scenarioID := c.Param("id")

	// Fetch current state from RTAC
	state, err := s.fetchRTACState()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cannot reach RTAC: " + err.Error()})
		return
	}

	// Fetch audit log
	audit, err := s.fetchRTACAudit()
	if err != nil {
		audit = nil // non-fatal
	}

	// Get active firewall config
	s.activeConfigMu.RLock()
	activeConfig := s.activeConfig
	s.activeConfigMu.RUnlock()

	var checks []ValidationCheck

	switch scenarioID {
	case "baseline-assessment":
		checks = s.validateBaselineAssessment(state, audit, activeConfig)
	case "segmentation-requirements":
		checks = validateSegmentationRequirements(state, audit, activeConfig)
	case "remediation-planning":
		checks = validateRemediationPlanning(state, activeConfig)
	case "firewall-implementation":
		checks = validateFirewallImplementation(state, audit, activeConfig)
	case "hardening-configurations":
		checks = validateHardeningConfigurations(state, audit, activeConfig)
	case "vendor-rdp-compromise":
		checks = validateVendorRDPCompromise(state, audit, activeConfig)
	case "validation-evidence":
		checks = validateValidationEvidence(state, audit, activeConfig)
	default:
		// Generic validation: check basic operational state.
		// Used for any scenario without a dedicated validator.
		checks = validateGeneric(state, activeConfig)
	}

	// Determine overall outcome
	outcome := "PASS"
	for _, check := range checks {
		if check.Status == "fail" {
			outcome = "FAIL"
			break
		}
		if check.Status == "warn" && outcome == "PASS" {
			outcome = "PARTIAL"
		}
	}

	c.JSON(http.StatusOK, ValidationResult{
		ScenarioID: scenarioID,
		Outcome:    outcome,
		Checks:     checks,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})
}

// ── Lab 2.3-bonus: Vendor RDP Compromise ───────────────────────

func validateVendorRDPCompromise(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	// Check 1: Recloser should be closed (restored)
	elec := mapGet(state, "electrical")
	rclClosed := boolGet(elec, "recloser_closed")
	if rclClosed {
		checks = append(checks, ValidationCheck{"Recloser status", "pass", "Recloser is CLOSED — feeder serving downstream loads"})
	} else {
		checks = append(checks, ValidationCheck{"Recloser status", "fail", "Recloser is OPEN — sustained outage in progress"})
	}

	// Check 2: Auto-reclose enabled
	devices := mapGet(state, "devices")
	recloser := mapGet(devices, "recloser")
	recloseEnabled := boolGet(recloser, "reclose_enabled")
	if recloseEnabled {
		checks = append(checks, ValidationCheck{"Auto-reclose", "pass", "Auto-reclose is ENABLED — fault recovery active"})
	} else {
		checks = append(checks, ValidationCheck{"Auto-reclose", "fail", "Auto-reclose is DISABLED — attacker may have succeeded"})
	}

	// Check 3: All loads energized
	critEnergized := boolGet(elec, "critical_load_energized")
	genEnergized := boolGet(elec, "general_load_energized")
	if critEnergized && genEnergized {
		checks = append(checks, ValidationCheck{"Load status", "pass", "All loads energized — hospital and fire station online"})
	} else {
		checks = append(checks, ValidationCheck{"Load status", "fail", "Loads de-energized — customer outage"})
	}

	// Check 4: Firewall config
	if activeConfig == "improved" || activeConfig == "custom" {
		checks = append(checks, ValidationCheck{"Firewall policy", "pass", "Hardened config active — vendor zone cannot reach field devices"})
	} else {
		checks = append(checks, ValidationCheck{"Firewall policy", "warn", "Weak baseline active — vendor can still access field devices"})
	}

	return checks
}

// ── Helpers ──────────────────────────────────────────────────────

func (s *Server) fetchRTACState() (map[string]any, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(s.rtacURL() + "/api/state")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) fetchRTACAudit() ([]map[string]any, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(s.rtacURL() + "/api/audit")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Entries, nil
}

func mapGet(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	sub, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return sub
}

func boolGet(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func intGet(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func floatGet(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return f
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func fmtFloat(f float64) string {
	whole := int(f)
	frac := int((f - float64(whole)) * 10)
	if frac < 0 {
		frac = -frac
	}
	return itoa(whole) + "." + itoa(frac)
}

func countAuditByZoneAndCommand(entries []map[string]any, zone, command string) int {
	count := 0
	for _, e := range entries {
		z, _ := e["source_zone"].(string)
		cmd, _ := e["command"].(string)
		result, _ := e["result"].(string)
		if z == zone && cmd == command && result == "executed" {
			count++
		}
	}
	return count
}

// ── Exercise 0: Baseline Assessment ─────────────────────────────

func (s *Server) validateBaselineAssessment(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	elec := mapGet(state, "electrical")
	devices := mapGet(state, "devices")
	comms := mapGet(state, "device_comms")

	// 1. Check if a PCAP capture file exists (student completed the capture step)
	pcapExists := s.checkPcapFileExists()
	if pcapExists {
		checks = append(checks, ValidationCheck{"PCAP captured", "pass", "Baseline capture file found — traffic was recorded"})
	} else {
		checks = append(checks, ValidationCheck{"PCAP captured", "fail", "No capture file found — complete Step 2 to capture baseline traffic"})
	}

	// 2. RTAC communicating with all field devices (the primary required flow)
	relayComms := boolGet(comms, "relay")
	recloserComms := boolGet(comms, "recloser")
	regulatorComms := boolGet(comms, "regulator")
	if relayComms && recloserComms && regulatorComms {
		checks = append(checks, ValidationCheck{"RTAC → field device comms", "pass", "RTAC polling relay, recloser, regulator — primary required flow confirmed"})
	} else {
		detail := "RTAC not reaching:"
		if !relayComms { detail += " relay" }
		if !recloserComms { detail += " recloser" }
		if !regulatorComms { detail += " regulator" }
		checks = append(checks, ValidationCheck{"RTAC → field device comms", "fail", detail + " — these flows must be preserved"})
	}

	// 4. Normal operating state — the baseline we're documenting
	bkrClosed := boolGet(elec, "breaker_closed")
	rclClosed := boolGet(elec, "recloser_closed")
	if bkrClosed && rclClosed {
		checks = append(checks, ValidationCheck{"Protection active", "pass", "Breaker and recloser CLOSED — protection is normal baseline"})
	} else {
		checks = append(checks, ValidationCheck{"Protection active", "fail", "Breaker or recloser OPEN — reset lab before capturing baseline"})
	}

	// 5. Auto-reclose enabled (key protective function to preserve)
	recloser := mapGet(devices, "recloser")
	if boolGet(recloser, "reclose_enabled") {
		checks = append(checks, ValidationCheck{"Auto-reclose", "pass", "Auto-reclose ENABLED — fault recovery is part of normal operations"})
	} else {
		checks = append(checks, ValidationCheck{"Auto-reclose", "warn", "Auto-reclose DISABLED — this is not normal baseline state"})
	}

	// 6. All loads energized
	critEnergized := boolGet(elec, "critical_load_energized")
	genEnergized := boolGet(elec, "general_load_energized")
	if critEnergized && genEnergized {
		checks = append(checks, ValidationCheck{"Loads served", "pass", "All loads energized — hospital, fire station, general customers online"})
	} else {
		checks = append(checks, ValidationCheck{"Loads served", "fail", "Loads de-energized — not a valid baseline"})
	}

	// 7. Voltage within normal range
	critV := floatGet(elec, "critical_load_voltage_v")
	if critV >= 114 && critV <= 126 {
		checks = append(checks, ValidationCheck{"Voltage", "pass",
			fmtFloat(critV) + "V — within ANSI C84.1 Range A (114-126V)"})
	} else if critV > 0 {
		checks = append(checks, ValidationCheck{"Voltage", "warn",
			fmtFloat(critV) + "V — outside normal range"})
	} else {
		checks = append(checks, ValidationCheck{"Voltage", "fail", "0V — no power"})
	}

	return checks
}

// ── Exercise 1: Segmentation Requirements ───────────────────────

func validateSegmentationRequirements(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	elec := mapGet(state, "electrical")
	bkrClosed := boolGet(elec, "breaker_closed")

	if bkrClosed {
		checks = append(checks, ValidationCheck{"Feeder status", "pass", "Feeder operating normally"})
	} else {
		checks = append(checks, ValidationCheck{"Feeder status", "fail", "Feeder breaker open"})
	}

	// This exercise previews the improved config then returns to weak
	if activeConfig == "weak" {
		checks = append(checks, ValidationCheck{"Weak baseline restored", "pass", "Weak baseline is active — ready for remediation planning and implementation"})
	} else {
		checks = append(checks, ValidationCheck{"Weak baseline restored", "warn", "Firewall is on " + activeConfig + " config — complete the 'Return to weak baseline' step before moving on"})
	}

	// RTAC should still be communicating on the weak config
	comms := mapGet(state, "device_comms")
	if boolGet(comms, "relay") && boolGet(comms, "recloser") && boolGet(comms, "regulator") {
		checks = append(checks, ValidationCheck{"RTAC operational", "pass", "RTAC communicating with all field devices"})
	} else {
		checks = append(checks, ValidationCheck{"RTAC operational", "fail", "RTAC lost communication with field devices"})
	}

	return checks
}

// ── Lab 2.4: Testing & Validation ───────────────────────────────

func validateValidationEvidence(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	// Must be on a hardened config (improved or custom from Exercise 3)
	if activeConfig == "improved" || activeConfig == "custom" {
		checks = append(checks, ValidationCheck{"Hardened config", "pass", "Hardened firewall configuration is active (" + activeConfig + ")"})
	} else {
		checks = append(checks, ValidationCheck{"Hardened config", "fail", "Hardened config not active — apply it using Apply Hardened or Apply Your Plan"})
	}

	// All devices operational
	elec := mapGet(state, "electrical")
	bkrClosed := boolGet(elec, "breaker_closed")
	rclClosed := boolGet(elec, "recloser_closed")
	critEnergized := boolGet(elec, "critical_load_energized")

	if bkrClosed && rclClosed {
		checks = append(checks, ValidationCheck{"Protection status", "pass", "Breaker and recloser CLOSED — normal protection"})
	} else {
		checks = append(checks, ValidationCheck{"Protection status", "fail", "Breaker or recloser OPEN — not normal"})
	}

	if critEnergized {
		checks = append(checks, ValidationCheck{"Critical loads", "pass", "Hospital and fire station energized"})
	} else {
		checks = append(checks, ValidationCheck{"Critical loads", "fail", "Critical loads de-energized"})
	}

	// RTAC still communicating through hardened firewall
	comms := mapGet(state, "device_comms")
	allComms := boolGet(comms, "relay") && boolGet(comms, "recloser") && boolGet(comms, "regulator")
	if allComms {
		checks = append(checks, ValidationCheck{"RTAC→field comms", "pass", "RTAC polling all field devices through containd"})
	} else {
		checks = append(checks, ValidationCheck{"RTAC→field comms", "fail", "RTAC lost communication with field devices"})
	}

	devices := mapGet(state, "devices")
	recloser := mapGet(devices, "recloser")
	if boolGet(recloser, "reclose_enabled") {
		checks = append(checks, ValidationCheck{"Auto-reclose", "pass", "Auto-reclose enabled"})
	} else {
		checks = append(checks, ValidationCheck{"Auto-reclose", "fail", "Auto-reclose disabled"})
	}

	return checks
}

// ── Exercise 2: Remediation Planning ───────────────────────────

func validateRemediationPlanning(state map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	// Planning exercise: substation should still be in normal state
	elec := mapGet(state, "electrical")
	bkrClosed := boolGet(elec, "breaker_closed")
	rclClosed := boolGet(elec, "recloser_closed")
	critEnergized := boolGet(elec, "critical_load_energized")

	if bkrClosed && rclClosed && critEnergized {
		checks = append(checks, ValidationCheck{"Substation operational", "pass", "All protection closed, loads energized — no unintended changes during planning"})
	} else {
		checks = append(checks, ValidationCheck{"Substation operational", "fail", "Substation not in normal state — reset lab before planning"})
	}

	// Planning is a student decision exercise — no automated check for plan quality
	checks = append(checks, ValidationCheck{"Plan saved", "pass", "Remediation plan selections are saved in browser — referenced by later exercises"})

	return checks
}

// ── Exercise 3: Firewall Implementation ────────────────────────

func validateFirewallImplementation(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	// Firewall should no longer be on the weak baseline
	if activeConfig == "weak" {
		checks = append(checks, ValidationCheck{"Firewall policy changed", "fail", "Still on weak baseline — apply your custom policy or the improved config"})
	} else {
		checks = append(checks, ValidationCheck{"Firewall policy changed", "pass", "Firewall policy updated from weak baseline (active: " + activeConfig + ")"})
	}

	// RTAC must still communicate with field devices
	comms := mapGet(state, "device_comms")
	relayOk := boolGet(comms, "relay")
	recloserOk := boolGet(comms, "recloser")
	regulatorOk := boolGet(comms, "regulator")
	capbankOk := boolGet(comms, "capbank")
	if relayOk && recloserOk && regulatorOk {
		detail := "RTAC polling relay, recloser, regulator"
		if capbankOk {
			detail += ", capbank"
		}
		checks = append(checks, ValidationCheck{"RTAC → field comms", "pass", detail + " — authorized flows preserved"})
	} else {
		detail := "RTAC lost communication with:"
		if !relayOk {
			detail += " relay"
		}
		if !recloserOk {
			detail += " recloser"
		}
		if !regulatorOk {
			detail += " regulator"
		}
		checks = append(checks, ValidationCheck{"RTAC → field comms", "fail", detail + " — check your ALLOW rules"})
	}

	// Normal operating state preserved
	elec := mapGet(state, "electrical")
	bkrClosed := boolGet(elec, "breaker_closed")
	rclClosed := boolGet(elec, "recloser_closed")
	critEnergized := boolGet(elec, "critical_load_energized")
	if bkrClosed && rclClosed && critEnergized {
		checks = append(checks, ValidationCheck{"Operations preserved", "pass", "Breaker/recloser CLOSED, loads energized — segmentation did not break operations"})
	} else {
		checks = append(checks, ValidationCheck{"Operations preserved", "fail", "Feeder not in normal state — your rules may be too restrictive"})
	}

	// Voltage within range
	critV := floatGet(elec, "critical_load_voltage_v")
	if critV >= 114 && critV <= 126 {
		checks = append(checks, ValidationCheck{"Voltage quality", "pass", fmtFloat(critV) + "V — within ANSI C84.1 Range A"})
	} else if critV > 0 {
		checks = append(checks, ValidationCheck{"Voltage quality", "warn", fmtFloat(critV) + "V — outside normal range"})
	} else {
		checks = append(checks, ValidationCheck{"Voltage quality", "fail", "0V — no power"})
	}

	return checks
}

// ── Lab 2.3: Hardening Configurations ───────────────────────────
//
// Combines the modbus-override and dnp3-command-injection checks
// since Lab 2.3 stress-tests the hardened policy against both
// attack vectors. The lab passes when the substation is in normal
// state and the hardened firewall config is active.

func validateHardeningConfigurations(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	elec := mapGet(state, "electrical")
	devices := mapGet(state, "devices")

	// Regulator tap near normal (Modbus override side)
	tap := intGet(elec, "regulator_tap")
	if tap >= -3 && tap <= 3 {
		checks = append(checks, ValidationCheck{"Regulator tap", "pass",
			"Tap position is " + itoa(tap) + " (normal range)"})
	} else {
		checks = append(checks, ValidationCheck{"Regulator tap", "fail",
			"Tap position is " + itoa(tap) + " — Modbus write may have succeeded"})
	}

	// Critical-load voltage within ANSI C84.1 Range A
	critV := floatGet(elec, "critical_load_voltage_v")
	if critV >= 114 && critV <= 126 {
		checks = append(checks, ValidationCheck{"Critical load voltage", "pass",
			fmtFloat(critV) + "V — within normal range"})
	} else if critV > 0 {
		checks = append(checks, ValidationCheck{"Critical load voltage", "fail",
			fmtFloat(critV) + "V — outside ANSI Range A"})
	}

	// Recloser closed + auto-reclose enabled (DNP3 injection side)
	rclClosed := boolGet(elec, "recloser_closed")
	recloseEnabled := boolGet(mapGet(devices, "recloser"), "reclose_enabled")
	if rclClosed && recloseEnabled {
		checks = append(checks, ValidationCheck{"Recloser status", "pass",
			"Recloser CLOSED with auto-reclose ENABLED"})
	} else if !rclClosed {
		checks = append(checks, ValidationCheck{"Recloser status", "fail",
			"Recloser is OPEN — outage may be in progress"})
	} else {
		checks = append(checks, ValidationCheck{"Recloser status", "fail",
			"Auto-reclose is DISABLED — DNP3 attack may have succeeded"})
	}

	// Hardened firewall must be active to claim the lab passed
	if activeConfig == "improved" || activeConfig == "custom" {
		checks = append(checks, ValidationCheck{"Firewall policy", "pass",
			"Hardened config active (" + activeConfig + ") — DPI + source-pinning in effect"})
	} else {
		checks = append(checks, ValidationCheck{"Firewall policy", "fail",
			"Weak baseline still active — apply Hardened or Your Plan first"})
	}

	// Audit: no unauthorized writes from non-RTAC sources
	badWrites := countAuditNonRTACCommand(audit, "set_tap") +
		countAuditByZoneAndCommand(audit, "enterprise", "crob_reclose") +
		countAuditByZoneAndCommand(audit, "enterprise", "disable_reclose")
	if badWrites == 0 {
		checks = append(checks, ValidationCheck{"Audit: unauthorized writes", "pass",
			"No unauthorized Modbus or DNP3 commands in audit log"})
	} else {
		checks = append(checks, ValidationCheck{"Audit: unauthorized writes", "warn",
			strings.Replace("N unauthorized command(s) reached field devices", "N", itoa(badWrites), 1)})
	}

	return checks
}

// ── Generic validation (fallback) ───────────────────────────────

func validateGeneric(state map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	elec := mapGet(state, "electrical")
	if boolGet(elec, "breaker_closed") {
		checks = append(checks, ValidationCheck{"Breaker", "pass", "Feeder breaker CLOSED"})
	} else {
		checks = append(checks, ValidationCheck{"Breaker", "fail", "Feeder breaker OPEN"})
	}

	if boolGet(elec, "critical_load_energized") {
		checks = append(checks, ValidationCheck{"Critical load", "pass", "Energized"})
	} else {
		checks = append(checks, ValidationCheck{"Critical load", "fail", "De-energized"})
	}

	checks = append(checks, ValidationCheck{"Firewall config", "pass", "Active: " + activeConfig})

	return checks
}

// checkPcapFileOnDisk checks if PCAP capture files exist on the filesystem
// or via the containd API. Does NOT check the in-memory flag, which can be
// stale from a previous session and survive lab resets.
func (s *Server) checkPcapFileOnDisk() bool {
	// 1. Check filesystem inside the firewall container
	if dockerCli := s.orchestrator.DockerClient(); dockerCli != nil {
		execCfg := container.ExecOptions{
			Cmd:          []string{"sh", "-c", "ls /data/captures/*.pcap 2>/dev/null | head -1"},
			AttachStdout: true,
		}
		execID, err := dockerCli.ContainerExecCreate(context.Background(), firewallContainer, execCfg)
		if err == nil {
			resp, err := dockerCli.ContainerExecAttach(context.Background(), execID.ID, container.ExecAttachOptions{})
			if err == nil {
				out, _ := io.ReadAll(resp.Reader)
				resp.Close()
				if strings.Contains(string(out), ".pcap") {
					return true
				}
			}
		}
	}

	// 2. Check containd PCAP API
	containdURL := s.cfg.ContaindAPIURL
	if containdURL == "" {
		containdURL = "http://firewall:8080"
	}
	client := containd.NewClient(containdURL)
	files, err := client.ListPcapFiles()
	if err == nil && len(files) > 0 {
		return true
	}

	return false
}

// checkPcapFileExists checks if any PCAP capture files are available.
func (s *Server) checkPcapFileExists() bool {
	// 1. Check filesystem inside the firewall container (covers manual tcpdump captures)
	if dockerCli := s.orchestrator.DockerClient(); dockerCli != nil {
		execCfg := container.ExecOptions{
			Cmd:          []string{"sh", "-c", "test -s /data/captures/baseline.pcap && echo YES"},
			AttachStdout: true,
		}
		execID, err := dockerCli.ContainerExecCreate(context.Background(), firewallContainer, execCfg)
		if err == nil {
			resp, err := dockerCli.ContainerExecAttach(context.Background(), execID.ID, container.ExecAttachOptions{})
			if err == nil {
				out, _ := io.ReadAll(resp.Reader)
				resp.Close()
				if strings.Contains(string(out), "YES") {
					return true
				}
			}
		}
	}

	// 2. Check containd PCAP API (covers API-initiated captures)
	containdURL := s.cfg.ContaindAPIURL
	if containdURL == "" {
		containdURL = "http://firewall:8080"
	}
	client := containd.NewClient(containdURL)
	files, err := client.ListPcapFiles()
	if err == nil && len(files) > 0 {
		return true
	}

	// 3. Check the backend's own capture tracking
	s.pcapMu.Lock()
	hasCapture := s.pcap.FileReady
	s.pcapMu.Unlock()

	return hasCapture
}

func countAuditNonRTACCommand(entries []map[string]any, command string) int {
	count := 0
	for _, e := range entries {
		cmd, _ := e["command"].(string)
		source, _ := e["source"].(string)
		result, _ := e["result"].(string)
		if cmd == command && result == "executed" {
			// RTAC is the legitimate source
			if !strings.Contains(strings.ToLower(source), "rtac") &&
				!strings.Contains(strings.ToLower(source), "reset") &&
				!strings.Contains(strings.ToLower(source), "operator") {
				count++
			}
		}
	}
	return count
}
