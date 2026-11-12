package labs

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// LabYAML mirrors the YAML schema for a package's topology template.
type LabYAML struct {
	ID             string                     `yaml:"id"`
	Name           string                     `yaml:"name"`
	Description    string                     `yaml:"description"`
	FirewallConfig string                     `yaml:"firewall_config"` // path relative to the topology file's directory
	Networks       []NetworkYAML              `yaml:"networks"`
	Nodes          []NodeYAML                 `yaml:"nodes"`
	Flows          []TopologyFlow             `yaml:"flows"`
	Liveness       map[string]LivenessBinding `yaml:"liveness"`
	// Scenarios is accepted only as an empty list. A package's scenarios
	// directory is its only curriculum source; inline scenarios are a load
	// error so a topology cannot smuggle exercises past package ownership.
	Scenarios []any `yaml:"scenarios"`
}

// NetworkYAML defines a virtual network.
type NetworkYAML struct {
	ID          string               `yaml:"id" json:"id,omitempty"`
	Name        string               `yaml:"name" json:"name"`
	Purdue      string               `yaml:"purdue" json:"purdue"`
	TargetSL    *TargetSecurityLevel `yaml:"target_sl,omitempty" json:"target_sl,omitempty"`
	CIDR        string               `yaml:"cidr" json:"cidr,omitempty"`
	Subnet      string               `yaml:"subnet" json:"subnet,omitempty"`
	Zone        string               `yaml:"zone" json:"zone,omitempty"`
	Description string               `yaml:"description" json:"description,omitempty"`
}

// TargetSecurityLevel is an integer design target, never a measured level.
type TargetSecurityLevel int

// UnmarshalYAML rejects non-integer scalars rather than silently truncating
// fractions as yaml.v3's ordinary int decoding does.
func (sl *TargetSecurityLevel) UnmarshalYAML(value *yaml.Node) error {
	if value.Tag != "!!int" {
		return fmt.Errorf("target_sl must be an integer (1-4)")
	}
	return value.Decode((*int)(sl))
}

// NodeYAML describes a node template in YAML.
type NodeYAML struct {
	ID        string   `yaml:"id" json:"id"`
	Type      string   `yaml:"type" json:"type"`
	Name      string   `yaml:"name" json:"name"`
	Networks  []string `yaml:"networks" json:"networks"`
	IP        string   `yaml:"ip" json:"ip,omitempty"`
	Container string   `yaml:"container" json:"container,omitempty"`
}

// ScenarioYAML defines scenario metadata loaded from YAML.
type ScenarioYAML struct {
	ID          string         `yaml:"id"`
	Name        string         `yaml:"name"`
	Summary     string         `yaml:"summary"`
	Description string         `yaml:"description"`
	Nodes       []string       `yaml:"nodes,omitempty"`
	Tags        []string       `yaml:"tags"`
	Steps       []ScenarioStep `yaml:"steps"`
	// EstimatedMinutes is the golden-path time budget for the lab (Guided
	// track, skipping Advanced hints and optional drill-downs). Surfaced as a
	// chip on the exercise card. Optional; 0/absent renders no chip.
	EstimatedMinutes int `yaml:"estimated_minutes,omitempty"`
	// Validator names the registered Go validator behind
	// POST /api/scenarios/:id/validate. Empty means the scenario has no
	// validation result.
	Validator string `yaml:"validator,omitempty"`
}

// ScenarioStep describes a single scenario instruction.
type ScenarioStep struct {
	// ID is the authored, stable step identifier (unique in its scenario).
	// Progress and references use it; step execution stays index-based.
	ID             string      `yaml:"id" json:"id"`
	Title          string      `yaml:"title" json:"title"`
	Description    string      `yaml:"description" json:"description"`
	ExpectedConfig string      `yaml:"expected_config,omitempty" json:"expected_config,omitempty"`
	Action         *StepAction `yaml:"action,omitempty" json:"action,omitempty"`
	Node           string      `yaml:"node,omitempty" json:"node,omitempty"`
}

// StepAction defines an executable action for a scenario step.
type StepAction struct {
	Type     string          `yaml:"type" json:"type"`                             // "check", "firewall", "sequence", "decision", "probe", "session-cut"
	Flow     string          `yaml:"flow,omitempty" json:"flow,omitempty"`         // for type=session-cut
	Config   string          `yaml:"config,omitempty" json:"config,omitempty"`     // for type=firewall
	Expect   map[string]any  `yaml:"expect,omitempty" json:"expect,omitempty"`     // for type=check
	Targets  []ProbeTarget   `yaml:"targets,omitempty" json:"targets,omitempty"`   // for type=probe
	Commands []StepActionCmd `yaml:"commands,omitempty" json:"commands,omitempty"` // for type=sequence
	Run      []StepRun       `yaml:"run,omitempty" json:"run,omitempty"`           // for type=check
}

// StepRun is one wire command a check presumes the student already sent in
// this step. The workshop test suite replays the list, in order, on each
// entry's node through the exec path, then lets state propagate before it
// evaluates the check (the check stays the oracle; a non-zero exit is noted,
// not a failure). Interactive Execute Step ignores Run and stays read-only.
type StepRun struct {
	Node    string `yaml:"node" json:"node"`
	Command string `yaml:"command" json:"command"`
}

// ProbeTarget identifies a TCP destination and optional source override.
type ProbeTarget struct {
	From string `yaml:"from,omitempty" json:"from,omitempty"`
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
	Note string `yaml:"note,omitempty" json:"note,omitempty"`
}

// StepActionCmd is a single command in a sequence action.
type StepActionCmd struct {
	Device  string   `yaml:"device" json:"device"`
	Command string   `yaml:"command" json:"command"`
	Source  string   `yaml:"source,omitempty" json:"source,omitempty"`
	Value   *float64 `yaml:"value,omitempty" json:"value,omitempty"`
}

// DecisionRole defines a team with a finite capacity for the decision exercise.
type DecisionRole struct {
	Name          string `yaml:"name" json:"name"`
	CapacityHours int    `yaml:"capacity_hours" json:"capacity_hours"`
}

// DecisionAction is a single remediation choice in the decision catalog.
type DecisionAction struct {
	ID          string   `yaml:"id" json:"id"`
	Title       string   `yaml:"title" json:"title"`
	Why         string   `yaml:"why" json:"why"`
	EffortHours int      `yaml:"effort_hours" json:"effort_hours"`
	Roles       []string `yaml:"roles" json:"roles"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}
