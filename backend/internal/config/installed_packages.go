package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const installedPackagesSchema = "rangerdanger-installed-packages-v1"

var packageIDPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// ReadInstalledPackages rereads the host's atomic acquisition receipt. Directory
// presence and cached images are not proof of installation.
func ReadInstalledPackages(root string, available []string) ([]string, error) {
	file := filepath.Join(root, "data", "installed-packages")
	fail := func(reason string) ([]string, error) {
		return nil, fmt.Errorf("%s: %s; repair the record or remove it and run ./setup.sh --packages <ids> (Windows: .\\setup.ps1 -Packages <ids>)", file, reason)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return fail(err.Error())
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		return fail("expected LF-terminated record")
	}
	lines := strings.Split(string(data[:len(data)-1]), "\n")
	if len(lines) < 2 || lines[0] != installedPackagesSchema {
		return fail("invalid schema or empty package set")
	}
	ids := lines[1:]
	for i, id := range ids {
		if !packageIDPattern.MatchString(id) || !slices.Contains(available, id) || (i > 0 && ids[i-1] >= id) {
			return fail("package IDs must be known, sorted and unique")
		}
	}
	return ids, nil
}

// InstalledPackages returns the current receipt against the shipped catalog.
func (c *Config) InstalledPackages() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(c.LabDefinitionsPath, "packages", "*", "package.yml"))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(files))
	for _, file := range files {
		ids = append(ids, filepath.Base(filepath.Dir(file)))
	}
	return ReadInstalledPackages(c.Root, ids)
}
