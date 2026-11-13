package labs

// CurriculumBinding is the typed segmentation-plan feature.
type CurriculumBinding struct {
	Dir          string               `yaml:"dir" json:"-"`
	Activities   CurriculumActivities `yaml:"activities" json:"activities"`
	DynamicSteps map[string]string    `yaml:"dynamicSteps" json:"dynamicSteps"`
	Model        *Curriculum          `yaml:"-" json:"model"`
}

// CurriculumActivity is one ordered scenario occurrence, with tri-state flags.
type CurriculumActivity struct {
	Scenario      string `yaml:"scenario" json:"scenario"`
	Label         string `yaml:"label" json:"label"`
	PolicyActions *bool  `yaml:"policyActions,omitempty" json:"policyActions,omitempty"`
	HideBanner    *bool  `yaml:"hideBanner,omitempty" json:"hideBanner,omitempty"`
	Validate      *bool  `yaml:"validate,omitempty" json:"validate,omitempty"`
}

// Curriculum is a strictly decoded, topology-resolved package planning model.
type Curriculum struct {
	Plan              *CurriculumPlan                  `yaml:"plan" json:"-"`
	RequirementOrder  []string                         `yaml:"requirementOrder" json:"requirementOrder"`
	GroupOrder        []string                         `yaml:"groupOrder" json:"groupOrder"`
	ImpactOrder       []string                         `yaml:"impactOrder" json:"impactOrder"`
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
