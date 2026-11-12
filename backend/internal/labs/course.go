package labs

// Course is the package's navigation and its optional teaching features.
type Course struct {
	Nodes       string         `yaml:"nodes" json:"-"`
	Modules     []CourseModule `yaml:"modules" json:"modules"`
	Features    CourseFeatures `yaml:"features" json:"features"`
	CourseNodes `yaml:"-"`
}

// CourseModule groups ordered activities; its title is data, not a UI heading.
type CourseModule struct {
	ID         string               `yaml:"id" json:"id"`
	Title      string               `yaml:"title" json:"title"`
	Activities []CurriculumActivity `yaml:"activities" json:"activities"`
}

// CourseFeatures is closed: unknown feature types fail strict decoding.
type CourseFeatures struct {
	SegmentationPlan *CurriculumBinding `yaml:"segmentationPlan" json:"segmentationPlan,omitempty"`
}

// CurriculumActivities binds the three distinct planning activities.
type CurriculumActivities struct {
	Requirements   string `yaml:"requirements" json:"requirements"`
	Planning       string `yaml:"planning" json:"planning"`
	Implementation string `yaml:"implementation" json:"implementation"`
}

// CourseNodes is generic runner routing, independent of planning.
type CourseNodes struct {
	NodeMap       map[string]CurriculumNodes `yaml:"nodeMap" json:"nodeMap"`
	NodeInference []CurriculumNodeInference  `yaml:"nodeInference" json:"nodeInference"`
}

// Activities returns the sole package-local presentation order.
func (c *Course) Activities() []CurriculumActivity {
	var out []CurriculumActivity
	if c != nil {
		for _, module := range c.Modules {
			out = append(out, module.Activities...)
		}
	}
	return out
}

// Activity returns an occurrence by its local scenario id.
func (c *Course) Activity(id string) (CurriculumActivity, bool) {
	for _, a := range c.Activities() {
		if a.Scenario == id {
			return a, true
		}
	}
	return CurriculumActivity{}, false
}

// SegmentationPlan returns the resolved optional planner model.
func (p Package) SegmentationPlan() *Curriculum {
	if p.Course == nil || p.Course.Features.SegmentationPlan == nil {
		return nil
	}
	return p.Course.Features.SegmentationPlan.Model
}
