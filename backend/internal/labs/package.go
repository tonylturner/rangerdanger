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
	CapabilityProtocolIEC104     = "protocol.iec104"
	CapabilityProcessTruth       = "process.truth"
)

// Operator views a package declares in package.yml `operator_view`. The
// frontend picks the operator page by this value.
const (
	OperatorViewUSDNP3   = "us-dnp3-substation"
	OperatorViewEUIEC104 = "eu-iec104"
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
	CapabilityProtocolIEC104:     true,
	CapabilityProcessTruth:       true,
}

var knownOperatorViews = map[string]bool{
	OperatorViewUSDNP3:   true,
	OperatorViewEUIEC104: true,
}

// slugPattern is the shape of package IDs, scenario IDs and step IDs.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidatorRequirements maps each registered validator key to the package
// capabilities it needs. The server owns the validator code; the loader
// only checks that declared keys exist and that the package can serve them.
type ValidatorRequirements map[string][]string

// PackageYAML mirrors lab-definitions/packages/<id>/package.yml.
type PackageYAML struct {
	Curriculum   *CurriculumBinding `yaml:"curriculum"`
	Schema       int                `yaml:"schema"`
	ID           string             `yaml:"id"`
	Title        string             `yaml:"title"`
	Revision     int                `yaml:"revision"`
	Topology     string             `yaml:"topology"`  // relative to package.yml
	Scenarios    string             `yaml:"scenarios"` // directory, relative to package.yml
	Knowledge    string             `yaml:"knowledge"` // required knowledge index, relative to package.yml
	Capabilities []string           `yaml:"capabilities"`
	// Policies maps the names the firewall buttons and actions apply
	// ("weak", "improved") to policy files relative to package.yml.
	Policies     map[string]string `yaml:"policies"`
	OperatorView string            `yaml:"operator_view"`
	// ICSRules is ICSRulesAllowed or ICSRulesForbidden; empty means allowed.
	ICSRules string `yaml:"ics_rules"`
}

// Package is one fully parsed and validated curriculum package.
type Package struct {
	Curriculum   *Curriculum
	ID           string
	Title        string
	Revision     int
	Capabilities []string
	Template     LabYAML
	// FirewallConfigPath is the topology's firewall_config resolved from the
	// topology file and re-expressed relative to the definitions directory,
	// which is how the orchestrator joins it.
	FirewallConfigPath string
	// Policies maps each named policy to its file relative to the
	// definitions directory. FirewallConfigPath is one of the values.
	Policies     map[string]string
	OperatorView string
	// ICSRules is ICSRulesAllowed or ICSRulesForbidden, never empty.
	ICSRules  string
	Scenarios []ScenarioYAML
	Knowledge []KnowledgeSection
}

// PolicyName returns the name of the policy whose file is exactly path
// (definitions-relative).
func (p Package) PolicyName(path string) (string, bool) {
	for name, file := range p.Policies {
		if file == path {
			return name, true
		}
	}
	return "", false
}

// PackageInfo is the public summary of a package.
type PackageInfo struct {
	ID           string
	Title        string
	Revision     int
	Capabilities []string
	OperatorView string
}

// Info summarizes the package.
func (p Package) Info() PackageInfo {
	return PackageInfo{
		ID:           p.ID,
		Title:        p.Title,
		Revision:     p.Revision,
		Capabilities: append([]string(nil), p.Capabilities...),
		OperatorView: p.OperatorView,
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

// loadPackage parses one package.yml plus the topology, policies and
// scenarios it references, and validates all of it. It performs no
// database writes.
func (l *Loader) loadPackage(manifestPath string) (Package, error) {
	pkg, scenarios, err := readDefinition(l.DefinitionsDir, manifestPath)
	if err != nil {
		return Package{}, err
	}
	scenarioDir := filepath.Join(filepath.Dir(manifestPath), scenarios)
	info, err := os.Stat(scenarioDir)
	if err != nil {
		return Package{}, fmt.Errorf("scenarios directory: %w", err)
	}
	if !info.IsDir() {
		return Package{}, fmt.Errorf("scenarios %s is not a directory", scenarios)
	}
	if pkg.Scenarios, err = loadScenarioFiles(scenarioDir); err != nil {
		return Package{}, err
	}
	if err := ValidateLab(&pkg.Template, pkg.Scenarios, pkg.Capabilities, pkg.Policies, l.Validators); err != nil {
		return Package{}, err
	}
	if err := validateCurriculum(pkg); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

// ReadDefinition parses and validates the parts of a package that need no
// validator registry: package.yml, its topology and its policies. The
// package lint uses it; the loader adds the scenarios.
func ReadDefinition(definitionsDir, manifestPath string) (Package, error) {
	pkg, _, err := readDefinition(definitionsDir, manifestPath)
	return pkg, err
}

// readDefinition is ReadDefinition plus the package.yml scenarios value.
func readDefinition(definitionsDir, manifestPath string) (Package, string, error) {
	var manifest PackageYAML
	if err := decodeFile(manifestPath, &manifest); err != nil {
		return Package{}, "", err
	}
	packageDir := filepath.Dir(manifestPath)
	if err := validateManifest(manifest, filepath.Base(packageDir)); err != nil {
		return Package{}, "", err
	}
	icsRules := manifest.ICSRules
	if icsRules == "" {
		icsRules = ICSRulesAllowed
	}

	topologyPath := filepath.Join(packageDir, manifest.Topology)
	if _, err := definitionsRelative(definitionsDir, topologyPath); err != nil {
		return Package{}, "", fmt.Errorf("topology %q %w", manifest.Topology, err)
	}
	var def LabYAML
	if err := decodeFile(topologyPath, &def); err != nil {
		return Package{}, "", fmt.Errorf("topology: %w", err)
	}
	if def.ID == "" {
		return Package{}, "", fmt.Errorf("topology %s: missing id", filepath.Base(topologyPath))
	}
	if len(def.Scenarios) > 0 {
		return Package{}, "", fmt.Errorf("topology %s: inline scenarios are not supported; put each scenario in the package's scenarios directory", filepath.Base(topologyPath))
	}

	if err := ValidateTopologyFlows(&def); err != nil {
		return Package{}, "", fmt.Errorf("topology %s: %w", filepath.Base(topologyPath), err)
	}

	firewallConfigPath, err := resolveFirewallConfig(definitionsDir, filepath.Dir(topologyPath), def.FirewallConfig)
	if err != nil {
		return Package{}, "", fmt.Errorf("topology %s: %w", filepath.Base(topologyPath), err)
	}
	policies, err := resolvePolicies(definitionsDir, packageDir, manifest.Policies, icsRules)
	if err != nil {
		return Package{}, "", err
	}
	pkg := Package{
		ID:                 manifest.ID,
		Title:              manifest.Title,
		Revision:           manifest.Revision,
		Capabilities:       manifest.Capabilities,
		Template:           def,
		FirewallConfigPath: firewallConfigPath,
		Policies:           policies,
		OperatorView:       manifest.OperatorView,
		ICSRules:           icsRules,
	}
	if _, ok := pkg.PolicyName(firewallConfigPath); !ok {
		return Package{}, "", fmt.Errorf("topology %s: firewall_config %s is not one of the package policies", filepath.Base(topologyPath), firewallConfigPath)
	}
	if pkg.Knowledge, err = loadKnowledge(packageDir, manifest.Knowledge, manifest.Scenarios); err != nil {
		return Package{}, "", fmt.Errorf("knowledge: %w", err)
	}
	if pkg.Curriculum, err = loadCurriculum(packageDir, manifest.Curriculum, pkg); err != nil {
		return Package{}, "", fmt.Errorf("curriculum: %w", err)
	}
	return pkg, manifest.Scenarios, nil
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
	if !knownOperatorViews[manifest.OperatorView] {
		problems = append(problems, fmt.Sprintf("operator_view must be %s or %s, got %q", OperatorViewUSDNP3, OperatorViewEUIEC104, manifest.OperatorView))
	}
	if manifest.ICSRules != "" && manifest.ICSRules != ICSRulesAllowed && manifest.ICSRules != ICSRulesForbidden {
		problems = append(problems, fmt.Sprintf("ics_rules must be %s or %s, got %q", ICSRulesAllowed, ICSRulesForbidden, manifest.ICSRules))
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
// directory and returns it relative to the definitions directory. Every
// package names its default policy there.
func resolveFirewallConfig(definitionsDir, topologyDir, value string) (string, error) {
	if value == "" {
		return "", errors.New("firewall_config must name the package's default policy")
	}
	resolved := filepath.Join(topologyDir, value)
	if _, err := os.Stat(resolved); err != nil {
		return "", fmt.Errorf("firewall_config: %w", err)
	}
	relative, err := definitionsRelative(definitionsDir, resolved)
	if err != nil {
		return "", fmt.Errorf("firewall_config %q %w", value, err)
	}
	return relative, nil
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
