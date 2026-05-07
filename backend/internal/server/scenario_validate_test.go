package server

import (
	"strings"
	"testing"
)

// substationState builds the nested map structure the validators
// expect — same shape as what /api/state returns from the RTAC, so
// tests can manipulate individual fields without rebuilding the
// entire payload.
type substationOpts struct {
	BreakerClosed         bool
	RecloserClosed        bool
	RecloseEnabled        bool
	CritEnergized         bool
	GenEnergized          bool
	CritVoltage           float64
	RegulatorTap          int
	CapbankSwitchedIn     bool
	CapbankLockout        bool
	IntraZoneFlowsHealthy bool
}

// healthyOpts returns a substation-state options bundle in the
// "normal operating" configuration. Tests deviate from this for
// individual checks.
func healthyOpts() substationOpts {
	return substationOpts{
		BreakerClosed:         true,
		RecloserClosed:        true,
		RecloseEnabled:        true,
		CritEnergized:         true,
		GenEnergized:          true,
		CritVoltage:           120.0,
		RegulatorTap:          0,
		CapbankSwitchedIn:     true,
		CapbankLockout:        false,
		IntraZoneFlowsHealthy: true,
	}
}

func substationState(o substationOpts) map[string]any {
	return map[string]any{
		"electrical": map[string]any{
			"breaker_closed":          o.BreakerClosed,
			"recloser_closed":         o.RecloserClosed,
			"critical_load_energized": o.CritEnergized,
			"general_load_energized":  o.GenEnergized,
			"critical_load_voltage_v": o.CritVoltage,
			"regulator_tap":           o.RegulatorTap,
		},
		"devices": map[string]any{
			"recloser": map[string]any{
				"reclose_enabled": o.RecloseEnabled,
			},
			"capbank": map[string]any{
				"switched_in": o.CapbankSwitchedIn,
				"lockout":     o.CapbankLockout,
			},
		},
		"intra_zone_flows": map[string]any{
			"hmi_to_rtac_healthy": o.IntraZoneFlowsHealthy,
		},
	}
}

// outcomeCounts tallies pass/warn/fail across a check list — makes
// assertions terser.
func outcomeCounts(checks []ValidationCheck) (pass, warn, fail int) {
	for _, c := range checks {
		switch c.Status {
		case "pass":
			pass++
		case "warn":
			warn++
		case "fail":
			fail++
		}
	}
	return
}

func findCheckMatching(t *testing.T, checks []ValidationCheck, namePart string) ValidationCheck {
	t.Helper()
	for _, c := range checks {
		if strings.Contains(c.Name, namePart) {
			return c
		}
	}
	t.Fatalf("no check found with name containing %q; got %d checks", namePart, len(checks))
	return ValidationCheck{}
}

// ── Lab 2.3: Hardening Configurations ────────────────────────────

func TestValidateHardeningConfigurations_HardenedAndHealthy(t *testing.T) {
	state := substationState(healthyOpts())
	checks := validateHardeningConfigurations(state, nil, "improved")
	pass, warn, fail := outcomeCounts(checks)
	if fail != 0 || warn != 0 {
		t.Errorf("expected all-pass under hardened+healthy state, got pass=%d warn=%d fail=%d", pass, warn, fail)
		for _, c := range checks {
			t.Logf("  %s: %s — %s", c.Status, c.Name, c.Detail)
		}
	}
}

func TestValidateHardeningConfigurations_WeakBaselineFails(t *testing.T) {
	state := substationState(healthyOpts())
	checks := validateHardeningConfigurations(state, nil, "weak")
	fw := findCheckMatching(t, checks, "Firewall policy")
	if fw.Status != "fail" {
		t.Errorf("weak baseline must fail the firewall-policy check, got status=%q detail=%q", fw.Status, fw.Detail)
	}
}

func TestValidateHardeningConfigurations_TapManipulationFails(t *testing.T) {
	o := healthyOpts()
	o.RegulatorTap = -16 // attacker forced extreme low
	state := substationState(o)
	checks := validateHardeningConfigurations(state, nil, "improved")
	tap := findCheckMatching(t, checks, "Regulator tap")
	if tap.Status != "fail" {
		t.Errorf("extreme tap must fail, got %q", tap.Status)
	}
}

func TestValidateHardeningConfigurations_AutoRecloseDisabledFails(t *testing.T) {
	o := healthyOpts()
	o.RecloseEnabled = false // attacker disabled it
	state := substationState(o)
	checks := validateHardeningConfigurations(state, nil, "improved")
	rcl := findCheckMatching(t, checks, "Recloser status")
	if rcl.Status != "fail" {
		t.Errorf("disabled auto-reclose must fail Recloser-status check, got %q (%q)", rcl.Status, rcl.Detail)
	}
}

func TestValidateHardeningConfigurations_AuditWarnsOnEnterpriseAttacks(t *testing.T) {
	state := substationState(healthyOpts())
	// Audit entries must carry result="executed" — the matching helpers
	// only count entries that actually reached the device, not the ones
	// the firewall blocked. This mirrors what the RTAC writes when an
	// inbound command is processed.
	audit := []map[string]any{
		{"source_zone": "enterprise", "command": "crob_reclose", "result": "executed"},
		{"source_zone": "enterprise", "command": "disable_reclose", "result": "executed"},
	}
	checks := validateHardeningConfigurations(state, audit, "improved")
	auditCheck := findCheckMatching(t, checks, "Audit:")
	if auditCheck.Status != "warn" {
		t.Errorf("enterprise-source audit entries must warn, got %q (%q)", auditCheck.Status, auditCheck.Detail)
	}
}

// ── Lab 2.3-bonus: Vendor RDP Compromise ─────────────────────────

func TestValidateVendorRDPCompromise_HardenedAndRestored(t *testing.T) {
	state := substationState(healthyOpts())
	checks := validateVendorRDPCompromise(state, nil, "improved")
	pass, _, fail := outcomeCounts(checks)
	if fail != 0 {
		t.Errorf("expected no failures under hardened+restored state, got pass=%d fail=%d", pass, fail)
	}
}

func TestValidateVendorRDPCompromise_AutoRecloseDisabledFails(t *testing.T) {
	o := healthyOpts()
	o.RecloseEnabled = false
	state := substationState(o)
	checks := validateVendorRDPCompromise(state, nil, "improved")
	rcl := findCheckMatching(t, checks, "Auto-reclose")
	if rcl.Status != "fail" {
		t.Errorf("disabled auto-reclose must fail; got %q (%q)", rcl.Status, rcl.Detail)
	}
}

func TestValidateVendorRDPCompromise_CustomConfigCountsAsHardened(t *testing.T) {
	state := substationState(healthyOpts())
	checks := validateVendorRDPCompromise(state, nil, "custom")
	fw := findCheckMatching(t, checks, "Firewall policy")
	if fw.Status != "pass" {
		t.Errorf("custom config (the student's own policy from Lab 2.2) should count as hardened; got %q", fw.Status)
	}
}

// ── Lab 2.4: Validation & Evidence ───────────────────────────────

func TestValidateValidationEvidence_RequiresHardenedConfig(t *testing.T) {
	state := substationState(healthyOpts())
	checks := validateValidationEvidence(state, nil, "weak")
	hard := findCheckMatching(t, checks, "Hardened config")
	if hard.Status != "fail" {
		t.Errorf("weak baseline must fail Hardened-config check in Lab 2.4, got %q", hard.Status)
	}
}

func TestValidateValidationEvidence_PassesUnderImproved(t *testing.T) {
	state := substationState(healthyOpts())
	checks := validateValidationEvidence(state, nil, "improved")
	hard := findCheckMatching(t, checks, "Hardened config")
	if hard.Status != "pass" {
		t.Errorf("improved config must pass Hardened-config check, got %q (%q)", hard.Status, hard.Detail)
	}
}

// ── Lab 1.4: Remediation Planning ────────────────────────────────

func TestValidateRemediationPlanning_AlwaysProducesChecks(t *testing.T) {
	state := substationState(healthyOpts())
	for _, cfg := range []string{"weak", "improved", "custom"} {
		checks := validateRemediationPlanning(state, cfg)
		if len(checks) == 0 {
			t.Errorf("validateRemediationPlanning returned no checks for activeConfig=%q", cfg)
		}
	}
}

// ── Generic fallback ─────────────────────────────────────────────

func TestValidateGeneric_FallbackForUnknownScenario(t *testing.T) {
	state := substationState(healthyOpts())
	checks := validateGeneric(state, "weak")
	if len(checks) == 0 {
		t.Errorf("generic fallback must always return some checks")
	}
}

// ── Helper smoke-tests ───────────────────────────────────────────

func TestMapGet_NilSafe(t *testing.T) {
	if got := mapGet(nil, "x"); got != nil {
		t.Errorf("mapGet(nil,_) must return nil, got %v", got)
	}
	if got := mapGet(map[string]any{"x": 5}, "x"); got != nil {
		t.Errorf("mapGet on non-map value must return nil, got %v", got)
	}
}

func TestBoolGet_DefaultsToFalse(t *testing.T) {
	if boolGet(nil, "x") {
		t.Errorf("boolGet(nil,_) must be false")
	}
	if boolGet(map[string]any{"x": "true"}, "x") {
		t.Errorf("boolGet on string value must be false (no coercion)")
	}
	if !boolGet(map[string]any{"x": true}, "x") {
		t.Errorf("boolGet on true value must be true")
	}
}

func TestIntGet_AcceptsFloatAndInt(t *testing.T) {
	if intGet(map[string]any{"x": float64(7)}, "x") != 7 {
		t.Errorf("intGet must coerce float64 (JSON's numeric type) to int")
	}
	if intGet(map[string]any{"x": 7}, "x") != 7 {
		t.Errorf("intGet must accept int directly")
	}
	if intGet(nil, "x") != 0 {
		t.Errorf("intGet(nil,_) must default to 0")
	}
}

func TestCountAuditByZoneAndCommand(t *testing.T) {
	// Helper requires result="executed" — entries that the firewall
	// blocked (result="blocked" or absent) shouldn't count.
	audit := []map[string]any{
		{"source_zone": "enterprise", "command": "trip", "result": "executed"},
		{"source_zone": "enterprise", "command": "trip", "result": "executed"},
		{"source_zone": "vendor", "command": "trip", "result": "executed"},
		{"source_zone": "enterprise", "command": "set_tap", "result": "executed"},
		{"source_zone": "enterprise", "command": "trip", "result": "blocked"}, // ignored
	}
	if got := countAuditByZoneAndCommand(audit, "enterprise", "trip"); got != 2 {
		t.Errorf("expected 2 executed enterprise trips, got %d", got)
	}
	if got := countAuditByZoneAndCommand(audit, "vendor", "trip"); got != 1 {
		t.Errorf("expected 1 executed vendor trip, got %d", got)
	}
	if got := countAuditByZoneAndCommand(audit, "enterprise", "missing"); got != 0 {
		t.Errorf("missing command must return 0, got %d", got)
	}
}

func TestCountAuditByZoneAndCommand_BlockedNotCounted(t *testing.T) {
	// All-blocked audit (e.g., firewall denied everything) must
	// count as zero — that's the desired outcome under hardened.
	audit := []map[string]any{
		{"source_zone": "enterprise", "command": "trip", "result": "blocked"},
		{"source_zone": "enterprise", "command": "set_tap", "result": "denied"},
	}
	if got := countAuditByZoneAndCommand(audit, "enterprise", "trip"); got != 0 {
		t.Errorf("blocked entries must not count, got %d", got)
	}
}
