package labs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

// resolveScenarios loads the package's single declared directory in filename order.
func resolveScenarios(root, packageDir, directory string) ([]ScenarioYAML, error) {
	if strings.TrimSpace(directory) == "" || filepath.IsAbs(directory) || strings.Contains(directory, `\`) ||
		(len(directory) >= 2 && directory[1] == ':') {
		return nil, fmt.Errorf("scenarios must be a nonempty relative portable directory path")
	}
	dir := filepath.Join(packageDir, directory)
	if _, err := scenarioPath(root, dir); err != nil {
		return nil, fmt.Errorf("scenarios directory %q: %w", directory, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scenarios %q is not a directory", directory)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	scenarios := []ScenarioYAML{}
	owners := map[string]string{}
	for _, entry := range files { // ReadDir sorts by filename.
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
		if owner, exists := owners[scenario.ID]; exists {
			return nil, fmt.Errorf("duplicate scenario id %q in %s and %s", scenario.ID, owner, file)
		}
		owners[scenario.ID] = file
		scenarios = append(scenarios, scenario)
	}
	return scenarios, nil
}
