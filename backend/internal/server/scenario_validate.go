package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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
	case "enterprise-to-breaker":
		checks = validateEnterpriseToBreaker(state, audit, activeConfig)
	case "vendor-access-abuse":
		checks = validateVendorAccessAbuse(state, audit, activeConfig)
	case "ot-east-west-pivot":
		checks = validateOTEastWestPivot(state, audit, activeConfig)
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown scenario: " + scenarioID})
		return
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

// ── Scenario 1: Enterprise to Breaker ────────────────────────────

func validateEnterpriseToBreaker(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	// Check 1: Breaker should be closed (normal state)
	elec := mapGet(state, "electrical")
	bkrClosed := boolGet(elec, "breaker_closed")
	if bkrClosed {
		checks = append(checks, ValidationCheck{"Breaker status", "pass", "Feeder breaker is CLOSED — loads are energized"})
	} else {
		checks = append(checks, ValidationCheck{"Breaker status", "fail", "Feeder breaker is OPEN — attack may have succeeded"})
	}

	// Check 2: Critical loads energized
	critEnergized := boolGet(elec, "critical_load_energized")
	genEnergized := boolGet(elec, "general_load_energized")
	if critEnergized && genEnergized {
		checks = append(checks, ValidationCheck{"Load status", "pass", "All loads energized"})
	} else {
		checks = append(checks, ValidationCheck{"Load status", "fail", "Loads de-energized — feeder is not serving customers"})
	}

	// Check 3: Firewall config should be improved
	if activeConfig == "improved" {
		checks = append(checks, ValidationCheck{"Firewall policy", "pass", "Improved (hardened) config is active — enterprise→field blocked"})
	} else {
		checks = append(checks, ValidationCheck{"Firewall policy", "warn", "Weak baseline config is active — enterprise can still reach field devices"})
	}

	// Check 4: No unauthorized trip commands from enterprise zone in audit
	entTrips := countAuditByZoneAndCommand(audit, "enterprise", "trip")
	if entTrips == 0 {
		checks = append(checks, ValidationCheck{"Audit: enterprise trips", "pass", "No trip commands from enterprise zone in audit log"})
	} else {
		checks = append(checks, ValidationCheck{"Audit: enterprise trips", "warn",
			strings.Replace("N trip command(s) from enterprise zone detected in audit", "N", itoa(entTrips), 1)})
	}

	return checks
}

// ── Scenario 2: Vendor Access Abuse ──────────────────────────────

func validateVendorAccessAbuse(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	// Check 1: Auto-reclose should be enabled
	devices := mapGet(state, "devices")
	recloser := mapGet(devices, "recloser")
	recloseEnabled := boolGet(recloser, "reclose_enabled")
	if recloseEnabled {
		checks = append(checks, ValidationCheck{"Auto-reclose", "pass", "Auto-reclose is ENABLED — fault recovery active"})
	} else {
		checks = append(checks, ValidationCheck{"Auto-reclose", "fail", "Auto-reclose is DISABLED — attacker may have succeeded"})
	}

	// Check 2: Recloser closed (normal state)
	elec := mapGet(state, "electrical")
	rclClosed := boolGet(elec, "recloser_closed")
	if rclClosed {
		checks = append(checks, ValidationCheck{"Recloser status", "pass", "Recloser is CLOSED"})
	} else {
		checks = append(checks, ValidationCheck{"Recloser status", "fail", "Recloser is OPEN — may be locked out from unrecovered fault"})
	}

	// Check 3: Firewall config
	if activeConfig == "improved" {
		checks = append(checks, ValidationCheck{"Firewall policy", "pass", "Improved config active — vendor→field blocked"})
	} else {
		checks = append(checks, ValidationCheck{"Firewall policy", "warn", "Weak baseline active — vendor can still reach field devices"})
	}

	// Check 4: No vendor disable_reclose commands
	vendorDisable := countAuditByZoneAndCommand(audit, "vendor", "disable_reclose")
	if vendorDisable == 0 {
		checks = append(checks, ValidationCheck{"Audit: vendor reclose commands", "pass", "No disable_reclose from vendor zone"})
	} else {
		checks = append(checks, ValidationCheck{"Audit: vendor reclose commands", "warn",
			strings.Replace("N disable_reclose command(s) from vendor zone", "N", itoa(vendorDisable), 1)})
	}

	return checks
}

// ── Scenario 3: OT East-West Pivot ──────────────────────────────

func validateOTEastWestPivot(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
	var checks []ValidationCheck

	// Check 1: Regulator tap should be near 0 (normal)
	elec := mapGet(state, "electrical")
	tap := intGet(elec, "regulator_tap")
	if tap >= -3 && tap <= 3 {
		checks = append(checks, ValidationCheck{"Regulator tap", "pass",
			"Tap position is " + itoa(tap) + " (within normal range)"})
	} else {
		checks = append(checks, ValidationCheck{"Regulator tap", "fail",
			"Tap position is " + itoa(tap) + " (extreme — attacker may have manipulated)"})
	}

	// Check 2: Critical load voltage within ANSI C84.1 Range A
	critV := floatGet(elec, "critical_load_voltage_v")
	if critV >= 114 && critV <= 126 {
		checks = append(checks, ValidationCheck{"Critical load voltage", "pass",
			fmtFloat(critV) + "V — within ANSI C84.1 Range A (114-126V)"})
	} else if critV >= 108 && critV <= 132 {
		checks = append(checks, ValidationCheck{"Critical load voltage", "warn",
			fmtFloat(critV) + "V — Range B (outside normal but within tolerance)"})
	} else if critV > 0 {
		checks = append(checks, ValidationCheck{"Critical load voltage", "fail",
			fmtFloat(critV) + "V — outside ANSI C84.1 Range B (service quality violation)"})
	} else {
		checks = append(checks, ValidationCheck{"Critical load voltage", "fail", "Voltage is 0V — loads de-energized"})
	}

	// Check 3: Firewall config
	if activeConfig == "improved" {
		checks = append(checks, ValidationCheck{"Firewall policy", "pass", "Improved config active — only RTAC can reach field devices"})
	} else {
		checks = append(checks, ValidationCheck{"Firewall policy", "warn", "Weak baseline active — any OT node can reach field devices"})
	}

	// Check 4: No unauthorized set_tap commands from non-RTAC sources
	badTaps := countAuditNonRTACCommand(audit, "set_tap")
	if badTaps == 0 {
		checks = append(checks, ValidationCheck{"Audit: unauthorized tap changes", "pass", "No set_tap commands from non-RTAC sources"})
	} else {
		checks = append(checks, ValidationCheck{"Audit: unauthorized tap changes", "warn",
			strings.Replace("N unauthorized set_tap command(s) detected", "N", itoa(badTaps), 1)})
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
