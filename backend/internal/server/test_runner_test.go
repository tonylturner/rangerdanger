package server

import (
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestEvaluateTestStep(t *testing.T) {
	tests := []struct {
		name           string
		action         *labs.StepAction
		commandResults []StepActionResult
		firewallResult StepActionResult
		checkResults   []StepActionResult
		wantPassed     bool
		wantAutoPass   bool
		wantDetail     string
	}{
		{
			name:         "no action auto-passes",
			wantPassed:   true,
			wantAutoPass: true,
			wantDetail:   "manual step (no action)",
		},
		{
			name:         "decision auto-passes",
			action:       &labs.StepAction{Type: "decision"},
			wantPassed:   true,
			wantAutoPass: true,
			wantDetail:   "decision step (student planning — auto-pass)",
		},
		{
			name:       "unknown action fails",
			action:     &labs.StepAction{Type: "firewll"},
			wantDetail: "unknown action type: firewll",
		},
		{
			name:           "successful command passes",
			action:         &labs.StepAction{Type: "command", Device: "relay", Command: "close"},
			commandResults: []StepActionResult{{Success: true, Detail: "command executed"}},
			wantPassed:     true,
			wantDetail:     "command executed",
		},
		{
			name:           "expected containd block passes",
			action:         &labs.StepAction{Type: "command", Device: "relay", Command: "close"},
			commandResults: []StepActionResult{{Detail: "BLOCKED by containd — source denied"}},
			wantPassed:     true,
			wantDetail:     "BLOCKED by containd — source denied (expected — hardened policy is working)",
		},
		{
			name:           "failed command fails",
			action:         &labs.StepAction{Type: "command", Device: "relay", Command: "close"},
			commandResults: []StepActionResult{{Detail: "connection refused"}},
			wantDetail:     "connection refused",
		},
		{
			name:   "sequence with one failure fails",
			action: &labs.StepAction{Type: "sequence", Commands: []labs.StepActionCmd{{Device: "relay", Command: "close"}, {Device: "recloser", Command: "close"}}},
			commandResults: []StepActionResult{
				{Success: true},
				{Success: false},
			},
			wantDetail: "2 commands",
		},
		{
			name:           "firewall warnings fail",
			action:         &labs.StepAction{Type: "firewall", Config: "improved"},
			firewallResult: StepActionResult{Success: true, Detail: "policy partially applied (warnings: nft failed)", Warnings: []string{"nft failed"}},
			wantDetail:     "policy partially applied (warnings: nft failed)",
		},
		{
			name:           "clean firewall passes",
			action:         &labs.StepAction{Type: "firewall", Config: "weak"},
			firewallResult: StepActionResult{Success: true, Detail: "weak baseline"},
			wantPassed:     true,
			wantDetail:     "weak baseline",
		},
		{
			name:         "check with one failing result fails",
			action:       &labs.StepAction{Type: "check", Expect: map[string]any{"breaker_closed": true}},
			checkResults: []StepActionResult{{Success: true}, {Success: false}},
			wantDetail:   "2 checks, all pass: false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandIndex := 0
			command := func(string, string, string, *float64) StepActionResult {
				if commandIndex >= len(tt.commandResults) {
					t.Fatalf("unexpected command call %d", commandIndex+1)
				}
				result := tt.commandResults[commandIndex]
				commandIndex++
				return result
			}
			result := evaluateTestStep(4, labs.ScenarioStep{Title: "test step", Action: tt.action}, stepExecutors{
				command:  command,
				firewall: func(string) StepActionResult { return tt.firewallResult },
				check:    func(map[string]any) []StepActionResult { return tt.checkResults },
			})

			if result.StepIndex != 4 || result.StepTitle != "test step" {
				t.Errorf("step identity = (%d, %q), want (4, %q)", result.StepIndex, result.StepTitle, "test step")
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.wantPassed)
			}
			if result.AutoPass != tt.wantAutoPass {
				t.Errorf("AutoPass = %v, want %v", result.AutoPass, tt.wantAutoPass)
			}
			if result.Detail != tt.wantDetail {
				t.Errorf("Detail = %q, want %q", result.Detail, tt.wantDetail)
			}
			if commandIndex != len(tt.commandResults) {
				t.Errorf("command calls = %d, want %d", commandIndex, len(tt.commandResults))
			}
		})
	}
}

func TestAggregateResetProblems(t *testing.T) {
	tests := []struct {
		name              string
		preResetProblems  []string
		resetProblems     []string
		wantResetOK       bool
		wantResetDetail   string
		wantResetFailures int
	}{
		{
			name:              "clean resets",
			wantResetOK:       true,
			wantResetFailures: 0,
		},
		{
			name:              "pre-reset failure is prefixed",
			preResetProblems:  []string{"relay/close: unavailable"},
			wantResetDetail:   "pre-reset: relay/close: unavailable",
			wantResetFailures: 1,
		},
		{
			name:              "post-reset failure is prefixed",
			resetProblems:     []string{"firewall apply warning: partial"},
			wantResetDetail:   "reset: firewall apply warning: partial",
			wantResetFailures: 1,
		},
		{
			name:              "both reset phases are retained",
			preResetProblems:  []string{"relay/close: unavailable", "clear PCAP captures: docker unavailable"},
			resetProblems:     []string{"regulator/set_tap: timeout"},
			wantResetDetail:   "pre-reset: relay/close: unavailable; pre-reset: clear PCAP captures: docker unavailable; reset: regulator/set_tap: timeout",
			wantResetFailures: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetOK, resetDetail := aggregateResetProblems(tt.preResetProblems, tt.resetProblems)
			if resetOK != tt.wantResetOK {
				t.Errorf("ResetOK = %v, want %v", resetOK, tt.wantResetOK)
			}
			if resetDetail != tt.wantResetDetail {
				t.Errorf("ResetDetail = %q, want %q", resetDetail, tt.wantResetDetail)
			}

			gotFailures := countResetFailures([]scenarioTestResult{{ResetOK: resetOK}})
			if gotFailures != tt.wantResetFailures {
				t.Errorf("ResetFailures = %d, want %d", gotFailures, tt.wantResetFailures)
			}
			if !resetOK && !strings.Contains(resetDetail, "pre-reset:") && !strings.Contains(resetDetail, "reset:") {
				t.Errorf("failed reset has no phase prefix: %q", resetDetail)
			}
		})
	}
}
