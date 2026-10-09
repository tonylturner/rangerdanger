package labs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PackageSchemaVersion is the only package.yml schema this loader reads.
const PackageSchemaVersion = 1

// Capabilities a package can declare. Validators and the load-time action
// vocabulary are gated on them.
const (
	CapabilityProcessElectrical  = "process.electrical"
	CapabilityPolicyContaind     = "policy.containd"
	CapabilityAuditDeviceControl = "audit.device-control"
	CapabilityCaptureFirewall    = "capture.firewall"
)

const (
	packagesDirName     = "packages"
	packageManifestName = "package.yml"
	scenarioFilePattern = "*.yml"
	// slugRule describes slugPattern in error messages.
	slugRule = "lowercase letters and digits separated by single hyphens"
)

var knownCapabilities = map[string]bool{
	CapabilityProcessElectrical:  true,
	CapabilityPolicyContaind:     true,
	CapabilityAuditDeviceControl: true,
	CapabilityCaptureFirewall:    true,
}

// slugPattern is the shape of package IDs, scenario IDs and step IDs.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidatorRequirements maps each registered validator key to the package
// capabilities it needs. The server owns the validator code; the loader
// only checks that declared keys exist and that the package can serve them.
type ValidatorRequirements map[string][]string

// PackageYAML mirrors lab-definitions/packages/<id>/package.yml.
type PackageYAML struct {
	Schema       int      `yaml:"schema"`
	ID           string   `yaml:"id"`
	Title        string   `yaml:"title"`
	Revision     int      `yaml:"revision"`
	Topology     string   `yaml:"topology"`  // relative to package.yml
	Scenarios    string   `yaml:"scenarios"` // directory, relative to package.yml
	Capabilities []string `yaml:"capabilities"`
}

// Package is one fully parsed and validated curriculum package.
type Package struct {
	ID           string
	Title        string
	Revision     int
	Capabilities []string
	Template     LabYAML
	// FirewallConfigPath is the topology's firewall_config resolved from the
	// topology file and re-expressed relative to the definitions directory,
	// which is how the orchestrator joins it.
	FirewallConfigPath string
	Scenarios          []ScenarioYAML
}

// PackageInfo is the public summary of a package.
type PackageInfo struct {
	ID           string
	Title        string
	Revision     int
	TemplateID   string
	Capabilities []string
}

// Info summarizes the package.
func (p Package) Info() PackageInfo {
	return PackageInfo{
		ID:           p.ID,
		Title:        p.Title,
		Revision:     p.Revision,
		TemplateID:   p.Template.ID,
		Capabilities: append([]string(nil), p.Capabilities...),
	}
}

// Catalog is the set of packages found on disk, sorted by ID.
type Catalog struct {
	Packages []Package
}

// Info returns the summary of the package with the given ID.
func (c *Catalog) Info(id string) (PackageInfo, bool) {
	if c == nil {
		return PackageInfo{}, false
	}
	for _, pkg := range c.Packages {
		if pkg.ID == id {
			return pkg.Info(), true
		}
	}
	return PackageInfo{}, false
}

// Infos returns every package summary in ID order.
func (c *Catalog) Infos() []PackageInfo {
	if c == nil {
		return nil
	}
	infos := make([]PackageInfo, len(c.Packages))
	for index, pkg := range c.Packages {
		infos[index] = pkg.Info()
	}
	return infos
}

// decodeStrict decodes exactly one YAML document and rejects unknown fields.
func decodeStrict(data []byte, out any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("document is empty")
		}
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("file must contain exactly one YAML document")
	}
	return nil
}

func decodeFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := decodeStrict(data, out); err != nil {
		return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return nil
}

// loadPackage parses one package.yml plus the topology and scenarios it
// references, and validates all of it. It performs no database writes.
func (l *Loader) loadPackage(manifestPath string) (Package, error) {
	var manifest PackageYAML
	if err := decodeFile(manifestPath, &manifest); err != nil {
		return Package{}, err
	}
	packageDir := filepath.Dir(manifestPath)
	if err := validateManifest(manifest, filepath.Base(packageDir)); err != nil {
		return Package{}, err
	}

	topologyPath := filepath.Join(packageDir, manifest.Topology)
	var def LabYAML
	if err := decodeFile(topologyPath, &def); err != nil {
		return Package{}, fmt.Errorf("topology: %w", err)
	}
	if def.ID == "" {
		return Package{}, fmt.Errorf("topology %s: missing id", filepath.Base(topologyPath))
	}
	if len(def.Scenarios) > 0 {
		return Package{}, fmt.Errorf("topology %s: inline scenarios are not supported; put each scenario in the package's scenarios directory", filepath.Base(topologyPath))
	}

	firewallConfigPath, err := l.resolveFirewallConfig(filepath.Dir(topologyPath), def.FirewallConfig)
	if err != nil {
		return Package{}, fmt.Errorf("topology %s: %w", filepath.Base(topologyPath), err)
	}

	scenarioDir := filepath.Join(packageDir, manifest.Scenarios)
	info, err := os.Stat(scenarioDir)
	if err != nil {
		return Package{}, fmt.Errorf("scenarios directory: %w", err)
	}
	if !info.IsDir() {
		return Package{}, fmt.Errorf("scenarios %s is not a directory", manifest.Scenarios)
	}
	scenarios, err := loadScenarioFiles(scenarioDir)
	if err != nil {
		return Package{}, err
	}
	if err := ValidateLab(&def, scenarios, manifest.Capabilities, l.Validators); err != nil {
		return Package{}, err
	}

	return Package{
		ID:                 manifest.ID,
		Title:              manifest.Title,
		Revision:           manifest.Revision,
		Capabilities:       manifest.Capabilities,
		Template:           def,
		FirewallConfigPath: firewallConfigPath,
		Scenarios:          scenarios,
	}, nil
}

func validateManifest(manifest PackageYAML, dirName string) error {
	var problems []string
	if manifest.Schema != PackageSchemaVersion {
		problems = append(problems, fmt.Sprintf("schema must be %d, got %d", PackageSchemaVersion, manifest.Schema))
	}
	if !slugPattern.MatchString(manifest.ID) {
		problems = append(problems, fmt.Sprintf("id %q must be %s", manifest.ID, slugRule))
	} else if manifest.ID != dirName {
		problems = append(problems, fmt.Sprintf("id %q must match its directory name %q", manifest.ID, dirName))
	}
	if strings.TrimSpace(manifest.Title) == "" {
		problems = append(problems, "title must not be empty")
	}
	if manifest.Revision < 1 {
		problems = append(problems, "revision must be at least 1")
	}
	if manifest.Topology == "" {
		problems = append(problems, "topology must not be empty")
	}
	if manifest.Scenarios == "" {
		problems = append(problems, "scenarios must not be empty")
	}
	seen := make(map[string]bool, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		if !knownCapabilities[capability] {
			problems = append(problems, fmt.Sprintf("unknown capability %q", capability))
		}
		if seen[capability] {
			problems = append(problems, fmt.Sprintf("duplicate capability %q", capability))
		}
		seen[capability] = true
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid %s:\n%s", packageManifestName, strings.Join(problems, "\n"))
}

// resolveFirewallConfig resolves firewall_config from the topology's
// directory and returns it relative to the definitions directory.
func (l *Loader) resolveFirewallConfig(topologyDir, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	resolved := filepath.Join(topologyDir, value)
	if _, err := os.Stat(resolved); err != nil {
		return "", fmt.Errorf("firewall_config: %w", err)
	}
	relative, err := filepath.Rel(l.DefinitionsDir, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("firewall_config %q must stay inside the definitions directory", value)
	}
	return filepath.ToSlash(relative), nil
}

func loadScenarioFiles(dir string) ([]ScenarioYAML, error) {
	files, err := filepath.Glob(filepath.Join(dir, scenarioFilePattern))
	if err != nil {
		return nil, fmt.Errorf("find scenario YAML files: %w", err)
	}
	sort.Strings(files)

	scenarios := make([]ScenarioYAML, 0, len(files))
	for _, file := range files {
		var scenario ScenarioYAML
		if err := decodeFile(file, &scenario); err != nil {
			return nil, fmt.Errorf("scenario: %w", err)
		}
		scenarios = append(scenarios, scenario)
	}
	return scenarios, nil
}
