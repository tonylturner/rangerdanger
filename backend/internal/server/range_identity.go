package server

import (
	"fmt"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// The helpers below resolve range identities (containers, addresses,
// endpoints) from the generation's manifest, by topology node or by role.
// Handlers never name a container or an address of the range themselves.

// nodeService is the manifest service that renders topology node node.
func nodeService(gen *lifecycle.Generation, node string) (manifest.Service, error) {
	svc, ok := gen.Manifest.ServiceByNode(node)
	if !ok {
		return manifest.Service{}, fmt.Errorf("node %s is not part of package %s", node, gen.Package.ID)
	}
	return svc, nil
}

// roleService is the manifest service with role.
func roleService(gen *lifecycle.Generation, role manifest.Role) (manifest.Service, error) {
	svc, ok := gen.Manifest.ServiceByRole(role)
	if !ok {
		return manifest.Service{}, fmt.Errorf("package %s has no %s service", gen.Package.ID, role)
	}
	return svc, nil
}

// firewallContainer is the container of the range's firewall.
func firewallContainer(gen *lifecycle.Generation) (string, error) {
	svc, err := roleService(gen, manifest.RoleFirewall)
	return svc.Container, err
}

// roleEndpoint is a named endpoint of the service with role.
func roleEndpoint(gen *lifecycle.Generation, role manifest.Role, name string) (string, error) {
	endpoint, ok := gen.Manifest.Endpoint(role, name)
	if !ok {
		return "", fmt.Errorf("package %s has no %s %s endpoint", gen.Package.ID, role, name)
	}
	return endpoint, nil
}

// rtacURL is the RTAC API base URL.
func rtacURL(gen *lifecycle.Generation) (string, error) {
	return roleEndpoint(gen, manifest.RoleRTAC, "api")
}

// interfaceIP is the service's address on the network with key network.
func interfaceIP(svc manifest.Service, network string) (string, error) {
	for _, iface := range svc.Interfaces {
		if iface.Network == network {
			return iface.IPv4, nil
		}
	}
	return "", fmt.Errorf("service %s has no interface on %s", svc.Key, network)
}

// nodeIP is node's address on network.
func nodeIP(gen *lifecycle.Generation, node, network string) (string, error) {
	svc, err := nodeService(gen, node)
	if err != nil {
		return "", err
	}
	return interfaceIP(svc, network)
}

// isFirewall reports whether svc is the range firewall.
func isFirewall(svc manifest.Service) bool {
	for _, role := range svc.Roles {
		if role == manifest.RoleFirewall {
			return true
		}
	}
	return false
}
