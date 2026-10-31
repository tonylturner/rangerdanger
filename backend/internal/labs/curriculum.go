package labs

// CurriculumBinding is the small package.yml binding. Roles are scenario IDs;
// dynamicSteps are step IDs in the implementation scenario, never title matches.
type CurriculumBinding struct {
	Dir          string                        `yaml:"dir" json:"-"`
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
	Plan              *CurriculumPlan `yaml:"plan" json:"-"`
	RequirementOrder  []string        `yaml:"requirementOrder" json:"requirementOrder"`
	GroupOrder        []string        `yaml:"groupOrder" json:"groupOrder"`
	ImpactOrder       []string        `yaml:"impactOrder" json:"impactOrder"`
	CurriculumBinding `yaml:"-"`
	Schema            int                              `yaml:"schema" json:"schema"`
	ICSRules          string                           `yaml:"-" json:"icsRules"`
	Requirements      map[string]CurriculumRequirement `yaml:"requirements" json:"requirements"`
	Readiness         []CurriculumReadiness            `yaml:"readiness" json:"readiness"`
	ReadinessFlags    []CurriculumReadinessFlag        `yaml:"readinessFlags" json:"readinessFlags"`
	BudgetAdjustments []CurriculumBudgetAdjustment     `yaml:"budgetAdjustments" json:"budgetAdjustments"`
	BudgetHours       int                              `yaml:"budgetHours" json:"budgetHours"`
	Teams             []DecisionRole                   `yaml:"teams" json:"teams"`
	Catalog           []DecisionAction                 `yaml:"catalog" json:"catalog"`
	Impact            map[string]CurriculumImpact      `yaml:"impact" json:"impact"`
	Groups            map[string]CurriculumGroup       `yaml:"groups" json:"groups"`
	PlanningOnly      []string                         `yaml:"planningOnly" json:"planningOnly"`
	Phases            map[string]string                `yaml:"phases" json:"phases"`
	NoPlan            string                           `yaml:"noPlan" json:"noPlan"`
	Tests             []CurriculumTest                 `yaml:"tests" json:"tests"`
	NegativeEmpty     string                           `yaml:"negativeEmpty" json:"negativeEmpty"`
	NodeMap           map[string]CurriculumNodes       `yaml:"nodeMap" json:"nodeMap"`
	NodeInference     []CurriculumNodeInference        `yaml:"nodeInference" json:"nodeInference"`
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
type CurriculumGroup struct {
	Title       string `yaml:"title" json:"title"`
	Description string `yaml:"description" json:"description"`
	Effect      string `yaml:"effect" json:"effect"`
}
type CurriculumTest struct {
	Kind string `yaml:"kind" json:"kind"`
	Text string `yaml:"text" json:"text"`
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

// CurriculumBudgetAdjustment constrains a source/verdict pair. Caps combine by
// minimum; effort multiplies first, then adds overhead, rounding only once.
type CurriculumBudgetAdjustment struct {
	Source   string                       `yaml:"source" json:"source"`
	Verdict  string                       `yaml:"verdict" json:"verdict"`
	TeamCaps map[string]int               `yaml:"teamCaps" json:"teamCaps"`
	Effort   []CurriculumEffortAdjustment `yaml:"effort" json:"effort"`
}

// CurriculumEffortAdjustment adds shared action effort after all multipliers.
type CurriculumEffortAdjustment struct {
	Actions    []string `yaml:"actions" json:"actions"`
	Multiplier float64  `yaml:"multiplier" json:"multiplier"`
	ExtraHours int      `yaml:"extraHours" json:"extraHours"`
}
