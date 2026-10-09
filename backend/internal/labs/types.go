package labs

// LabYAML mirrors the YAML schema for lab templates.
type LabYAML struct {
	ID             string         `yaml:"id"`
	Name           string         `yaml:"name"`
	Description    string         `yaml:"description"`
	FirewallConfig string         `yaml:"firewall_config"` // path relative to lab-definitions dir
	Networks       []NetworkYAML  `yaml:"networks"`
	Nodes          []NodeYAML     `yaml:"nodes"`
	Scenarios      []ScenarioYAML `yaml:"scenarios"`
}

// NetworkYAML defines a virtual network.
type NetworkYAML struct {
	ID          string `yaml:"id" json:"id,omitempty"`
	Name        string `yaml:"name" json:"name"`
	CIDR        string `yaml:"cidr" json:"cidr,omitempty"`
	Subnet      string `yaml:"subnet" json:"subnet,omitempty"`
	Zone        string `yaml:"zone" json:"zone,omitempty"`
	Description string `yaml:"description" json:"description,omitempty"`
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
	Order       string         `yaml:"order"`
	Nodes       []string       `yaml:"nodes,omitempty"`
	Tags        []string       `yaml:"tags"`
	Steps       []ScenarioStep `yaml:"steps"`
	// EstimatedMinutes is the golden-path time budget for the lab (Guided
	// track, skipping Advanced hints and optional drill-downs). Surfaced as a
	// chip on the exercise card. Optional; 0/absent renders no chip.
	EstimatedMinutes int `yaml:"estimated_minutes,omitempty"`
}

// ScenarioStep describes a single scenario instruction.
type ScenarioStep struct {
	Title          string      `yaml:"title" json:"title"`
	Description    string      `yaml:"description" json:"description"`
	ExpectedConfig string      `yaml:"expected_config,omitempty" json:"expected_config,omitempty"`
	Action         *StepAction `yaml:"action,omitempty" json:"action,omitempty"`
	Node           string      `yaml:"node,omitempty" json:"node,omitempty"`
}

// StepAction defines an executable action for a scenario step.
type StepAction struct {
	Type     string          `yaml:"type" json:"type"`                             // "command", "check", "firewall", "sequence", "decision", "probe"
	Device   string          `yaml:"device,omitempty" json:"device,omitempty"`     // for type=command
	Command  string          `yaml:"command,omitempty" json:"command,omitempty"`   // for type=command
	Source   string          `yaml:"source,omitempty" json:"source,omitempty"`     // for type=command
	Value    *float64        `yaml:"value,omitempty" json:"value,omitempty"`       // for type=command (e.g. set_tap)
	Config   string          `yaml:"config,omitempty" json:"config,omitempty"`     // for type=firewall
	Expect   map[string]any  `yaml:"expect,omitempty" json:"expect,omitempty"`     // for type=check
	Outcome  string          `yaml:"outcome,omitempty" json:"outcome,omitempty"`   // for type=probe
	Targets  []ProbeTarget   `yaml:"targets,omitempty" json:"targets,omitempty"`   // for type=probe
	Commands []StepActionCmd `yaml:"commands,omitempty" json:"commands,omitempty"` // for type=sequence

	// Decision-action fields (for type=decision). Describes a constrained
	// remediation selection exercise with a labor budget and per-role capacity.
	BudgetHours int              `yaml:"budget_hours,omitempty" json:"budget_hours,omitempty"`
	Roles       []DecisionRole   `yaml:"roles,omitempty" json:"roles,omitempty"`
	Actions     []DecisionAction `yaml:"actions,omitempty" json:"actions,omitempty"`
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
