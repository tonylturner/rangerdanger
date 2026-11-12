package labs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ScenarioSource selects authored IDs from a directory relative to package.yml.
// A nil Include selects every .yml file; an explicitly empty list is invalid.
type ScenarioSource struct {
	Dir     string   `yaml:"dir"`
	Include []string `yaml:"include"`
}

// UnmarshalYAML keeps optional include distinct from an explicit null and
// preserves strict field checking at this custom decoding boundary.
func (s *ScenarioSource) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("scenario source must be a mapping with dir and optional include")
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		switch key {
		case "dir":
		case "include":
			if value.Kind != yaml.SequenceNode {
				return fmt.Errorf("scenario include must be a nonempty list of ids")
			}
		default:
			return fmt.Errorf("field %s not found in scenario source", key)
		}
	}
	type plain ScenarioSource
	return node.Decode((*plain)(s))
}

// scenarioPath confines both lexical paths and symlink targets to definitions.
func scenarioPath(root, path string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	inside := func(root, path string) bool {
		rel, err := filepath.Rel(root, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if !inside(root, path) {
		return "", fmt.Errorf("scenario path %q escapes definitions root", path)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !inside(realRoot, realPath) {
		return "", fmt.Errorf("scenario path %q escapes definitions root", path)
	}
	return realPath, nil
}

func resolveScenarioSources(root, packageDir string, sources []ScenarioSource) ([]ScenarioYAML, error) {
	scenarios := []ScenarioYAML{}
	owners := map[string]string{}
	for index, source := range sources {
		if strings.TrimSpace(source.Dir) == "" || filepath.IsAbs(source.Dir) || strings.Contains(source.Dir, `\`) ||
			(len(source.Dir) >= 2 && source.Dir[1] == ':') {
			return nil, fmt.Errorf("scenario source %d: dir must be a nonempty relative portable path", index)
		}
		dir := filepath.Join(packageDir, source.Dir)
		_, err := scenarioPath(root, dir)
		if err != nil {
			return nil, fmt.Errorf("scenarios directory %q: %w", source.Dir, err)
		}
		info, err := os.Stat(dir)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("scenarios %q is not a directory", source.Dir)
		}
		wanted := map[string]bool{}
		if source.Include != nil {
			if len(source.Include) == 0 {
				return nil, fmt.Errorf("scenario source %q: include must not be empty", source.Dir)
			}
			for _, id := range source.Include {
				if !slugPattern.MatchString(id) || wanted[id] {
					return nil, fmt.Errorf("scenario source %q: invalid or duplicate include id %q", source.Dir, id)
				}
				wanted[id] = true
			}
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		matches := map[string]int{}
		for _, entry := range files { // ReadDir sorts by filename within each source.
			if !strings.HasSuffix(entry.Name(), ".yml") {
				continue
			}
			file := filepath.Join(dir, entry.Name())
			realFile, err := scenarioPath(root, file)
			if err != nil {
				return nil, err
			}
			info, err := os.Stat(realFile)
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("scenario %s is not a regular file", file)
			}
			var scenario ScenarioYAML
			if err := decodeFile(realFile, &scenario); err != nil {
				return nil, fmt.Errorf("scenario %s: %w", file, err)
			}
			if source.Include != nil && !wanted[scenario.ID] {
				continue
			}
			matches[scenario.ID]++
			if owner, exists := owners[scenario.ID]; exists {
				return nil, fmt.Errorf("duplicate scenario id %q in %s and %s", scenario.ID, owner, file)
			}
			owners[scenario.ID] = file
			scenarios = append(scenarios, scenario)
		}
		for _, id := range source.Include {
			if matches[id] != 1 {
				return nil, fmt.Errorf("scenario source %q: include id %q must resolve exactly once (got %d)", source.Dir, id, matches[id])
			}
		}
	}
	return scenarios, nil
}
