package server

import (
	"fmt"
	"net"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// The six policy intents expand to nine positive endpoints: RTU access to
// every field device matters independently. Negatives likewise cover all four
// field devices, not just one representative of a subnet-wide rule.
func euValidationMatrix() []validationProbeSpec {
	rows := []validationProbeSpec{
		{"cc-1", "", "rtu-1", "station_net", "2404", "allow", "CC IEC 104 to RTU", "authorized"},
		{"plc-1", "", "oltc-1", "field_net", "502", "allow", "PLC to OLTC (pinned station-to-field conduit)", "authorized"},
		{"cc-1", "", "historian-1", "ops_dmz_net", "8081", "allow", "CC historian ingest", "authorized"},
		{"kali-1", "", "historian-1", "ops_dmz_net", "8080", "allow", "Enterprise historian read", "authorized"},
		{"eng-ws-1", "", "plc-1", "station_net", "8080", "allow", "Named engineering PLC maintenance", "authorized"},
		{"kali-1", "", "rtu-1", "station_net", "2404", "deny", "Foreign IEC 104 connect", "unauthorized"},
		{"kali-1", "", "plc-1", "station_net", "8080", "deny", "Enterprise PLC web", "unauthorized"},
		{"kali-1", "", "vendor-jump-1", "vendor_net", "3389", "deny", "Enterprise vendor RDP", "unauthorized"},
		{"kali-1", "", "vendor-jump-1", "vendor_net", "5900", "deny", "Enterprise vendor VNC", "unauthorized"},
	}
	for _, device := range []string{"breaker-1", "rmu-1", "rmu-2", "oltc-1"} {
		rows = append(rows, validationProbeSpec{"rtu-1", "", device, "field_net", "502", "allow", "RTU field Modbus", "authorized"})
		for _, source := range []string{"kali-1", "vendor-jump-1", "eng-ws-1"} {
			rows = append(rows, validationProbeSpec{source, "", device, "field_net", "502", "deny", "Foreign field Modbus connect", "unauthorized"})
		}
	}
	return rows
}

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

func euResolveMatrix(gen *lifecycle.Generation, specs []validationProbeSpec, slot string) ([]validationProbe, error) {
	var probes []validationProbe
	for _, spec := range specs {
		if slot != "" {
			if spec.expected != "deny" {
				continue
			}
			switch slot {
			case "eu-firewall-implementation":
			case "eu-hardening-configurations":
				if spec.toNode != "rtu-1" || spec.fromNode != "kali-1" {
					continue
				}
			case "eu-vendor-rdp-compromise":
				if !(spec.toNode == "vendor-jump-1" || spec.fromNode == "vendor-jump-1" && spec.network == "field_net") {
					continue
				}
			default:
				return nil, fmt.Errorf("slot %s has no negative matrix", slot)
			}
		}
		label, network, ip, err := euSource(gen, spec.fromNode)
		if err != nil {
			return nil, err
		}
		source, err := nodeService(gen, spec.fromNode)
		if err != nil {
			return nil, err
		}
		dest, err := nodeIP(gen, spec.toNode, spec.network)
		if err != nil {
			return nil, err
		}
		note := fmt.Sprintf("%s (topology source %s on %s)", spec.note, ip, network)
		probes = append(probes, validationProbe{source.Container, label, dest, spec.port, spec.expected, note, spec.category})
	}
	return probes, nil
}
