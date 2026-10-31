package server

import (
	"errors"
	"strings"
	"testing"
)

func TestCaptureEvidenceRequiresSuccessfulAnalysableCapture(t *testing.T) {
	if err := captureCommandError(124, ""); err != nil {
		t.Fatalf("normal timeout completion should be accepted: %v", err)
	}
	if err := captureCommandError(1, "tcpdump: invalid capture file"); err == nil || !strings.Contains(err.Error(), "invalid capture file") {
		t.Fatalf("capture failure should surface its command output, got %v", err)
	}

	if _, err := summarizePcapOutput("", []string{"10", "40", "40"}); err == nil {
		t.Fatal("empty capture analysis must fail")
	}
	summary, err := summarizePcapOutput("12:00:00.1 eth0 Out IP 10.10.10.4.54321 > 10.40.40.2.502: Flags [S]\n", []string{"10", "40", "40"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "10.10.10.4") {
		t.Fatalf("capture source missing from analysis: %q", summary)
	}
	md := usRecipe.renderValidationMarkdown("custom", "source", "hash", "path", false, "Capture analysis failed: PCAP contains no analysable packets", nil, 0, 0, 0, 0, 0, false)
	if !strings.Contains(md, "PCAP contains no analysable packets") {
		t.Fatalf("report hid the capture failure: %s", md)
	}
}

func TestUSAuditJudgmentAllowsEmptyAuditButFailsOnFetchError(t *testing.T) {
	s := &Server{}
	gen := usGeneration(nil)
	check := s.usAuditJudgment(gen, []map[string]any{})
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
