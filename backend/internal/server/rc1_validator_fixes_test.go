package server

import (
	"errors"
	"strings"
	"testing"
)

func TestUSAuditJudgmentAllowsEmptyAuditButFailsOnFetchError(t *testing.T) {
	s := &Server{}
	gen := usGeneration(nil)
	check := s.usAuditJudgment(t.Context(), gen, []map[string]any{})
	if check.Status != "pass" || !strings.Contains(check.Detail, "0 permitted commands observed") {
		t.Fatalf("empty successful audit should pass with zero commands observed: %+v", check)
	}

	checks := withAuditFetchError([]ValidationCheck{checkPass("Audit: commands against judging target", "available")}, errors.New("connection refused"))
	if got := checks[0]; got.Status != "fail" || !strings.Contains(got.Detail, "connection refused") {
		t.Fatalf("audit fetch failure not surfaced: %+v", got)
	}
}

func TestRemediationPlanningDoesNotClaimUnverifiedSave(t *testing.T) {
	checks := validateRemediationPlanning(substationState(healthyOpts()), "weak")
	for _, check := range checks {
		if check.Name == "Plan saved" || strings.Contains(check.Detail, "selections are saved") {
			t.Fatalf("validator claimed browser persistence without evidence: %+v", check)
		}
	}
}

func TestSegmentationRequirementsGuidanceNamesCurrentControl(t *testing.T) {
	checks := validateSegmentationRequirements(substationState(healthyOpts()), nil, "improved")
	check := findCheckMatching(t, checks, "Weak baseline restored")
	if !strings.Contains(check.Detail, "Reset to Weak") || strings.Contains(check.Detail, "Return to weak baseline") {
		t.Fatalf("stale reset guidance: %+v", check)
	}
}

func TestHardeningConfigurationsFailOnMissingOrZeroMeasurements(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		check  string
	}{
		{
			name: "missing tap",
			change: func(state map[string]any) {
				delete(mapGet(state, "electrical"), "regulator_tap")
			},
			check: "Regulator tap",
		},
		{
			name: "missing voltage",
			change: func(state map[string]any) {
				delete(mapGet(state, "electrical"), "critical_load_voltage_v")
			},
			check: "Critical load voltage",
		},
		{
			name: "zero voltage",
			change: func(state map[string]any) {
				mapGet(state, "electrical")["critical_load_voltage_v"] = float64(0)
			},
			check: "Critical load voltage",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := substationState(healthyOpts())
			tc.change(state)
			check := findCheckMatching(t, validateHardeningConfigurations(state), tc.check)
			if check.Status != "fail" || strings.TrimSpace(check.Detail) == "" {
				t.Fatalf("missing/zero measurement did not fail clearly: %+v", check)
			}
		})
	}
}

func TestResetCommandFailureIsNotMasked(t *testing.T) {
	if err := commandExecutionError(0, ""); err != nil {
		t.Fatalf("successful command reported failure: %v", err)
	}
	if err := commandExecutionError(1, "permission denied"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("reset command failure should be propagated: %v", err)
	}
}
