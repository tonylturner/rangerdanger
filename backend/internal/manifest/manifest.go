// Package manifest reads a range package's deployment manifest:
// lab-definitions/packages/<id>/manifest.json.
//
// The manifest states what the backend, proxy, smoke gates and release
// tooling need to know about a package's range and that Compose cannot
// express: which container plays which role, which topology node it is,
// and which endpoints the backend calls. Compose stays the deployment
// authority. Values that both files carry (container names, networks,
// static IPs) must agree; the package lint checks that.
//
// File layout of a package directory, by convention (not listed in the
// manifest):
//
//	package.yml          curriculum (labs.PackageYAML)
//	manifest.json        this file
//	compose.source.yml   complete range model, images built on the host
//	compose.release.yml  complete range model, published images
//	nginx.routes.conf    proxy locations for the range, included by the platform proxy
//
// Paths inside the Compose files resolve against the installation root,
// which is always the Compose project directory.
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Schema is the manifest schema version this backend reads.
const Schema = 1

// RangeProject is the Compose project name of every range. One range runs
// at a time; the platform runs as PlatformProject.
const (
	RangeProject    = "rangerdanger"
	PlatformProject = "rangerdanger-platform"
)

// Conventional file names inside a package directory.
const (
	FileName   = "manifest.json"
	RoutesFile = "nginx.routes.conf"
)

// Mode selects the Compose file of a package.
type Mode string

const (
	ModeSource  Mode = "source"  // compose.source.yml: images built on the host by setup
	ModeRelease Mode = "release" // compose.release.yml: published images, pulled or loaded by setup
)

// ComposeFile is the conventional Compose file name for a mode.
func ComposeFile(mode Mode) string { return "compose." + string(mode) + ".yml" }

// Role is what a service does in the range. The set is closed: a package
// that needs a new role adds it here with the code that consumes it.
type Role string

const (
	RoleFirewall      Role = "firewall"       // containd; endpoints: api
	RoleRTAC          Role = "rtac"           // substation automation controller; endpoints: api
	RolePLC           Role = "plc"            // OpenPLC
	RoleHMI           Role = "hmi"            // operator HMI
	RoleHMIPoller     Role = "hmi-poller"     // HMI data poller
	RoleHistorian     Role = "historian"      // process historian
	RoleTimeSource    Role = "time-source"    // GPS clock
	RoleFieldDevice   Role = "field-device"   // relay, recloser, regulator, capacitor bank, ...
	RoleProcessSolver Role = "process-solver" // OpenDSS truth plane
	RoleAttacker      Role = "attacker"       // Kali
	RoleJumpHost      Role = "jump-host"      // vendor jump box
	RoleEngineeringWS Role = "engineering-ws" // engineering workstation
	RoleCorporateWS   Role = "corporate-ws"   // corporate workstation
)

// Roles is the closed role set, for validation.
var Roles = map[Role]bool{
	RoleFirewall: true, RoleRTAC: true, RolePLC: true, RoleHMI: true,
	RoleHMIPoller: true, RoleHistorian: true, RoleTimeSource: true,
	RoleFieldDevice: true, RoleProcessSolver: true, RoleAttacker: true,
	RoleJumpHost: true, RoleEngineeringWS: true, RoleCorporateWS: true,
}

// Manifest is one package's manifest.json.
type Manifest struct {
	Schema   int       `json:"schema"`
	Package  string    `json:"package"` // equals package.yml id and the directory name
	Networks []Network `json:"networks"`
	Services []Service `json:"services"`
}

// Network is one Docker network the range attaches to.
type Network struct {
	Key     string `json:"key"`     // Compose network key
	Name    string `json:"name"`    // Engine network name
	Subnet  string `json:"subnet"`  // IPv4 CIDR
	Gateway string `json:"gateway"` // IPv4
	// Platform marks the management network. The platform project owns it;
	// the range references it as external. Exactly one network sets it.
	Platform bool `json:"platform,omitempty"`
	// Zone is the topology network id this network teaches, or empty for
	// networks outside the teaching topology (management, physics).
	Zone string `json:"zone,omitempty"`
}

// Service is one Compose service of the range.
type Service struct {
	Key        string      `json:"key"`            // Compose service key
	Container  string      `json:"container"`      // container_name
	Node       string      `json:"node,omitempty"` // topology node id, if the service is a visible node
	Roles      []Role      `json:"roles"`
	Interfaces []Interface `json:"interfaces"`
	// Endpoints are URLs the backend calls, by name ("api"). They
	// use addresses reachable from the platform on the management network.
	Endpoints map[string]string `json:"endpoints,omitempty"`
}

// Interface is one static network attachment.
type Interface struct {
	Network string `json:"network"` // Network.Key
	IPv4    string `json:"ipv4"`
}

// Dir is the package directory under the installation root.
func Dir(root, packageID string) string {
	return filepath.Join(root, "lab-definitions", "packages", packageID)
}

// Load strictly decodes root/lab-definitions/packages/<id>/manifest.json.
// It checks only the schema version and that the package id matches the
// directory; semantic and Compose agreement checks are separate.
func Load(root, packageID string) (*Manifest, error) {
	path := filepath.Join(Dir(root, packageID), FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var m Manifest
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode %s: trailing data after the manifest object", path)
	}
	if m.Schema != Schema {
		return nil, fmt.Errorf("%s: schema %d, want %d", path, m.Schema, Schema)
	}
	if m.Package != packageID {
		return nil, fmt.Errorf("%s: package %q does not match directory %q", path, m.Package, packageID)
	}
	return &m, nil
}

// ServiceByRole returns the first service with the role.
func (m *Manifest) ServiceByRole(role Role) (Service, bool) {
	for _, svc := range m.Services {
		for _, r := range svc.Roles {
			if r == role {
				return svc, true
			}
		}
	}
	return Service{}, false
}

// ServiceByNode returns the service that renders a topology node.
func (m *Manifest) ServiceByNode(node string) (Service, bool) {
	for _, svc := range m.Services {
		if svc.Node != "" && svc.Node == node {
			return svc, true
		}
	}
	return Service{}, false
}

// Endpoint returns a named endpoint of the first service with the role.
func (m *Manifest) Endpoint(role Role, name string) (string, bool) {
	svc, ok := m.ServiceByRole(role)
	if !ok {
		return "", false
	}
	url, ok := svc.Endpoints[name]
	return url, ok
}
