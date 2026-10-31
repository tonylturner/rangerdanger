package labs

import "encoding/json"

// CurriculumPlan is the ordered firewall program and its finite proof universe.
// Catalog actions absent from Rules and Detect are planning-only.
type CurriculumPlan struct {
	Rules  []PlanSlot            `yaml:"rules" json:"rules"`
	Detect map[string]PlanDetect `yaml:"detect" json:"detect"`
	Proof  []PlanProof           `yaml:"proof" json:"proof"`
}

// PlanSlot is either one required rule or one action's selected/deferred rules.
// An explicit empty Selected list closes a path through a later/default deny.
type PlanSlot struct {
	Required *PlanRule  `yaml:"required,omitempty" json:"required,omitempty"`
	Action   string     `yaml:"action,omitempty" json:"action,omitempty"`
	Selected []PlanRule `yaml:"selected" json:"selected,omitempty"`
	Deferred []PlanRule `yaml:"deferred,omitempty" json:"deferred,omitempty"`
}

// MarshalYAML retains the distinction between a required slot with no variants
// and an action slot with an explicitly empty selected variant.
func (slot PlanSlot) MarshalYAML() (any, error) {
	if slot.Required != nil {
		return struct {
			Required *PlanRule `yaml:"required"`
		}{slot.Required}, nil
	}
	return struct {
		Action   string     `yaml:"action"`
		Selected []PlanRule `yaml:"selected"`
		Deferred []PlanRule `yaml:"deferred"`
	}{slot.Action, slot.Selected, slot.Deferred}, nil
}

// PlanRule deliberately excludes predicates the L4 evaluator cannot model,
// such as identities and schedules. ICS is detection metadata, not L4 policy.
type PlanRule struct {
	ID           string         `yaml:"id" json:"id"`
	Description  string         `yaml:"description,omitempty" json:"description,omitempty"`
	SourceZones  []string       `yaml:"sourceZones,omitempty" json:"sourceZones,omitempty"`
	DestZones    []string       `yaml:"destZones,omitempty" json:"destZones,omitempty"`
	Sources      []string       `yaml:"sources,omitempty" json:"sources,omitempty"`
	Destinations []string       `yaml:"destinations,omitempty" json:"destinations,omitempty"`
	Protocols    []PlanProtocol `yaml:"protocols,omitempty" json:"protocols,omitempty"`
	ICS          *PlanICS       `yaml:"ics,omitempty" json:"ics,omitempty"`
	Action       string         `yaml:"action" json:"action"`
	Log          bool           `yaml:"log,omitempty" json:"log,omitempty"`
}

// PlanProtocol matches a protocol and optionally a single port or port range.
type PlanProtocol struct {
	Name string `yaml:"name" json:"name"`
	Port string `yaml:"port,omitempty" json:"port,omitempty"`
}

// PlanICS is the plan's limited containd predicate (Modbus, DNP3 and IEC 104).
// UnitID and Addresses are Modbus-only; DNP3 point groups are not modeled.
// ObjectClasses cannot be used until plans support CIP.
// Empty Mode inherits the policy envelope; overrides are monitor or enforce.
// The L4 source pin enforces; DPI detects and logs.
type PlanICS struct {
	Protocol          string         `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	FunctionCode      []int          `yaml:"functionCode,omitempty" json:"functionCode,omitempty"`
	UnitID            *uint8         `yaml:"unitId,omitempty" json:"unitId,omitempty"`
	Addresses         []string       `yaml:"addresses,omitempty" json:"addresses,omitempty"`
	ObjectClasses     []uint16       `yaml:"objectClasses,omitempty" json:"objectClasses,omitempty"`
	ReadOnly          bool           `yaml:"readOnly,omitempty" json:"readOnly,omitempty"`
	WriteOnly         *bool          `yaml:"writeOnly,omitempty" json:"writeOnly,omitempty"`
	Direction         string         `yaml:"direction,omitempty" json:"direction,omitempty"`
	Mode              string         `yaml:"mode,omitempty" json:"mode,omitempty"`
	TypeIDs           []int          `yaml:"typeIds,omitempty" json:"typeIds,omitempty"`
	Classes           []string       `yaml:"classes,omitempty" json:"classes,omitempty"`
	CA                []int          `yaml:"ca,omitempty" json:"ca,omitempty"`
	IOA               []PlanIOARange `yaml:"ioa,omitempty" json:"ioa,omitempty"`
	COT               []int          `yaml:"cot,omitempty" json:"cot,omitempty"`
	Originators       []int          `yaml:"originators,omitempty" json:"originators,omitempty"`
	Negative          *bool          `yaml:"negative,omitempty" json:"negative,omitempty"`
	Test              *bool          `yaml:"test,omitempty" json:"test,omitempty"`
	SelectExecute     string         `yaml:"selectExecute,omitempty" json:"selectExecute,omitempty"`
	CommandStates     []int          `yaml:"commandStates,omitempty" json:"commandStates,omitempty"`
	CommandQualifiers []int          `yaml:"commandQualifiers,omitempty" json:"commandQualifiers,omitempty"`
}

// PlanIOARange is an inclusive IEC 104 information-object address range.
type PlanIOARange struct {
	Start int `yaml:"start" json:"start"`
	End   int `yaml:"end" json:"end"`
}

// PlanDetect overlays metadata and appends standalone observation rules.
type PlanDetect struct {
	Rules []PlanRule         `yaml:"rules,omitempty" json:"rules,omitempty"`
	Log   []string           `yaml:"log,omitempty" json:"log,omitempty"`
	ICS   map[string]PlanICS `yaml:"ics,omitempty" json:"ics,omitempty"`
}

// PlanProof identifies a TCP tuple by topology identities, not copied addresses.
type PlanProof struct {
	ID       string   `yaml:"id" json:"id"`
	From     string   `yaml:"from" json:"from"`
	To       string   `yaml:"to" json:"to"`
	Network  string   `yaml:"network" json:"network"`
	Port     int      `yaml:"port" json:"port"`
	Category string   `yaml:"category" json:"category"`
	Expect   Verdict  `yaml:"expect" json:"expect"`
	Owners   []string `yaml:"owners" json:"owners"`
	Labs     []string `yaml:"labs,omitempty" json:"labs,omitempty"`
	Note     string   `yaml:"note" json:"note"`
}

// PlanActionKind separates enforcement, detection and planning-only work.
type PlanActionKind string

const (
	PlanActionEnforce PlanActionKind = "enforce"
	PlanActionDetect  PlanActionKind = "detect"
	PlanActionOnly    PlanActionKind = "plan"
)

// PlanAction describes every catalog action, in catalog order.
type PlanAction struct {
	ID     string         `json:"id"`
	Kind   PlanActionKind `json:"kind"`
	Chosen bool           `json:"chosen"`
}

// PlanOrigin explains why an emitted rule exists.
type PlanOrigin struct {
	Kind   string `json:"kind"`
	Action string `json:"action,omitempty"`
}

// CompiledPlanRule pairs the appliance rule with its authored origin.
type CompiledPlanRule struct {
	Rule   PlanRule   `json:"rule"`
	Origin PlanOrigin `json:"origin"`
}

// PlanExpectation compares the selected plan with the Hardened reference.
type PlanExpectation struct {
	ID              string   `json:"id"`
	From            string   `json:"from"`
	To              string   `json:"to"`
	Network         string   `json:"network"`
	Port            int      `json:"port"`
	Category        string   `json:"category"`
	Owners          []string `json:"owners"`
	Labs            []string `json:"labs,omitempty"`
	Note            string   `json:"note"`
	PlanVerdict     Verdict  `json:"planVerdict"`
	HardenedVerdict Verdict  `json:"hardenedVerdict"`
	Bridged         bool     `json:"bridged"`
}

// CompiledPlan is the single result used for display, export and application.
// Config retains the improved policy envelope. The server hashes Config when
// recording provenance; labs must not depend on the appliance client.
type CompiledPlan struct {
	Config       json.RawMessage    `json:"config"`
	Rules        []CompiledPlanRule `json:"rules"`
	Actions      []PlanAction       `json:"actions"`
	Label        string             `json:"label"`
	Expectations []PlanExpectation  `json:"expectations"`
}
