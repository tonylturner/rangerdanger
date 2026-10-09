package labs

import (
	"strings"
	"testing"
)

// testValidators stands in for the server's registry.
var testValidators = ValidatorRequirements{
	"electrical-check": {CapabilityProcessElectrical},
	"audited-policy":   {CapabilityPolicyContaind, CapabilityAuditDeviceControl},
}

var electricalPackage = []string{CapabilityProcessElectrical}

func validatorFixture() (*LabYAML, ScenarioYAML) {
	def := &LabYAML{
		ID:    "substation",
		Nodes: []NodeYAML{{ID: "rtac-1"}, {ID: "fw-1"}},
	}
	scenario := ScenarioYAML{
		ID: "baseline", Name: "Baseline", Order: "1.1",
		Steps: []ScenarioStep{{ID: "review-policy", Title: "Review policy"}},
	}
	return def, scenario
}

// validateElectrical validates scenarios as a process.electrical package.
func validateElectrical(def *LabYAML, scenarios ...ScenarioYAML) error {
	return ValidateLab(def, scenarios, electricalPackage, testValidators)
}

func TestValidateLabRejectsRuntimeVocabularyViolations(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*LabYAML, *ScenarioYAML)
		problem string
	}{
		{
			name: "unknown action type",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "manual"}
			},
			problem: `unsupported action type "manual"`,
		},
		{
			name: "bad expected config",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].ExpectedConfig = "improved"
			},
			problem: "expected_config must be weak or hardened",
		},
		{
			name: "unknown step node",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Node = "missing-node"
			},
			problem: `unknown node "missing-node"`,
		},
		{
			name: "unknown scenario node",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Nodes = []string{"missing-node"}
			},
			problem: `unknown scenario node "missing-node"`,
		},
		{
			name: "unknown command device",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "command", Device: "pump", Command: "start"}
			},
			problem: `unknown device "pump"`,
		},
		{
			name: "unknown command for known device",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "command", Device: "relay", Command: "reboot"}
			},
			problem: `unknown command "reboot" for device "relay"`,
		},
		{
			name: "bad firewall config",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "firewall", Config: "hardened"}
			},
			problem: "firewall config must be weak or improved",
		},
		{
			name: "unknown check key",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "check", Expect: map[string]any{"breakers_closed": true}}
			},
			problem: `unknown check key "breakers_closed"`,
		},
		{
			name: "wrong check value type",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "check", Expect: map[string]any{"breaker_closed": "yes"}}
			},
			problem: "check breaker_closed value must be a boolean",
		},
		{
			name: "bad firewall check value",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "check", Expect: map[string]any{"firewall_config": "hardened"}}
			},
			problem: "check firewall_config must be weak or improved",
		},
		{
			name: "empty check expect",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "check"}
			},
			problem: "check expect must not be empty",
		},
		{
			name: "empty sequence",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "sequence"}
			},
			problem: "sequence must contain at least one command",
		},
		{
			name: "bad command in sequence",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = &StepAction{Type: "sequence", Commands: []StepActionCmd{{Device: "relay", Command: "explode"}}}
			},
			problem: `commands[0] unknown command "explode" for device "relay"`,
		},
		{
			name: "decision role mismatch",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].Action = validDecision()
				scenario.Steps[0].Action.Actions[0].Roles = []string{"Missing role"}
			},
			problem: `decision action "plan" references unknown role "Missing role"`,
		},
		{
			name: "unknown validator",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Validator = "no-such-validator"
			},
			problem: `unknown validator "no-such-validator"`,
		},
		{
			name: "validator capability missing from package",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Validator = "audited-policy"
			},
			problem: `validator "audited-policy" requires capability "policy.containd", which the package does not declare`,
		},
		{
			name: "missing step id",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].ID = ""
			},
			problem: "step id must not be empty",
		},
		{
			name: "step id not a slug",
			mutate: func(_ *LabYAML, scenario *ScenarioYAML) {
				scenario.Steps[0].ID = "Review_Policy"
			},
			problem: `step id "Review_Policy" must be lowercase letters and digits separated by single hyphens`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			def, scenario := validatorFixture()
			test.mutate(def, &scenario)
			err := validateElectrical(def, scenario)
			if err == nil {
				t.Fatal("ValidateLab() error = nil, want validation failure")
			}
			for _, want := range []string{"scenario baseline", "step 0", `"Review policy"`, test.problem} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidateLab() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestValidateLabRejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*LabYAML, *ScenarioYAML)
		problem string
	}{
		{"scenario id", func(_ *LabYAML, scenario *ScenarioYAML) { scenario.ID = "" }, "scenario id must not be empty"},
		{"scenario name", func(_ *LabYAML, scenario *ScenarioYAML) { scenario.Name = "" }, "scenario name must not be empty"},
		{"scenario order", func(_ *LabYAML, scenario *ScenarioYAML) { scenario.Order = "" }, "scenario order must not be empty"},
		{"step title", func(_ *LabYAML, scenario *ScenarioYAML) { scenario.Steps[0].Title = "" }, "step title must not be empty"},
		{"command device", func(_ *LabYAML, scenario *ScenarioYAML) {
			scenario.Steps[0].Action = &StepAction{Type: "command", Command: "trip"}
		}, "command device must not be empty"},
		{"command name", func(_ *LabYAML, scenario *ScenarioYAML) {
			scenario.Steps[0].Action = &StepAction{Type: "command", Device: "relay"}
		}, "command name must not be empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			def, scenario := validatorFixture()
			test.mutate(def, &scenario)
			err := validateElectrical(def, scenario)
			if err == nil || !strings.Contains(err.Error(), test.problem) {
				t.Fatalf("ValidateLab() error = %v, want %q", err, test.problem)
			}
		})
	}
}

func TestValidateLabAggregatesProblemsWithStepContext(t *testing.T) {
	def, scenario := validatorFixture()
	scenario.Steps[0].Action = &StepAction{
		Type: "check",
		Expect: map[string]any{
			"unknown":        true,
			"breaker_closed": "yes",
		},
	}
	scenario.Steps = append(scenario.Steps, ScenarioStep{
		ID: "bad-node", Title: "Bad node", Node: "not-in-template", ExpectedConfig: "improved",
	})
	err := validateElectrical(def, scenario)
	if err == nil {
		t.Fatal("ValidateLab() error = nil, want aggregated validation failure")
	}
	for _, want := range []string{
		`scenario baseline step 0 "Review policy": unknown check key "unknown"`,
		`scenario baseline step 0 "Review policy": check breaker_closed value must be a boolean`,
		`scenario baseline step 1 "Bad node": unknown node "not-in-template"`,
		`scenario baseline step 1 "Bad node": expected_config must be weak or hardened`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregated error %q does not contain %q", err, want)
		}
	}
}

func TestValidateLabRejectsInvalidDecisionFields(t *testing.T) {
	def, scenario := validatorFixture()
	decision := validDecision()
	decision.BudgetHours = 0
	decision.Roles[0].CapacityHours = 0
	decision.Actions[0].ID = ""
	decision.Actions[0].EffortHours = 0
	decision.Actions = append(decision.Actions,
		DecisionAction{ID: "plan", EffortHours: 2, Roles: []string{"Operator"}},
		DecisionAction{ID: "plan", EffortHours: 2, Roles: []string{"Operator"}},
	)
	scenario.Steps[0].Action = decision
	err := validateElectrical(def, scenario)
	if err == nil {
		t.Fatal("ValidateLab() error = nil, want decision validation failure")
	}
	for _, want := range []string{
		"decision budget_hours must be greater than zero",
		"decision must have at least one role with capacity_hours greater than zero",
		"decision action id must not be empty",
		"decision action \"\" effort_hours must be greater than zero",
		`duplicate decision action id "plan"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("decision error %q does not contain %q", err, want)
		}
	}
}

func TestValidateLabRejectsDecisionWithoutActions(t *testing.T) {
	def, scenario := validatorFixture()
	scenario.Steps[0].Action = &StepAction{
		Type: "decision", BudgetHours: 40,
		Roles: []DecisionRole{{Name: "Operator", CapacityHours: 8}},
	}
	err := validateElectrical(def, scenario)
	if err == nil || !strings.Contains(err.Error(), "decision must have at least one action") {
		t.Fatalf("ValidateLab() error = %v, want missing decision actions", err)
	}
}

func TestValidateProbe(t *testing.T) {
	valid := func() (*LabYAML, ScenarioYAML) {
		def, scenario := validatorFixture()
		scenario.Steps[0].Node = "rtac-1"
		scenario.Steps[0].Action = &StepAction{Type: "probe", Outcome: "reachable", Targets: []ProbeTarget{{Host: "10.40.40.20", Port: 502}}}
		return def, scenario
	}
	tests := []struct {
		name    string
		mutate  func(*ScenarioStep)
		problem string
	}{
		{"invalid outcome", func(s *ScenarioStep) { s.Action.Outcome = "allow" }, "probe outcome must be reachable or blocked"},
		{"check expect on probe", func(s *ScenarioStep) { s.Action.Expect = map[string]any{"breaker_closed": true} }, "probe expect must be empty"},
		{"no targets", func(s *ScenarioStep) { s.Action.Targets = nil }, "probe must contain at least one target"},
		{"bad host", func(s *ScenarioStep) { s.Action.Targets[0].Host = "example.com" }, "host must be a valid IPv4"},
		{"IPv6 host", func(s *ScenarioStep) { s.Action.Targets[0].Host = "::1" }, "host must be a valid IPv4"},
		{"zero port", func(s *ScenarioStep) { s.Action.Targets[0].Port = 0 }, "port must be 1-65535"},
		{"large port", func(s *ScenarioStep) { s.Action.Targets[0].Port = 65536 }, "port must be 1-65535"},
		{"missing source", func(s *ScenarioStep) { s.Node = "" }, "must have a source"},
		{"unknown override", func(s *ScenarioStep) { s.Action.Targets[0].From = "unknown" }, `unknown source node "unknown"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, scenario := valid()
			tt.mutate(&scenario.Steps[0])
			err := validateElectrical(def, scenario)
			if err == nil || !strings.Contains(err.Error(), tt.problem) {
				t.Fatalf("ValidateLab error = %v, want %q", err, tt.problem)
			}
		})
	}
	def, scenario := valid()
	scenario.Steps[0].Node = ""
	scenario.Steps[0].Action.Targets[0].From = "fw-1"
	if err := validateElectrical(def, scenario); err != nil {
		t.Fatalf("override-only probe rejected: %v", err)
	}
}

func validDecision() *StepAction {
	return &StepAction{
		Type: "decision", BudgetHours: 40,
		Roles:   []DecisionRole{{Name: "Operator", CapacityHours: 8}},
		Actions: []DecisionAction{{ID: "plan", EffortHours: 2, Roles: []string{"Operator"}}},
	}
}

func TestValidateLabAcceptsDeclaredValidator(t *testing.T) {
	def, scenario := validatorFixture()
	scenario.Validator = "electrical-check"
	if err := validateElectrical(def, scenario); err != nil {
		t.Fatalf("ValidateLab() = %v, want a known validator with its capability to pass", err)
	}
	scenario.Validator = ""
	if err := validateElectrical(def, scenario); err != nil {
		t.Fatalf("ValidateLab() = %v, want a scenario without a validator to pass", err)
	}
}

func TestValidateLabRejectsDuplicateIDs(t *testing.T) {
	def, scenario := validatorFixture()
	scenario.Steps = append(scenario.Steps, ScenarioStep{ID: "review-policy", Title: "Again"})
	err := validateElectrical(def, scenario)
	if err == nil || !strings.Contains(err.Error(), `step 1 "Again": step id "review-policy" must be unique within the scenario`) {
		t.Fatalf("ValidateLab() error = %v, want duplicate step id", err)
	}

	def, first := validatorFixture()
	_, second := validatorFixture()
	err = validateElectrical(def, first, second)
	if err == nil || !strings.Contains(err.Error(), "scenario id must be unique within the package") {
		t.Fatalf("ValidateLab() error = %v, want duplicate scenario id", err)
	}
}

func TestValidateLabRejectsNonSlugScenarioID(t *testing.T) {
	def, scenario := validatorFixture()
	scenario.ID = "us--baseline"
	err := validateElectrical(def, scenario)
	if err == nil || !strings.Contains(err.Error(), "scenario id must be lowercase letters and digits separated by single hyphens") {
		t.Fatalf("ValidateLab() error = %v, want slug error", err)
	}
}

func TestValidateLabGatesActionVocabularyOnCapability(t *testing.T) {
	for _, action := range []*StepAction{
		{Type: "command", Device: "relay", Command: "trip"},
		{Type: "sequence", Commands: []StepActionCmd{{Device: "relay", Command: "trip"}}},
		{Type: "check", Expect: map[string]any{"breaker_closed": true}},
	} {
		t.Run(action.Type, func(t *testing.T) {
			def, scenario := validatorFixture()
			scenario.Steps[0].Action = action
			if err := validateElectrical(def, scenario); err != nil {
				t.Fatalf("electrical package rejected %s: %v", action.Type, err)
			}
			err := ValidateLab(def, []ScenarioYAML{scenario}, []string{CapabilityPolicyContaind}, testValidators)
			want := action.Type + " actions need a package capability with an action vocabulary (process.electrical)"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("ValidateLab() error = %v, want %q", err, want)
			}
		})
	}
}
