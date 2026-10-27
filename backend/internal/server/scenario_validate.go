package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
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

// handleValidateScenario runs the active-package scenario's declared
// validator against current substation state. A scenario outside the active
// package, or one without a validator, has no validation result (404).
func (s *Server) handleValidateScenario(c *gin.Context) {
	scenarioID := c.Param("id")
	scenario, err := s.findActiveScenario(scenarioID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}
	if scenario.Validator == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario has no validator"})
		return
	}
	validator, ok := scenarioValidators[scenario.Validator]
	if !ok {
		// The loader rejects unknown keys, so only a stale row can get here.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unknown validator " + scenario.Validator})
		return
	}

	var input validatorInput
	var auditErr error
	if validator.needs(labs.CapabilityProcessElectrical) {
		input.state, err = s.fetchRTACState(c.Request.Context(), rangeOf(c))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cannot reach RTAC: " + err.Error()})
			return
		}
	}
	if validator.needs(labs.CapabilityAuditDeviceControl) {
		input.audit, auditErr = s.fetchRTACAudit(c.Request.Context(), rangeOf(c))
	}
	s.activeConfigMu.RLock()
	input.activeConfig = s.activeConfig
	s.activeConfigMu.RUnlock()
	input.gen = rangeOf(c)

	checks := validator.run(c.Request.Context(), s, input)
	checks = withAuditFetchError(checks, auditErr)

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

func withAuditFetchError(checks []ValidationCheck, err error) []ValidationCheck {
	if err == nil {
		return checks
	}
	for i, check := range checks {
		if strings.HasPrefix(check.Name, "Audit:") {
			checks[i] = checkFail(check.Name, "RTAC audit unavailable: "+err.Error())
			return checks
		}
	}
	return append(checks, checkFail("RTAC audit", "unavailable: "+err.Error()))
}

// ── Lab 2.3-bonus: Vendor RDP Compromise ───────────────────────

func validateVendorRDPCompromise(state map[string]any) []ValidationCheck {
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

	return checks
}

// ── Helpers ──────────────────────────────────────────────────────

func (s *Server) fetchRTACState(ctx context.Context, gen *lifecycle.Generation) (map[string]any, error) {
	rtac, err := rtacURL(gen)
	if err != nil {
		return nil, err
	}
	resp, err := rtacRequest(ctx, http.MethodGet, rtac+"/api/state", nil)
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

func (s *Server) fetchRTACAudit(ctx context.Context, gen *lifecycle.Generation) ([]map[string]any, error) {
	rtac, err := rtacURL(gen)
	if err != nil {
		return nil, err
	}
	resp, err := rtacRequest(ctx, http.MethodGet, rtac+"/api/audit", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read RTAC audit: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("RTAC audit returned HTTP %d", resp.StatusCode)
	}
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

func intField(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n {
			return 0, false
		}
		converted := int(n)
		if float64(converted) != n {
			return 0, false
		}
		return converted, true
	default:
		return 0, false
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

func floatField(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
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

func (s *Server) validateBaselineAssessment(ctx context.Context, gen *lifecycle.Generation, state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	elec := mapGet(state, "electrical")
	devices := mapGet(state, "devices")
	comms := mapGet(state, "device_comms")

	// 1. Check if a PCAP capture file exists (student completed the capture step)
	pcapExists := s.checkPcapFileExists(ctx, gen)
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
		if !relayComms {
			detail += " relay"
		}
		if !recloserComms {
			detail += " recloser"
		}
		if !regulatorComms {
			detail += " regulator"
		}
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
		checks = append(checks, ValidationCheck{"Weak baseline restored", "warn", "Firewall is on " + activeConfig + " config — click Reset to Weak in the policy-actions panel to restore the baseline"})
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

func validateValidationEvidence(state map[string]any) []ValidationCheck {
	var checks []ValidationCheck

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

	return checks
}

// ── Exercise 3: Firewall Implementation ────────────────────────

func validateFirewallImplementation(state map[string]any) []ValidationCheck {
	var checks []ValidationCheck

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

func validateHardeningConfigurations(state map[string]any) []ValidationCheck {
	var checks []ValidationCheck

	elec := mapGet(state, "electrical")
	devices := mapGet(state, "devices")

	// Regulator tap near normal (Modbus override side)
	tap, tapAvailable := intField(elec, "regulator_tap")
	if !tapAvailable {
		checks = append(checks, ValidationCheck{"Regulator tap", "fail", "Tap position unavailable — cannot verify that the Modbus write was blocked"})
	} else if tap >= -3 && tap <= 3 {
		checks = append(checks, ValidationCheck{"Regulator tap", "pass",
			"Tap position is " + itoa(tap) + " (normal range)"})
	} else {
		checks = append(checks, ValidationCheck{"Regulator tap", "fail",
			"Tap position is " + itoa(tap) + " — Modbus write may have succeeded"})
	}

	// Critical-load voltage within ANSI C84.1 Range A
	critV, voltageAvailable := floatField(elec, "critical_load_voltage_v")
	if !voltageAvailable {
		checks = append(checks, ValidationCheck{"Critical load voltage", "fail", "Voltage measurement unavailable — cannot verify the critical load"})
	} else if critV == 0 {
		checks = append(checks, ValidationCheck{"Critical load voltage", "fail", "0V — no power"})
	} else if critV >= 114 && critV <= 126 {
		checks = append(checks, ValidationCheck{"Critical load voltage", "pass",
			fmtFloat(critV) + "V — within normal range"})
	} else {
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

	return checks
}

// checkPcapFileExists checks if any PCAP capture files are available.
func (s *Server) checkPcapFileExists(ctx context.Context, gen *lifecycle.Generation) bool {
	// 1. Check filesystem inside the firewall container (covers manual tcpdump captures)
	firewall, err := firewallContainer(gen)
	if dockerCli := s.orchestrator.DockerClient(); dockerCli != nil && err == nil {
		execCfg := container.ExecOptions{
			Cmd:          []string{"sh", "-c", "test -s /data/captures/baseline.pcap && echo YES"},
			AttachStdout: true,
		}
		execID, err := dockerCli.ContainerExecCreate(ctx, firewall, execCfg)
		if err == nil {
			resp, err := dockerCli.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
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
	files, err := gen.Containd().ListPcapFiles(ctx)
	if err == nil && len(files) > 0 {
		return true
	}

	// 3. Check the backend's own capture tracking
	s.pcapMu.Lock()
	hasCapture := s.pcap.FileReady
	s.pcapMu.Unlock()

	return hasCapture
}

func init() {
	registerScenarioValidator("us-baseline-assessment", scenarioValidator{
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityCaptureFirewall},
		run: func(ctx context.Context, s *Server, in validatorInput) []ValidationCheck {
			return s.validateBaselineAssessment(ctx, in.gen, in.state, in.audit, in.activeConfig)
		},
	})
	registerScenarioValidator("us-segmentation-requirements", scenarioValidator{
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		run: func(_ context.Context, _ *Server, in validatorInput) []ValidationCheck {
			return validateSegmentationRequirements(in.state, in.audit, in.activeConfig)
		},
	})
	registerScenarioValidator("us-remediation-planning", scenarioValidator{
		capabilities: []string{labs.CapabilityProcessElectrical},
		run: func(_ context.Context, _ *Server, in validatorInput) []ValidationCheck {
			return validateRemediationPlanning(in.state, in.activeConfig)
		},
	})
	registerScenarioValidator("us-firewall-implementation", scenarioValidator{
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		run: func(ctx context.Context, s *Server, in validatorInput) []ValidationCheck {
			checks := validateFirewallImplementation(in.state)
			return append(checks, s.usPolicyJudgment(ctx, in.gen))
		},
	})
	registerScenarioValidator("us-hardening-configurations", scenarioValidator{
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind, labs.CapabilityAuditDeviceControl},
		run: func(ctx context.Context, s *Server, in validatorInput) []ValidationCheck {
			checks := validateHardeningConfigurations(in.state)
			return append(checks, s.usPolicyJudgment(ctx, in.gen), s.usAuditJudgment(in.gen, in.audit))
		},
	})
	registerScenarioValidator("us-vendor-rdp-compromise", scenarioValidator{
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		run: func(ctx context.Context, s *Server, in validatorInput) []ValidationCheck {
			checks := validateVendorRDPCompromise(in.state)
			return append(checks, s.usPolicyJudgment(ctx, in.gen))
		},
	})

	registerScenarioValidator("us-validation-evidence", scenarioValidator{
		capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
		run: func(ctx context.Context, s *Server, in validatorInput) []ValidationCheck {
			checks := validateValidationEvidence(in.state)
			return append(checks, s.usPolicyJudgment(ctx, in.gen), s.usRetainedReport(in.gen))
		},
	})

}
