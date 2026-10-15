package labs

import "testing"

func TestSimulationActionVocabulary(t *testing.T) {
	tests := []struct {
		name         string
		action       StepAction
		capabilities []string
		valid        bool
	}{
		{"fault", StepAction{Type: "command", Device: "breaker", Command: "inject_fault"}, []string{CapabilityProcessTruth}, true},
		{"clear", StepAction{Type: "command", Device: "breaker", Command: "clear_fault"}, []string{CapabilityProcessTruth}, true},
		{"reset", StepAction{Type: "command", Device: "breaker", Command: "reset"}, []string{CapabilityProcessTruth}, true},
		{"sequence", StepAction{Type: "sequence", Commands: []StepActionCmd{{Device: "breaker", Command: "clear_fault"}, {Device: "breaker", Command: "reset"}}}, []string{CapabilityProcessTruth}, true},
		{"not a supervisory command", StepAction{Type: "command", Device: "breaker", Command: "close"}, []string{CapabilityProcessTruth}, false},
		{"not a US device", StepAction{Type: "command", Device: "relay", Command: "inject_fault"}, []string{CapabilityProcessTruth}, false},
		{"US has no simulation vocabulary", StepAction{Type: "command", Device: "breaker", Command: "reset"}, electricalPackage, false},
		{"no capability", StepAction{Type: "command", Device: "breaker", Command: "reset"}, nil, false},
		{"session cut", StepAction{Type: "session-cut", Flow: "cc-rtu"}, []string{CapabilityPolicyContaind}, true},
		{"unknown flow", StepAction{Type: "session-cut", Flow: "missing"}, []string{CapabilityPolicyContaind}, false},
		{"empty flow", StepAction{Type: "session-cut"}, []string{CapabilityPolicyContaind}, false},
		{"no firewall capability", StepAction{Type: "session-cut", Flow: "cc-rtu"}, []string{CapabilityProcessTruth}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			def, scenario := validatorFixture()
			def.Flows = []TopologyFlow{{ID: "cc-rtu"}}
			scenario.Steps[0].Action = &test.action
			err := ValidateLab(def, []ScenarioYAML{scenario}, test.capabilities, usPolicies, testValidators)
			if (err == nil) != test.valid {
				t.Fatalf("valid = %v, error = %v", test.valid, err)
			}
		})
	}
}
