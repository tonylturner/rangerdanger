package labs

import (
	"math"
	"strings"
	"testing"
)

func TestCurriculumBudgetAdjustments(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*CurriculumBudgetAdjustment)
		want string
	}{
		{"source", func(a *CurriculumBudgetAdjustment) { a.Source = "foreign" }, "unknown source/verdict"},
		{"verdict", func(a *CurriculumBudgetAdjustment) { a.Verdict = "foreign" }, "unknown source/verdict"},
		{"null caps", func(a *CurriculumBudgetAdjustment) { a.TeamCaps = nil }, "explicit collections"},
		{"null effort", func(a *CurriculumBudgetAdjustment) { a.Effort = nil }, "explicit collections"},
		{"empty", func(a *CurriculumBudgetAdjustment) { a.TeamCaps = map[string]int{} }, "must have an effect"},
		{"team", func(a *CurriculumBudgetAdjustment) { a.TeamCaps = map[string]int{"foreign": 0} }, "invalid team cap"},
		{"negative", func(a *CurriculumBudgetAdjustment) { a.TeamCaps["OT Engineer"] = -1 }, "invalid team cap"},
		{"increase", func(a *CurriculumBudgetAdjustment) { a.TeamCaps["OT Engineer"] = 121 }, "invalid team cap"},
		{"actions", func(a *CurriculumBudgetAdjustment) {
			a.Effort = []CurriculumEffortAdjustment{{Actions: []string{"foreign"}, Multiplier: 2}}
		}, "unknown or duplicate"},
		{"duplicate action", func(a *CurriculumBudgetAdjustment) {
			a.Effort = []CurriculumEffortAdjustment{{Actions: []string{"modbus-dpi", "modbus-dpi"}, Multiplier: 2}}
		}, "unknown or duplicate"},
		{"zero multiplier", func(a *CurriculumBudgetAdjustment) {
			a.Effort = []CurriculumEffortAdjustment{{Actions: []string{"modbus-dpi"}, Multiplier: 0}}
		}, "finite multiplier"},
		{"infinity", func(a *CurriculumBudgetAdjustment) {
			a.Effort = []CurriculumEffortAdjustment{{Actions: []string{"modbus-dpi"}, Multiplier: math.Inf(1)}}
		}, "finite multiplier"},
		{"nan", func(a *CurriculumBudgetAdjustment) {
			a.Effort = []CurriculumEffortAdjustment{{Actions: []string{"modbus-dpi"}, Multiplier: math.NaN()}}
		}, "finite multiplier"},
		{"negative overhead", func(a *CurriculumBudgetAdjustment) {
			a.Effort = []CurriculumEffortAdjustment{{Actions: []string{"modbus-dpi"}, Multiplier: 1, ExtraHours: -1}}
		}, "finite multiplier"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, _ := curriculumExample(t)
			pkg.Curriculum.BudgetAdjustments = []CurriculumBudgetAdjustment{{
				Source: "ot-eng-availability", Verdict: "STRETCHED",
				TeamCaps: map[string]int{"OT Engineer": 60}, Effort: []CurriculumEffortAdjustment{},
			}}
			if err := validateCurriculum(pkg); err != nil {
				t.Fatal(err)
			}
			tc.edit(&pkg.Curriculum.BudgetAdjustments[0])
			if err := validateCurriculum(pkg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestCurriculumBudgetCollectionsAndDuplicates(t *testing.T) {
	pkg, _ := curriculumExample(t)
	pkg.Curriculum.BudgetAdjustments = nil
	if err := validateCurriculum(pkg); err == nil {
		t.Fatal("omitted budgetAdjustments accepted")
	}
	a := CurriculumBudgetAdjustment{
		Source: "ot-eng-availability", Verdict: "UNAVAILABLE",
		TeamCaps: map[string]int{"OT Engineer": 0}, Effort: []CurriculumEffortAdjustment{},
	}
	pkg.Curriculum.BudgetAdjustments = []CurriculumBudgetAdjustment{a}
	if err := validateCurriculum(pkg); err != nil {
		t.Fatalf("zero-cap adjustment: %v", err)
	}
	pkg.Curriculum.BudgetAdjustments = append(pkg.Curriculum.BudgetAdjustments, a)
	if err := validateCurriculum(pkg); err == nil || !strings.Contains(err.Error(), "duplicate source/verdict") {
		t.Fatalf("duplicate adjustment: %v", err)
	}
}
