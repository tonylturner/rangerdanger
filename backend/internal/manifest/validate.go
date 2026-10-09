package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"gopkg.in/yaml.v3"
)

// Validate checks the manifest's internal references and required package
// files. Compose agreement is checked separately against Compose's normalized
// output.
func Validate(m *Manifest, root string) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	var problems []string
	if m.Schema != Schema {
		problems = append(problems, fmt.Sprintf("schema must be %d, got %d", Schema, m.Schema))
	}
	if m.Package == "" {
		problems = append(problems, "package id is empty")
	}
	networks := make(map[string]Network, len(m.Networks))
	networkNames := make(map[string]string, len(m.Networks))
	platformCount := 0
	for _, network := range m.Networks {
		if network.Key == "" {
			problems = append(problems, "network key is empty")
		} else if _, exists := networks[network.Key]; exists {
			problems = append(problems, fmt.Sprintf("duplicate network key %q", network.Key))
		} else {
			networks[network.Key] = network
		}
		if network.Name == "" {
			problems = append(problems, fmt.Sprintf("network %q has an empty name", network.Key))
		} else if previous, exists := networkNames[network.Name]; exists {
			problems = append(problems, fmt.Sprintf("networks %q and %q share name %q", previous, network.Key, network.Name))
		} else {
			networkNames[network.Name] = network.Key
		}
		if network.Platform {
			platformCount++
			if network.Name != "rangerdanger_mgmt_net" {
				problems = append(problems, fmt.Sprintf("platform network %q must be named rangerdanger_mgmt_net", network.Key))
			}
		}
		if _, _, err := parseIPv4Subnet(network.Subnet); err != nil {
			problems = append(problems, fmt.Sprintf("network %q has invalid IPv4 subnet %q", network.Key, network.Subnet))
		}
		gateway := net.ParseIP(network.Gateway)
		if gateway == nil || gateway.To4() == nil {
			problems = append(problems, fmt.Sprintf("network %q has invalid IPv4 gateway %q", network.Key, network.Gateway))
		} else if _, subnet, err := parseIPv4Subnet(network.Subnet); err == nil && !subnet.Contains(gateway) {
			problems = append(problems, fmt.Sprintf("network %q gateway %q is outside subnet %q", network.Key, network.Gateway, network.Subnet))
		}
	}
	if platformCount != 1 {
		problems = append(problems, fmt.Sprintf("manifest must declare exactly one platform network, got %d", platformCount))
	}

	services := make(map[string]Service, len(m.Services))
	containers := make(map[string]string, len(m.Services))
	nodes := make(map[string]string, len(m.Services))
	ips := make(map[string]string)
	mgmtIPs := make(map[string]string, len(m.Services))
	for _, service := range m.Services {
		if service.Key == "" {
			problems = append(problems, "service key is empty")
		} else if _, exists := services[service.Key]; exists {
			problems = append(problems, fmt.Sprintf("duplicate service key %q", service.Key))
		} else {
			services[service.Key] = service
		}
		if service.Container == "" {
			problems = append(problems, fmt.Sprintf("service %q has an empty container name", service.Key))
		} else if previous, exists := containers[service.Container]; exists {
			problems = append(problems, fmt.Sprintf("services %q and %q share container %q", previous, service.Key, service.Container))
		} else {
			containers[service.Container] = service.Key
		}
		if service.Node != "" {
			if previous, exists := nodes[service.Node]; exists {
				problems = append(problems, fmt.Sprintf("services %q and %q share node %q", previous, service.Key, service.Node))
			} else {
				nodes[service.Node] = service.Key
			}
		}
		seenRoles := map[Role]bool{}
		for _, role := range service.Roles {
			if !Roles[role] {
				problems = append(problems, fmt.Sprintf("service %q has unknown role %q", service.Key, role))
			}
			if seenRoles[role] {
				problems = append(problems, fmt.Sprintf("service %q repeats role %q", service.Key, role))
			}
			seenRoles[role] = true
		}
		seenInterfaces := map[string]bool{}
		for _, iface := range service.Interfaces {
			network, exists := networks[iface.Network]
			if !exists {
				problems = append(problems, fmt.Sprintf("service %q interface references undeclared network %q", service.Key, iface.Network))
				continue
			}
			if seenInterfaces[iface.Network] {
				problems = append(problems, fmt.Sprintf("service %q repeats interface on network %q", service.Key, iface.Network))
			}
			seenInterfaces[iface.Network] = true
			ip := net.ParseIP(iface.IPv4)
			if ip == nil || ip.To4() == nil {
				problems = append(problems, fmt.Sprintf("service %q has invalid IPv4 address %q on %q", service.Key, iface.IPv4, iface.Network))
				continue
			}
			_, subnet, err := parseIPv4Subnet(network.Subnet)
			if err == nil && !subnet.Contains(ip) {
				problems = append(problems, fmt.Sprintf("service %q IP %q is outside network %q subnet %q", service.Key, iface.IPv4, iface.Network, network.Subnet))
			}
			if iface.IPv4 == network.Gateway {
				problems = append(problems, fmt.Sprintf("service %q IP %q uses network %q gateway", service.Key, iface.IPv4, iface.Network))
			}
			if previous, exists := ips[iface.IPv4]; exists {
				problems = append(problems, fmt.Sprintf("IP %q is shared by %s and %s", iface.IPv4, previous, service.Key))
			} else {
				ips[iface.IPv4] = service.Key
			}
			if network.Platform {
				mgmtIPs[service.Key] = iface.IPv4
			}
		}
	}

	firewallAPI := false
	for _, service := range m.Services {
		for _, role := range service.Roles {
			if role == RoleFirewall && service.Endpoints["api"] != "" {
				firewallAPI = true
			}
		}
		for name, endpoint := range service.Endpoints {
			parsed, err := url.Parse(endpoint)
			if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
				problems = append(problems, fmt.Sprintf("service %q endpoint %q is not an absolute URL", service.Key, name))
				continue
			}
			hostIP := net.ParseIP(parsed.Hostname())
			if hostIP == nil || hostIP.To4() == nil || mgmtIPs[service.Key] == "" || !hostIP.Equal(net.ParseIP(mgmtIPs[service.Key])) {
				problems = append(problems, fmt.Sprintf("service %q endpoint %q host %q must be its management IP %q", service.Key, name, parsed.Hostname(), mgmtIPs[service.Key]))
			}
		}
	}
	if !firewallAPI {
		problems = append(problems, "a firewall service with an api endpoint is required")
	}

	if root == "" {
		problems = append(problems, "installation root is empty")
	} else if m.Package != "" {
		dir := Dir(root, m.Package)
		for _, name := range []string{"package.yml", FileName, ComposeFile(ModeSource), ComposeFile(ModeRelease), RoutesFile} {
			info, err := os.Stat(filepath.Join(dir, name))
			if err != nil {
				problems = append(problems, fmt.Sprintf("required package file %s is unavailable: %v", name, err))
			} else if !info.Mode().IsRegular() {
				problems = append(problems, fmt.Sprintf("required package file %s is not a regular file", name))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// CheckPlatform verifies that the platform Compose model owns the declared
// management network and does not assign an address used by the range.
func CheckPlatform(m *Manifest, platformNormalized []byte) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	model, err := decodeCompose(platformNormalized)
	if err != nil {
		return fmt.Errorf("decode normalized platform Compose: %w", err)
	}
	if model.ProjectName != PlatformProject {
		return fmt.Errorf("platform Compose project is %q, want %q", model.ProjectName, PlatformProject)
	}
	var platform *Network
	for index := range m.Networks {
		if m.Networks[index].Platform {
			if platform != nil {
				return fmt.Errorf("manifest declares multiple platform networks")
			}
			platform = &m.Networks[index]
		}
	}
	if platform == nil {
		return fmt.Errorf("manifest has no platform network")
	}
	composeNetwork, exists := model.Networks[platform.Key]
	if !exists {
		return fmt.Errorf("platform Compose is missing network %q", platform.Key)
	}
	if composeNetwork.Name != platform.Name {
		return fmt.Errorf("platform network %q name %q does not match manifest %q", platform.Key, composeNetwork.Name, platform.Name)
	}
	if composeNetwork.External {
		return fmt.Errorf("platform network %q must be owned by the platform project", platform.Key)
	}
	subnet, gateway, hasIPAM := networkIPAM(composeNetwork)
	if !hasIPAM || subnet != platform.Subnet || gateway != platform.Gateway {
		return fmt.Errorf("platform network %q IPAM is %q/%q, want %q/%q", platform.Key, subnet, gateway, platform.Subnet, platform.Gateway)
	}
	rangeIPs := map[string]string{}
	for _, service := range m.Services {
		for _, iface := range service.Interfaces {
			if iface.Network == platform.Key {
				rangeIPs[iface.IPv4] = service.Key
			}
		}
	}
	for serviceKey, service := range model.Services {
		for networkKey, attachment := range service.Networks {
			if networkKey != platform.Key || attachment.IPv4 == "" {
				continue
			}
			if owner, collision := rangeIPs[attachment.IPv4]; collision {
				return fmt.Errorf("platform service %q IP %q collides with range service %q", serviceKey, attachment.IPv4, owner)
			}
		}
	}
	return nil
}

// CheckTopology verifies manifest node mappings and zone references against a
// loaded curriculum package.
func CheckTopology(m *Manifest, pkg labs.Package) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	if m.Package != pkg.ID {
		return fmt.Errorf("manifest package %q does not match curriculum package %q", m.Package, pkg.ID)
	}
	if pkg.Template.ID == "" {
		return fmt.Errorf("curriculum package %q has no topology id", pkg.ID)
	}
	topologyNodes := make(map[string]labs.NodeYAML, len(pkg.Template.Nodes))
	topologyContainers := make(map[string]labs.NodeYAML, len(pkg.Template.Nodes))
	for _, node := range pkg.Template.Nodes {
		if _, exists := topologyNodes[node.ID]; exists {
			return fmt.Errorf("topology has duplicate node id %q", node.ID)
		}
		topologyNodes[node.ID] = node
		if node.Container != "" {
			if previous, exists := topologyContainers[node.Container]; exists {
				return fmt.Errorf("topology nodes %q and %q share container %q", previous.ID, node.ID, node.Container)
			}
			topologyContainers[node.Container] = node
		}
	}
	manifestContainers := make(map[string]Service, len(m.Services))
	for _, service := range m.Services {
		manifestContainers[service.Container] = service
		if service.Node == "" {
			continue
		}
		node, exists := topologyNodes[service.Node]
		if !exists {
			return fmt.Errorf("service %q references topology node %q which does not exist", service.Key, service.Node)
		}
		if node.Container != "" && node.Container != service.Container {
			return fmt.Errorf("service %q container %q does not match topology node %q container %q", service.Key, service.Container, node.ID, node.Container)
		}
	}
	for container, node := range topologyContainers {
		service, exists := manifestContainers[container]
		if !exists {
			return fmt.Errorf("topology node %q container %q has no manifest service", node.ID, container)
		}
		if service.Node != node.ID {
			return fmt.Errorf("topology node %q container %q maps to service %q with node %q", node.ID, container, service.Key, service.Node)
		}
	}
	zones := map[string]bool{}
	for _, network := range pkg.Template.Networks {
		if network.Zone == "" {
			continue
		}
		if network.ID == "" {
			return fmt.Errorf("topology zone %q has no network id", network.Zone)
		}
		if zones[network.ID] {
			return fmt.Errorf("topology has duplicate zone network id %q", network.ID)
		}
		zones[network.ID] = true
	}
	manifestZones := map[string]bool{}
	for _, network := range m.Networks {
		if network.Zone == "" {
			continue
		}
		if !zones[network.Zone] {
			return fmt.Errorf("manifest network %q references unknown topology zone %q", network.Key, network.Zone)
		}
		manifestZones[network.Zone] = true
	}
	var missing []string
	for zone := range zones {
		if !manifestZones[zone] {
			missing = append(missing, zone)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("topology zones %s are missing from the manifest", strings.Join(missing, ", "))
	}
	return nil
}

// CheckCompose verifies the range model emitted by `docker compose config
// --format json` against the manifest.
func CheckCompose(m *Manifest, mode Mode, root string, normalized []byte) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	if mode != ModeSource && mode != ModeRelease {
		return fmt.Errorf("unsupported Compose mode %q", mode)
	}
	if err := CheckRawCompose(filepath.Join(Dir(root, m.Package), ComposeFile(mode))); err != nil {
		return err
	}
	model, err := decodeCompose(normalized)
	if err != nil {
		return fmt.Errorf("decode normalized Compose: %w", err)
	}
	if model.ProjectName != RangeProject {
		return fmt.Errorf("range Compose project is %q, want %q", model.ProjectName, RangeProject)
	}
	wantServices := make(map[string]Service, len(m.Services))
	for _, service := range m.Services {
		wantServices[service.Key] = service
	}
	if len(model.Services) != len(wantServices) {
		return fmt.Errorf("Compose service set has %d services, manifest has %d", len(model.Services), len(wantServices))
	}
	for key, service := range wantServices {
		actual, exists := model.Services[key]
		if !exists {
			return fmt.Errorf("Compose is missing manifest service %q", key)
		}
		if actual.ContainerName != service.Container {
			return fmt.Errorf("service %q container_name %q does not match manifest %q", key, actual.ContainerName, service.Container)
		}
		if len(actual.Networks) != len(service.Interfaces) {
			return fmt.Errorf("service %q has %d Compose network attachments, manifest has %d", key, len(actual.Networks), len(service.Interfaces))
		}
		for _, iface := range service.Interfaces {
			attachment, exists := actual.Networks[iface.Network]
			if !exists {
				return fmt.Errorf("service %q is missing network %q", key, iface.Network)
			}
			if attachment.IPv4 != iface.IPv4 {
				return fmt.Errorf("service %q network %q IPv4 %q does not match manifest %q", key, iface.Network, attachment.IPv4, iface.IPv4)
			}
		}
	}

	wantNetworks := make(map[string]Network, len(m.Networks))
	for _, network := range m.Networks {
		wantNetworks[network.Key] = network
	}
	if len(model.Networks) != len(wantNetworks) {
		return fmt.Errorf("Compose network set has %d networks, manifest has %d", len(model.Networks), len(wantNetworks))
	}
	for key, network := range wantNetworks {
		actual, exists := model.Networks[key]
		if !exists {
			return fmt.Errorf("Compose is missing manifest network %q", key)
		}
		if actual.Name != network.Name {
			return fmt.Errorf("network %q name %q does not match manifest %q", key, actual.Name, network.Name)
		}
		if network.Platform {
			if !actual.External {
				return fmt.Errorf("platform network %q must be external in a range Compose file", key)
			}
			if actual.IPAM != nil && len(actual.IPAM.Config) != 0 {
				return fmt.Errorf("external platform network %q must not declare IPAM in a range Compose file", key)
			}
		} else {
			if actual.External {
				return fmt.Errorf("range network %q must not be external", key)
			}
			subnet, gateway, hasIPAM := networkIPAM(actual)
			if !hasIPAM || subnet != network.Subnet || gateway != network.Gateway {
				return fmt.Errorf("network %q IPAM is %q/%q, want %q/%q", key, subnet, gateway, network.Subnet, network.Gateway)
			}
		}
	}
	return checkComposeSafety(model, root)
}

// CheckRawCompose rejects Compose features the package format does not allow.
func CheckRawCompose(path string) error {
	return checkRawCompose(path, RangeProject)
}

// CheckRawPlatformCompose applies the raw-file restrictions to a platform
// Compose file, whose fixed project name differs from the range name.
func CheckRawPlatformCompose(path string) error {
	return checkRawCompose(path, PlatformProject)
}

func checkRawCompose(path, expectedName string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read raw Compose file %s: %w", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("parse raw Compose file %s: %w", path, err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("raw Compose file %s must contain one mapping document", path)
	}
	root := document.Content[0]
	name := ""
	for index := 0; index+1 < len(root.Content); index += 2 {
		key := root.Content[index].Value
		if key == "name" {
			name = root.Content[index+1].Value
		}
	}
	if name != expectedName {
		return fmt.Errorf("raw Compose file %s top-level name is %q, want %q", path, name, expectedName)
	}
	for _, forbidden := range []string{"profiles", "extends", "include"} {
		if findYAMLKey(root, forbidden) {
			return fmt.Errorf("raw Compose file %s uses forbidden %q", path, forbidden)
		}
	}
	return nil
}

// CheckCrossMode ensures the source and release models differ only in fields
// that are intentionally mode-specific.
func CheckCrossMode(source, release []byte) error {
	if _, err := decodeCompose(source); err != nil {
		return fmt.Errorf("decode source Compose: %w", err)
	}
	if _, err := decodeCompose(release); err != nil {
		return fmt.Errorf("decode release Compose: %w", err)
	}
	left, err := decodeJSONMap(source)
	if err != nil {
		return err
	}
	right, err := decodeJSONMap(release)
	if err != nil {
		return err
	}
	leftServices := mapValue(left, "services")
	rightServices := mapValue(right, "services")
	for _, services := range []map[string]any{leftServices, rightServices} {
		for _, raw := range services {
			service, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			delete(service, "image")
			delete(service, "build")
			delete(service, "pull_policy")
		}
	}
	if !reflect.DeepEqual(left, right) {
		return fmt.Errorf("source and release Compose differ outside image, build, and pull_policy")
	}
	return nil
}

// CheckCrossPackage verifies build consistency for service keys shared by two
// source-mode package models.
func CheckCrossPackage(packageA string, sourceA []byte, packageB string, sourceB []byte) error {
	modelA, err := decodeCompose(sourceA)
	if err != nil {
		return fmt.Errorf("decode package %s source Compose: %w", packageA, err)
	}
	modelB, err := decodeCompose(sourceB)
	if err != nil {
		return fmt.Errorf("decode package %s source Compose: %w", packageB, err)
	}
	if modelA.ProjectName != RangeProject || modelB.ProjectName != RangeProject {
		return fmt.Errorf("source Compose projects for %s and %s must both be named %q", packageA, packageB, RangeProject)
	}
	keys := make([]string, 0)
	for key := range modelA.Services {
		if _, exists := modelB.Services[key]; exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		a := modelA.ServicesRaw[key]
		b := modelB.ServicesRaw[key]
		buildA, hasA := a["build"]
		buildB, hasB := b["build"]
		if !hasA && !hasB {
			continue
		}
		if !hasA || !hasB || !reflect.DeepEqual(buildA, buildB) {
			return fmt.Errorf("shared service %q has inconsistent source build between %s and %s", key, packageA, packageB)
		}
		expectedImage := RangeProject + "-" + key
		imageA, imageB := stringValue(a["image"]), stringValue(b["image"])
		if imageA == "" {
			imageA = modelA.ProjectName + "-" + key
		}
		if imageB == "" {
			imageB = modelB.ProjectName + "-" + key
		}
		if imageA != expectedImage || imageB != expectedImage {
			return fmt.Errorf("shared built service %q must produce image %q", key, expectedImage)
		}
	}
	return nil
}

type composeModel struct {
	ProjectName string                    `json:"name"`
	Services    map[string]composeService `json:"services"`
	Networks    map[string]composeNetwork `json:"networks"`
	Volumes     map[string]any            `json:"volumes"`
	ServicesRaw map[string]map[string]any
}

type composeService struct {
	ContainerName string                       `json:"container_name"`
	Networks      map[string]composeAttachment `json:"networks"`
	Ports         []composePort                `json:"ports"`
	Volumes       []composeVolume              `json:"volumes"`
}

type composeAttachment struct {
	IPv4 string `json:"ipv4_address"`
}

type composePort struct {
	HostIP string `json:"host_ip"`
}

type composeVolume struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type composeNetwork struct {
	Name     string       `json:"name"`
	External bool         `json:"external"`
	IPAM     *composeIPAM `json:"ipam"`
}

type composeIPAM struct {
	Config []composeIPAMConfig `json:"config"`
}

type composeIPAMConfig struct {
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway"`
}

func decodeCompose(data []byte) (composeModel, error) {
	var model composeModel
	if err := json.Unmarshal(data, &model); err != nil {
		return composeModel{}, err
	}
	raw, err := decodeJSONMap(data)
	if err != nil {
		return composeModel{}, err
	}
	model.ServicesRaw = map[string]map[string]any{}
	for key, value := range mapValue(raw, "services") {
		if service, ok := value.(map[string]any); ok {
			model.ServicesRaw[key] = service
		}
	}
	return model, nil
}

func decodeJSONMap(data []byte) (map[string]any, error) {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("normalized Compose must be a JSON object")
	}
	return value, nil
}

func mapValue(object map[string]any, key string) map[string]any {
	value, _ := object[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func networkIPAM(network composeNetwork) (string, string, bool) {
	if network.IPAM == nil || len(network.IPAM.Config) != 1 {
		return "", "", false
	}
	config := network.IPAM.Config[0]
	return config.Subnet, config.Gateway, config.Subnet != "" && config.Gateway != ""
}

func checkComposeSafety(model composeModel, root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve installation root: %w", err)
	}
	for serviceKey, service := range model.Services {
		for _, port := range service.Ports {
			if port.HostIP != "127.0.0.1" {
				return fmt.Errorf("service %q published port binds %q, want 127.0.0.1", serviceKey, port.HostIP)
			}
		}
		for _, volume := range service.Volumes {
			if volume.Type == "volume" && volume.Source != "" {
				return fmt.Errorf("service %q uses named volume %q", serviceKey, volume.Source)
			}
			if volume.Type != "bind" {
				continue
			}
			if isDockerSocket(volume.Source) || isDockerSocket(volume.Target) {
				return fmt.Errorf("service %q binds the Docker socket", serviceKey)
			}
			if !filepath.IsAbs(volume.Source) {
				return fmt.Errorf("service %q bind source %q is not resolved to an absolute path", serviceKey, volume.Source)
			}
			relative, err := filepath.Rel(root, filepath.Clean(volume.Source))
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("service %q bind source %q is outside installation root %q", serviceKey, volume.Source, root)
			}
		}
	}
	if len(model.Volumes) != 0 {
		return fmt.Errorf("Compose declares named volumes")
	}
	return nil
}

func isDockerSocket(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/var/run/docker.sock" || clean == "/run/docker.sock" ||
		filepath.Base(clean) == "docker.sock"
}

func parseIPv4Subnet(value string) (net.IP, *net.IPNet, error) {
	ip, subnet, err := net.ParseCIDR(value)
	if err != nil || ip.To4() == nil {
		return nil, nil, fmt.Errorf("not an IPv4 CIDR")
	}
	subnet.IP = ip.To4()
	return ip.To4(), subnet, nil
}

func findYAMLKey(node *yaml.Node, key string) bool {
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == key || findYAMLKey(node.Content[index+1], key) {
				return true
			}
		}
	}
	for _, child := range node.Content {
		if findYAMLKey(child, key) {
			return true
		}
	}
	return false
}
