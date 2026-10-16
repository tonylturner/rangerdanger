package labs

import (
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
)

var validActionTypes = map[string]struct{}{
	"command": {}, "sequence": {}, "firewall": {}, "check": {}, "decision": {}, "probe": {}, "session-cut": {},
}

// vocabularyActionTypes are the action types checked against a
// capability's action vocabulary.
var vocabularyActionTypes = map[string]bool{"command": true, "sequence": true, "check": true}

// actionVocabulary is the device-command and check-key vocabulary a
// capability brings to command, sequence and check actions.
type actionVocabulary struct {
	commands      map[string][]string
	checkKeys     map[string]bool
	valueCommands map[string]bool
}

// capabilityVocabularies lists the action vocabulary each capability adds.
// Packages use their process capability to declare device operations.
var capabilityVocabularies = map[string]actionVocabulary{
	CapabilityProcessTruth: {
		checkKeys: map[string]bool{"breaker_closed": true, "supply_present": true, "cc_link_fresh": true, "rmu_normal": true, "oltc_auto": true, "firewall_config": true},
		commands: map[string][]string{
			"breaker": {"reset_lockout", "reset"},
			"rmu-1":   {"set_local", "set_remote", "earth_close", "earth_open", "reset_fault_passage", "reset"},
			"rmu-2":   {"set_local", "set_remote", "earth_close", "earth_open", "reset_fault_passage", "reset"},
			"oltc-1":  {"set_local", "set_remote", "block", "unblock", "reset"},
			"process": {"fault_s1", "fault_s2", "clear_fault", "reset_f2", "set_load", "set_source", "reset"},
			"gps":     {"set_offset", "reset_offset"},
		},
		valueCommands: map[string]bool{"set_load": true, "set_source": true, "set_offset": true},
	},
	CapabilityProcessElectrical: {
		// commands mirrors the device case tables in the services' sim
		// main.go files. Keep it here so validation does not depend on
		// importing service binaries into the backend.
		commands: map[string][]string{
			"relay":     {"trip", "close", "lockout", "unlock", "inject_fault", "clear_fault", "set_current", "set_voltage"},
			"recloser":  {"open", "close", "enable_reclose", "disable_reclose", "reset_lockout", "inject_fault", "clear_fault"},
			"regulator": {"raise_tap", "lower_tap", "set_manual", "set_auto", "set_setpoint", "set_tap"},
			"capbank":   {"switch_in", "switch_out", "set_auto", "set_manual", "reset_lockout", "set_thresh_low", "set_thresh_high"},
		},
		checkKeys: map[string]bool{
			"breaker_closed":  true,
			"loads_energized": true,
			"recloser_closed": true,
			"reclose_enabled": true,
			"voltage_normal":  true,
			"firewall_config": true,
		},
	},
}

// vocabularyFor returns the action vocabulary a package's capabilities
// provide, or nil when none of them brings one.
func vocabularyFor(capabilities []string) *actionVocabulary {
	for _, capability := range capabilities {
		if vocabulary, ok := capabilityVocabularies[capability]; ok {
			return &vocabulary
		}
	}
	return nil
}

type validationIssue struct {
	scenarioID string
	stepIndex  int
	title      string
	problem    string
}

func (i validationIssue) Error() string {
	scenarioID := i.scenarioID
	if scenarioID == "" {
		scenarioID = "<empty>"
	}
	return fmt.Sprintf("scenario %s step %d %q: %s", scenarioID, i.stepIndex, i.title, i.problem)
}

// ExpectedConfigHardened is the step expected_config alias for "the
// package's HardenedPolicy or a custom policy". The frontend policy banner
// and the test runner resolve it; a package must declare HardenedPolicy to
// use it.
const (
	ExpectedConfigHardened = "hardened"
	HardenedPolicy         = "improved"
)

// policyNames is the set of a package's named policies, for validation.
type policyNames map[string]string

// has reports whether name is one of the package's policies.
func (p policyNames) has(name string) bool {
	_, ok := p[name]
	return ok
}

// list is the policy names, sorted, for error messages.
func (p policyNames) list() string {
	names := make([]string, 0, len(p))
	for name := range p {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ValidateLab checks a package's scenarios against its topology, its
// declared capabilities, its named policies and the registered validators:
// IDs, node references, step IDs, validator keys and structured actions.
// Step indexes in errors are zero-based, matching the scenario executor.
func ValidateLab(def *LabYAML, scenarios []ScenarioYAML, capabilities []string, policies map[string]string, validators ValidatorRequirements) error {
	if def == nil {
		return fmt.Errorf("lab definition is nil")
	}

	nodeIDs := make(map[string]bool, len(def.Nodes))
	for _, node := range def.Nodes {
		nodeIDs[node.ID] = true
	}
	declared := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		declared[capability] = true
	}
	vocabulary := vocabularyFor(capabilities)
	names := policyNames(policies)

	issues := make([]validationIssue, 0)
	seenScenarioIDs := make(map[string]bool, len(scenarios))
	for _, scenario := range scenarios {
		contextTitle := "<scenario>"
		if len(scenario.Steps) > 0 {
			contextTitle = scenario.Steps[0].Title
			if contextTitle == "" {
				contextTitle = "<scenario>"
			}
		}
		add := func(stepIndex int, title, problem string) {
			issues = append(issues, validationIssue{
				scenarioID: scenario.ID,
				stepIndex:  stepIndex,
				title:      title,
				problem:    problem,
			})
		}
		addScenario := func(problem string) { add(0, contextTitle, problem) }

		if scenario.ID == "" {
			addScenario("scenario id must not be empty")
		} else if !slugPattern.MatchString(scenario.ID) {
			addScenario("scenario id must be " + slugRule)
		}
		if scenario.Name == "" {
			addScenario("scenario name must not be empty")
		}
		if scenario.Order == "" {
			addScenario("scenario order must not be empty")
		}
		if seenScenarioIDs[scenario.ID] {
			addScenario("scenario id must be unique within the package")
		}
		seenScenarioIDs[scenario.ID] = true
		if scenario.Validator != "" {
			required, known := validators[scenario.Validator]
			if !known {
				addScenario(fmt.Sprintf("unknown validator %q", scenario.Validator))
			}
			for _, capability := range required {
				if !declared[capability] {
					addScenario(fmt.Sprintf("validator %q requires capability %q, which the package does not declare", scenario.Validator, capability))
				}
			}
		}

		for _, nodeID := range scenario.Nodes {
			if !nodeIDs[nodeID] {
				addScenario(fmt.Sprintf("unknown scenario node %q", nodeID))
			}
		}

		seenStepIDs := make(map[string]bool, len(scenario.Steps))
		for stepIndex, step := range scenario.Steps {
			stepAdd := func(problem string) { add(stepIndex, step.Title, problem) }
			if step.ID == "" {
				stepAdd("step id must not be empty")
			} else if !slugPattern.MatchString(step.ID) {
				stepAdd(fmt.Sprintf("step id %q must be %s", step.ID, slugRule))
			} else if seenStepIDs[step.ID] {
				stepAdd(fmt.Sprintf("step id %q must be unique within the scenario", step.ID))
			}
			seenStepIDs[step.ID] = true
			if step.Title == "" {
				stepAdd("step title must not be empty")
			}
			if step.ExpectedConfig != "" && !validExpectedConfig(step.ExpectedConfig, names) {
				stepAdd(fmt.Sprintf("expected_config must be one of the package policies (%s) or %s, got %q", names.list(), ExpectedConfigHardened, step.ExpectedConfig))
			}
			if step.Node != "" && !nodeIDs[step.Node] {
				stepAdd(fmt.Sprintf("unknown node %q", step.Node))
			}
			if step.Action != nil {
				validateAction(*step.Action, step.Node, nodeIDs, vocabulary, names, stepAdd)
				if step.Action.Type == "session-cut" {
					if !declared[CapabilityPolicyContaind] {
						stepAdd("session-cut requires policy.containd")
					}
					found := false
					for _, flow := range def.Flows {
						found = found || flow.ID == step.Action.Flow
					}
					if !found {
						stepAdd(fmt.Sprintf("unknown session-cut flow %q", step.Action.Flow))
					}
				}
			}
		}
	}

	if len(issues) == 0 {
		return nil
	}
	lines := make([]string, len(issues))
	for index, issue := range issues {
		lines[index] = issue.Error()
	}
	return fmt.Errorf("lab validation failed:\n%s", strings.Join(lines, "\n"))
}

// validExpectedConfig reports whether value names a package policy, or is
// the hardened alias of a package that declares the policy it stands for.
func validExpectedConfig(value string, names policyNames) bool {
	if value == ExpectedConfigHardened {
		return names.has(HardenedPolicy)
	}
	return names.has(value)
}

func validateAction(action StepAction, stepNode string, nodeIDs map[string]bool, vocabulary *actionVocabulary, names policyNames, add func(string)) {
	if _, ok := validActionTypes[action.Type]; !ok {
		add(fmt.Sprintf("unsupported action type %q", action.Type))
		return
	}
	if vocabularyActionTypes[action.Type] && vocabulary == nil {
		add(fmt.Sprintf("%s actions need a package capability with an action vocabulary (%s)", action.Type, CapabilityProcessElectrical))
		return
	}

	switch action.Type {
	case "command":
		vocabulary.validateCommand(action.Device, action.Command, action.Value, "", add)
	case "sequence":
		if len(action.Commands) == 0 {
			add("sequence must contain at least one command")
		}
		for index, command := range action.Commands {
			vocabulary.validateCommand(command.Device, command.Command, command.Value, fmt.Sprintf("commands[%d] ", index), add)
		}
	case "firewall":
		if !names.has(action.Config) {
			add(fmt.Sprintf("firewall config must be one of the package policies (%s), got %q", names.list(), action.Config))
		}
	case "check":
		if len(action.Expect) == 0 {
			add("check expect must not be empty")
		}
		keys := make([]string, 0, len(action.Expect))
		for key := range action.Expect {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := action.Expect[key]
			if !vocabulary.checkKeys[key] {
				add(fmt.Sprintf("unknown check key %q", key))
				continue
			}
			if key == "firewall_config" {
				config, ok := value.(string)
				if !ok {
					add("check firewall_config value must be a string")
				} else if !names.has(config) {
					add(fmt.Sprintf("check firewall_config must be one of the package policies (%s), got %q", names.list(), config))
				}
				continue
			}
			if _, ok := value.(bool); !ok {
				add(fmt.Sprintf("check %s value must be a boolean", key))
			}
		}
	case "probe":
		if action.Outcome != "reachable" && action.Outcome != "blocked" {
			add("probe outcome must be reachable or blocked")
		}
		if len(action.Expect) != 0 {
			add("probe expect must be empty")
		}
		if len(action.Targets) == 0 {
			add("probe must contain at least one target")
		}
		for index, target := range action.Targets {
			prefix := fmt.Sprintf("targets[%d] ", index)
			if ip := net.ParseIP(target.Host); ip == nil || ip.To4() == nil || strings.Contains(target.Host, ":") {
				add(prefix + "host must be a valid IPv4 address")
			}
			if target.Port < 1 || target.Port > 65535 {
				add(prefix + "port must be 1-65535")
			}
			source := target.From
			if source == "" {
				source = stepNode
			}
			if source == "" {
				add(prefix + "must have a source (step node or from)")
			} else if !nodeIDs[source] {
				add(prefix + fmt.Sprintf("unknown source node %q", source))
			}
		}
	case "decision":
		validateDecision(action, add)
	}
}

// ValidateTruthCommand checks the process.truth command wire, including values.
// Executors use the same vocabulary as the curriculum loader.
func ValidateTruthCommand(device, command string, value *float64) error {
	var problems []string
	vocabulary := capabilityVocabularies[CapabilityProcessTruth]
	vocabulary.validateCommand(device, command, value, "", func(problem string) { problems = append(problems, problem) })
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (v actionVocabulary) validateCommand(device, command string, value *float64, prefix string, add func(string)) {
	validateCommand(v.commands, device, command, prefix, add)
	if v.valueCommands == nil {
		return
	}
	required := v.valueCommands[command]
	if required != (value != nil) {
		add(prefix + "value is required exactly for set_load, set_source and set_offset")
		return
	}
	if value == nil {
		return
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) {
		add(prefix + "value must be finite")
	} else if command == "set_load" && (*value < 0.2 || *value > 3.0) {
		add(prefix + "set_load value must be 0.2…3.0")
	} else if command == "set_source" && (*value < 0.90 || *value > 1.10) {
		add(prefix + "set_source value must be 0.90…1.10")
	}
}

func validateCommand(vocabulary map[string][]string, device, command, prefix string, add func(string)) {
	if command == "" {
		add(prefix + "command name must not be empty")
	}
	if device == "" {
		add(prefix + "command device must not be empty")
		return
	}
	commands, ok := vocabulary[device]
	if !ok {
		add(fmt.Sprintf("%sunknown device %q", prefix, device))
		return
	}
	if command == "" {
		return
	}
	for _, validCommand := range commands {
		if command == validCommand {
			return
		}
	}
	add(fmt.Sprintf("%sunknown command %q for device %q", prefix, command, device))
}

func validateDecision(action StepAction, add func(string)) {
	if action.BudgetHours <= 0 {
		add("decision budget_hours must be greater than zero")
	}
	roleNames := make(map[string]bool, len(action.Roles))
	hasCapacity := false
	for _, role := range action.Roles {
		roleNames[role.Name] = true
		if role.CapacityHours > 0 {
			hasCapacity = true
		}
	}
	if !hasCapacity {
		add("decision must have at least one role with capacity_hours greater than zero")
	}
	if len(action.Actions) == 0 {
		add("decision must have at least one action")
	}

	seenActionIDs := make(map[string]bool, len(action.Actions))
	for _, decisionAction := range action.Actions {
		if decisionAction.ID == "" {
			add("decision action id must not be empty")
		} else if seenActionIDs[decisionAction.ID] {
			add(fmt.Sprintf("duplicate decision action id %q", decisionAction.ID))
		}
		seenActionIDs[decisionAction.ID] = true
		if decisionAction.EffortHours <= 0 {
			add(fmt.Sprintf("decision action %q effort_hours must be greater than zero", decisionAction.ID))
		}
		for _, roleName := range decisionAction.Roles {
			if !roleNames[roleName] {
				add(fmt.Sprintf("decision action %q references unknown role %q", decisionAction.ID, roleName))
			}
		}
	}
}
