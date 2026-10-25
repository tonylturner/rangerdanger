package labs

// CurriculumBinding is the small package.yml binding. Roles are scenario IDs;
// dynamicSteps are step IDs in the implementation scenario, never title matches.
type CurriculumBinding struct {
	File         string                        `yaml:"file" json:"-"`
	Roles        map[string]string             `yaml:"roles" json:"roles"`
	Activities   map[string]CurriculumActivity `yaml:"activities" json:"activities"`
	DynamicSteps map[string]string             `yaml:"dynamicSteps" json:"dynamicSteps"`
}

// CurriculumActivity controls the planning surfaces for one scenario.
type CurriculumActivity struct {
	PolicyActions bool  `yaml:"policyActions" json:"policyActions"`
	HideBanner    bool  `yaml:"hideBanner" json:"hideBanner"`
	Validate      *bool `yaml:"validate" json:"validate,omitempty"`
}

// Curriculum is a strictly decoded, topology-resolved package planning model.
type Curriculum struct {
	Plan                   *CurriculumPlan  `yaml:"plan" json:"plan"`
	DPINotice              CurriculumNotice `yaml:"dpiNotice" json:"dpiNotice"`
	DPIForbiddenNotice     CurriculumNotice `yaml:"dpiForbiddenNotice" json:"dpiForbiddenNotice"`
	RequirementOrder       []string         `yaml:"requirementOrder" json:"requirementOrder"`
	GroupOrder             []string         `yaml:"groupOrder" json:"groupOrder"`
	ImpactOrder            []string         `yaml:"impactOrder" json:"impactOrder"`
	LoggingPrefixes        []string         `yaml:"loggingPrefixes" json:"loggingPrefixes"`
	CurriculumBinding      `yaml:"-"`
	Schema                 int                              `yaml:"schema" json:"schema"`
	ICSRules               string                           `yaml:"-" json:"icsRules"`
	Requirements           map[string]CurriculumRequirement `yaml:"requirements" json:"requirements"`
	Readiness              []CurriculumReadiness            `yaml:"readiness" json:"readiness"`
	ReadinessFlags         []CurriculumReadinessFlag        `yaml:"readinessFlags" json:"readinessFlags"`
	BudgetHours            int                              `yaml:"budgetHours" json:"budgetHours"`
	Teams                  []DecisionRole                   `yaml:"teams" json:"teams"`
	Catalog                []DecisionAction                 `yaml:"catalog" json:"catalog"`
	Impact                 map[string]CurriculumImpact      `yaml:"impact" json:"impact"`
	BaselineRules          []CurriculumDisplayRule          `yaml:"baselineRules" json:"baselineRules"`
	EnterpriseToVendorRule *CurriculumDisplayRule           `yaml:"enterpriseToVendorRule" json:"enterpriseToVendorRule"`
	Groups                 map[string]CurriculumGroup       `yaml:"groups" json:"groups"`
	PlanningOnly           []string                         `yaml:"planningOnly" json:"planningOnly"`
	Policy                 []CurriculumPolicyEntry          `yaml:"policy" json:"policy"`
	Phases                 map[string]string                `yaml:"phases" json:"phases"`
	NoPlan                 string                           `yaml:"noPlan" json:"noPlan"`
	Tests                  []CurriculumTest                 `yaml:"tests" json:"tests"`
	NegativeEmpty          string                           `yaml:"negativeEmpty" json:"negativeEmpty"`
	Flags                  CurriculumFlags                  `yaml:"flags" json:"flags"`
	NodeMap                map[string]CurriculumNodes       `yaml:"nodeMap" json:"nodeMap"`
	NodeInference          []CurriculumNodeInference        `yaml:"nodeInference" json:"nodeInference"`
	Interfaces             []CurriculumInterface            `yaml:"-" json:"interfaces"`
	Zones                  []CurriculumZone                 `yaml:"-" json:"zones"`
	// These explicit US compatibility semantics preserve known curriculum bugs.
	LegacyDisplayMutation bool `yaml:"legacyDisplayMutation" json:"legacyDisplayMutation"`
	LegacyEffects         bool `yaml:"legacyEffects" json:"legacyEffects"`
}

type CurriculumRequirement struct {
	Label string              `yaml:"label" json:"label"`
	Impl  map[string][]string `yaml:"impl" json:"impl"`
}
type CurriculumReadiness struct {
	ID    string `yaml:"id" json:"id"`
	Label string `yaml:"label" json:"label"`
	Good  string `yaml:"good" json:"good"`
}
type CurriculumReadinessFlag struct {
	Actions     []string `yaml:"actions" json:"actions"`
	Severity    string   `yaml:"severity" json:"severity"`
	Source      string   `yaml:"source" json:"source"`
	SourceLabel string   `yaml:"sourceLabel" json:"sourceLabel"`
	Verdict     string   `yaml:"verdict" json:"verdict"`
	Message     string   `yaml:"message" json:"message"`
}
type CurriculumImpact struct {
	Defends []string `yaml:"defends" json:"defends"`
	Note    string   `yaml:"note" json:"note"`
}
type CurriculumPredicate struct {
	Protocol      string          `yaml:"protocol" json:"protocol,omitempty"`
	FunctionCodes []int           `yaml:"functionCodes" json:"functionCodes,omitempty"`
	FunctionCode  []int           `yaml:"functionCode" json:"functionCode,omitempty"`
	Note          string          `yaml:"note" json:"note,omitempty"`
	ReadOnly      *bool           `yaml:"readOnly" json:"readOnly,omitempty"`
	TypeIDs       []int           `yaml:"typeIds" json:"typeIds,omitempty"`
	Classes       []string        `yaml:"classes" json:"classes,omitempty"`
	Direction     string          `yaml:"direction" json:"direction,omitempty"`
	CA            *int            `yaml:"ca" json:"ca,omitempty"`
	IOA           []CurriculumIOA `yaml:"ioa" json:"ioa,omitempty"`
	Mode          string          `yaml:"mode" json:"mode,omitempty"`
}
type CurriculumIOA struct {
	Start int `yaml:"start" json:"start"`
	End   int `yaml:"end" json:"end"`
}
type CurriculumDisplayRule struct {
	ID          string               `yaml:"id" json:"id"`
	Source      string               `yaml:"source" json:"source"`
	Destination string               `yaml:"destination" json:"destination"`
	Proto       string               `yaml:"proto" json:"proto"`
	Port        string               `yaml:"port" json:"port"`
	Action      string               `yaml:"action" json:"action"`
	Purpose     string               `yaml:"purpose" json:"purpose"`
	Log         bool                 `yaml:"log" json:"log,omitempty"`
	ICS         *CurriculumPredicate `yaml:"ics" json:"ics,omitempty"`
}
type CurriculumGroup struct {
	Title       string                  `yaml:"title" json:"title"`
	Description string                  `yaml:"description" json:"description"`
	Rules       []CurriculumDisplayRule `yaml:"rules" json:"rules"`
	Effect      string                  `yaml:"effect" json:"effect"`
	// OmittedRules are the scoped weak exposure when a closes/restricts action is deferred.
	OmittedRules []CurriculumDisplayRule `yaml:"omittedRules" json:"omittedRules,omitempty"`
}
type CurriculumPolicyEntry struct {
	Rule      CurriculumPolicyRule `yaml:"rule" json:"rule"`
	When      string               `yaml:"when" json:"when,omitempty"`
	Unless    string               `yaml:"unless" json:"unless,omitempty"`
	Logging   bool                 `yaml:"logging" json:"logging,omitempty"`
	Semantic  string               `yaml:"semantic" json:"semantic,omitempty"`
	Predicate *CurriculumPredicate `yaml:"predicate" json:"predicate,omitempty"`
}
type CurriculumPolicyRule struct {
	ID           string               `yaml:"id" json:"id"`
	Description  string               `yaml:"description" json:"description"`
	SourceZones  []string             `yaml:"sourceZones" json:"sourceZones,omitempty"`
	DestZones    []string             `yaml:"destZones" json:"destZones,omitempty"`
	Sources      []string             `yaml:"sources" json:"sources,omitempty"`
	Destinations []string             `yaml:"destinations" json:"destinations,omitempty"`
	Protocols    []CurriculumProtocol `yaml:"protocols" json:"protocols,omitempty"`
	State        []string             `yaml:"state" json:"state,omitempty"`
	ICS          *CurriculumPredicate `yaml:"ics" json:"ics,omitempty"`
	Action       string               `yaml:"action" json:"action"`
	Log          bool                 `yaml:"log" json:"log,omitempty"`
}
type CurriculumProtocol struct {
	Name string `yaml:"name" json:"name"`
	Port string `yaml:"port" json:"port"`
}
type CurriculumTest struct {
	Kind string `yaml:"kind" json:"kind"`
	When string `yaml:"when" json:"when,omitempty"`
	Text string `yaml:"text" json:"text"`
}
type CurriculumFlags struct {
	Logging    string   `yaml:"logging" json:"logging"`
	Objects    string   `yaml:"objects" json:"objects"`
	Validation string   `yaml:"validation" json:"validation"`
	DPI        []string `yaml:"dpi" json:"dpi"`
}
type CurriculumNodes struct {
	Primary string   `yaml:"primary" json:"primary"`
	Others  []string `yaml:"others" json:"others"`
}
type CurriculumNodeInference struct {
	Node    string `yaml:"node" json:"node"`
	IP      string `yaml:"ip" json:"ip"`
	Pattern string `yaml:"pattern" json:"pattern"`
}
type CurriculumInterface struct {
	Name   string           `json:"name"`
	Zone   string           `json:"zone"`
	Access CurriculumAccess `json:"access"`
}
type CurriculumZone struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CurriculumNotice is package-authored explanatory UI text.
type CurriculumNotice struct {
	Title       string `yaml:"title" json:"title"`
	Description string `yaml:"description" json:"description"`
}

// CurriculumAccess preserves appliance JSON property order as well as values.
type CurriculumAccess struct {
	Mgmt  bool `json:"mgmt,omitempty"`
	HTTP  bool `json:"http,omitempty"`
	HTTPS bool `json:"https,omitempty"`
	SSH   bool `json:"ssh,omitempty"`
}
