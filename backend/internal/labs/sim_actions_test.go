package labs

import (
	"fmt"
	"math"
	"testing"
)

func TestSimulationActionVocabulary(t *testing.T) {
	tests := []struct {
		name         string
		action       StepAction
		capabilities []string
		valid        bool
	}{
		{"fault", StepAction{Type: "command", Device: "process", Command: "fault_s1"}, []string{CapabilityProcessTruth}, true},
		{"clear", StepAction{Type: "command", Device: "process", Command: "clear_fault"}, []string{CapabilityProcessTruth}, true},
		{"reset", StepAction{Type: "command", Device: "breaker", Command: "reset"}, []string{CapabilityProcessTruth}, true},
		{"sequence", StepAction{Type: "sequence", Commands: []StepActionCmd{{Device: "process", Command: "clear_fault"}, {Device: "breaker", Command: "reset"}}}, []string{CapabilityProcessTruth}, true},
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

func TestTruthVocabularyValues(t *testing.T) {
	vocabulary := capabilityVocabularies[CapabilityProcessTruth]
	for device, commands := range vocabulary.commands {
		for _, command := range commands {
			t.Run(device+"/"+command, func(t *testing.T) {
				var value *float64
				if vocabulary.valueCommands[command] {
					v := 1.0
					value = &v
				}
				if err := ValidateTruthCommand(device, command, value); err != nil {
					t.Fatal(err)
				}
				if value == nil {
					v := 1.0
					value = &v
				} else {
					value = nil
				}
				if err := ValidateTruthCommand(device, command, value); err == nil {
					t.Fatal("accepted wrong value presence")
				}
			})
		}
	}
	for _, command := range []string{"set_load", "set_source", "set_offset"} {
		device := "process"
		if command == "set_offset" {
			device = "gps"
		}
		for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			if err := ValidateTruthCommand(device, command, &value); err == nil {
				t.Errorf("accepted %s %v", command, value)
			}
		}
	}
	for _, test := range []struct {
		command string
		value   float64
		valid   bool
	}{
		{"set_load", .2, true}, {"set_load", 3, true}, {"set_load", .19, false}, {"set_load", 3.01, false},
		{"set_source", .9, true}, {"set_source", 1.1, true}, {"set_source", .89, false}, {"set_source", 1.11, false},
	} {
		t.Run(fmt.Sprintf("%s/%v", test.command, test.value), func(t *testing.T) {
			def, scenario := validatorFixture()
			scenario.Steps[0].Action = &StepAction{Type: "sequence", Commands: []StepActionCmd{{Device: "process", Command: test.command, Value: &test.value}}}
			err := ValidateLab(def, []ScenarioYAML{scenario}, []string{CapabilityProcessTruth}, usPolicies, testValidators)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
	for _, command := range []string{"inject_fault", "clear_fault"} {
		if ValidateTruthCommand("breaker", command, nil) == nil {
			t.Errorf("accepted old breaker action %s", command)
		}
	}
}
