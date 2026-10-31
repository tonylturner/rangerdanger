package labs

import (
	"fmt"
	"strings"
)

// TopologyFlow is one package-owned teaching conversation.
type TopologyFlow struct {
	ID          string `yaml:"id" json:"id"`
	Source      string `yaml:"source" json:"source"`
	Target      string `yaml:"target" json:"target"`
	Protocol    string `yaml:"protocol" json:"protocol"`
	Port        int    `yaml:"port" json:"port"`
	Cadence     string `yaml:"cadence" json:"cadence"`
	Category    string `yaml:"category" json:"category"`
	LivenessKey string `yaml:"liveness_key" json:"liveness_key"`
}

// LivenessBinding names a telemetry provider, never a node-name convention.
// Session providers match the flow's original source, target and port.
type LivenessBinding struct {
	Provider  string   `yaml:"provider" json:"provider"`
	Device    string   `yaml:"device,omitempty" json:"device,omitempty"`
	Points    []string `yaml:"points,omitempty" json:"points,omitempty"`
	Transport string   `yaml:"transport,omitempty" json:"transport,omitempty"`
}

// ValidateTopologyFlows runs on ReadDefinition as well as catalog loading.
// Empty inventories are legitimate and never select a default flow table.
func ValidateTopologyFlows(def *LabYAML) error {
	nodes := map[string]bool{}
	for _, n := range def.Nodes {
		nodes[n.ID] = true
	}
	for key, b := range def.Liveness {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("liveness: empty key")
		}
		switch b.Provider {
		case "workshop-device":
			if strings.TrimSpace(b.Device) == "" {
				return fmt.Errorf("liveness %s: device is required", key)
			}
		case "operator-device":
			if len(b.Points) == 0 {
				return fmt.Errorf("liveness %s: points are required", key)
			}
			seen := map[string]bool{}
			for _, point := range b.Points {
				if strings.TrimSpace(point) == "" || seen[point] {
					return fmt.Errorf("liveness %s: empty or duplicate point", key)
				}
				seen[point] = true
			}
		case "session-presence":
			if b.Transport != "tcp" && b.Transport != "udp" {
				return fmt.Errorf("liveness %s: transport must be tcp or udp", key)
			}
		case "workshop-supervisor", "traffic-generator", "operator-link", "unobserved":
		default:
			return fmt.Errorf("liveness %s: unknown provider %q", key, b.Provider)
		}
		if (b.Provider != "workshop-device" && b.Device != "") ||
			(b.Provider != "operator-device" && len(b.Points) != 0) ||
			(b.Provider != "session-presence" && b.Transport != "") {
			return fmt.Errorf("liveness %s: fields do not belong to provider %s", key, b.Provider)
		}
	}
	ids, tuples := map[string]bool{}, map[string]bool{}
	for _, f := range def.Flows {
		if strings.TrimSpace(f.ID) == "" || ids[f.ID] {
			return fmt.Errorf("flows: empty or duplicate id %q", f.ID)
		}
		ids[f.ID] = true
		if strings.TrimSpace(f.Source) == "" || strings.TrimSpace(f.Target) == "" ||
			!nodes[f.Source] || !nodes[f.Target] || f.Source == f.Target {
			return fmt.Errorf("flow %s: invalid endpoints", f.ID)
		}
		if f.Port < 1 || f.Port > 65535 || strings.TrimSpace(f.Protocol) == "" || strings.TrimSpace(f.Cadence) == "" {
			return fmt.Errorf("flow %s: port, protocol and cadence are required", f.ID)
		}
		if f.Category != "baseline" && f.Category != "scenario" {
			return fmt.Errorf("flow %s: category must be baseline or scenario", f.ID)
		}
		_, ok := def.Liveness[f.LivenessKey]
		if !ok {
			return fmt.Errorf("flow %s: missing liveness binding %q", f.ID, f.LivenessKey)
		}
		// Display labels must not allow a duplicate endpoint/port tuple.
		tuple := fmt.Sprintf("%s/%s/%d", f.Source, f.Target, f.Port)
		if tuples[tuple] {
			return fmt.Errorf("flow %s: duplicate tuple", f.ID)
		}
		tuples[tuple] = true
	}
	return nil
}
