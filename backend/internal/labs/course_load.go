package labs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

func loadCourse(dir string, course *Course, pkg Package) (*Course, error) {
	if course == nil {
		return nil, fmt.Errorf("course must not be omitted")
	}
	pkg.Course = course
	if err := validateCourse(pkg); err != nil {
		return nil, err
	}
	if !slugPattern.MatchString(strings.TrimSuffix(course.Nodes, ".yml")) || !strings.HasSuffix(course.Nodes, ".yml") {
		return nil, fmt.Errorf("course.nodes must name a sibling YAML file")
	}
	path := filepath.Join(dir, course.Nodes)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("course.nodes must be a regular file, not a symlink")
	}
	var document yaml.Node
	if err := decodeFile(path, &document); err != nil {
		return nil, err
	}
	if err := resolveCurriculumTopology(&document, pkg); err != nil {
		return nil, err
	}
	resolved, err := yaml.Marshal(&document)
	if err != nil {
		return nil, err
	}
	if err := decodeStrict(resolved, &course.CourseNodes); err != nil {
		return nil, err
	}
	if err := validateCourseNodes(pkg); err != nil {
		return nil, err
	}
	if f := course.Features.SegmentationPlan; f != nil {
		f.Model, err = loadCurriculum(dir, f, pkg)
		if err != nil {
			return nil, err
		}
	}
	return course, nil
}

func validateCourse(pkg Package) error {
	c := pkg.Course
	if c == nil || len(c.Modules) == 0 {
		return fmt.Errorf("course.modules must not be empty")
	}
	scenarios := map[string]bool{}
	for _, s := range pkg.Scenarios {
		scenarios[s.ID] = true
	}
	modules, labels, activities := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, m := range c.Modules {
		if !slugPattern.MatchString(m.ID) || modules[m.ID] {
			return fmt.Errorf("invalid or duplicate course module id %q", m.ID)
		}
		modules[m.ID] = true
		if strings.TrimSpace(m.Title) == "" || len(m.Activities) == 0 {
			return fmt.Errorf("course module %s needs title and activities", m.ID)
		}
		for _, a := range m.Activities {
			if !scenarios[a.Scenario] {
				return fmt.Errorf("course activity references unknown scenario %s", a.Scenario)
			}
			if activities[a.Scenario] {
				return fmt.Errorf("scenario %s occurs in two activities", a.Scenario)
			}
			activities[a.Scenario] = true
			if strings.TrimSpace(a.Label) == "" || labels[a.Label] {
				return fmt.Errorf("blank or duplicate course label %q", a.Label)
			}
			labels[a.Label] = true
			if c.Features.SegmentationPlan == nil && (a.PolicyActions != nil || a.HideBanner != nil) {
				return fmt.Errorf("planning activity flags require segmentationPlan")
			}
		}
	}
	for id := range scenarios {
		if !activities[id] {
			return fmt.Errorf("scenario %s has no course activity", id)
		}
	}
	if f := c.Features.SegmentationPlan; f != nil {
		seen := map[string]bool{}
		for _, id := range []string{f.Activities.Requirements, f.Activities.Planning, f.Activities.Implementation} {
			if !activities[id] || seen[id] {
				return fmt.Errorf("segmentationPlan bindings must be distinct course activities: %q", id)
			}
			seen[id] = true
		}
		if f.DynamicSteps == nil {
			return fmt.Errorf("segmentationPlan dynamicSteps must be explicit")
		}
	}
	return nil
}

func validateCourseNodes(pkg Package) error {
	c := pkg.Course
	if c.NodeMap == nil || c.NodeInference == nil {
		return fmt.Errorf("course nodeMap and nodeInference must be explicit")
	}
	nodes := map[string]bool{}
	for _, n := range pkg.Template.Nodes {
		nodes[n.ID] = true
	}
	for id, n := range c.NodeMap {
		if _, ok := c.Activity(id); !ok {
			return fmt.Errorf("nodeMap references unknown scenario %s", id)
		}
		if n.Others == nil {
			return fmt.Errorf("nodeMap.%s.others must be an explicit list", id)
		}
		for _, id := range append([]string{n.Primary}, n.Others...) {
			if id != "" && !nodes[id] {
				return fmt.Errorf("unknown exercise node %s", id)
			}
		}
	}
	for _, n := range c.NodeInference {
		if !nodes[n.Node] {
			return fmt.Errorf("unknown inference node %s", n.Node)
		}
		if _, err := regexp.Compile(n.Pattern); err != nil {
			return fmt.Errorf("invalid inference pattern: %w", err)
		}
	}
	return nil
}

func validateFeatureUsage(pkg Package, validators ValidatorRequirements) error {
	if pkg.Course.Features.SegmentationPlan != nil {
		return nil
	}
	for _, scenario := range pkg.Scenarios {
		if strings.Contains(scenario.Description, ":::plan-coverage") {
			return fmt.Errorf("plan-coverage requires segmentationPlan")
		}
		if validators[scenario.Validator].SegmentationPlan {
			return fmt.Errorf("validator %s requires segmentationPlan", scenario.Validator)
		}
		for _, step := range scenario.Steps {
			if strings.Contains(step.Description, ":::plan-coverage") {
				return fmt.Errorf("plan-coverage requires segmentationPlan")
			}
			if step.Action != nil && step.Action.Type == "check" && step.Action.Expect["firewall_config"] == "improved" {
				return fmt.Errorf("improved check requires segmentationPlan")
			}
		}
	}
	return nil
}
