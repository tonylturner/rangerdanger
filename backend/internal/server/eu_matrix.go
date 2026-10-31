package server

import (
	"fmt"
	"net"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// Each source has exactly one authored teaching network. Desktop, management
// and process interfaces in the manifest are not eligible probe identities.
func euSource(gen *lifecycle.Generation, id string) (label, network, ip string, err error) {
	for _, node := range gen.Package.Template.Nodes {
		if node.ID != id {
			continue
		}
		if len(node.Networks) != 1 || node.Name == "" {
			return "", "", "", fmt.Errorf("source %s needs one topology network and a display name", id)
		}
		network = node.Networks[0]
		teaching := false
		for _, n := range gen.Package.Template.Networks {
			if n.ID == network && n.Zone != "" {
				teaching = true
			}
		}
		if !teaching {
			return "", "", "", fmt.Errorf("source %s network %s is not a teaching zone", id, network)
		}
		ip, err = nodeIP(gen, id, network)
		if err == nil && net.ParseIP(ip) == nil {
			err = fmt.Errorf("invalid source address for %s", id)
		}
		return node.Name, network, ip, err
	}
	return "", "", "", fmt.Errorf("missing topology source %s", id)
}
