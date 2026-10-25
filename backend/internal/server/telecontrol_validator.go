package server

import (
	"fmt"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// roleTeachingIP is the address of the role's service on its one teaching
// network (a manifest network with a zone).
func roleTeachingIP(gen *lifecycle.Generation, role manifest.Role) (string, error) {
	svc, err := roleService(gen, role)
	if err != nil {
		return "", err
	}
	zones := map[string]bool{}
	for _, network := range gen.Manifest.Networks {
		if network.Zone != "" {
			zones[network.Key] = true
		}
	}
	var ips []string
	for _, iface := range svc.Interfaces {
		if zones[iface.Network] {
			ips = append(ips, iface.IPv4)
		}
	}
	if len(ips) != 1 {
		return "", fmt.Errorf("service %s has %d teaching addresses, want 1", svc.Key, len(ips))
	}
	return ips[0], nil
}
