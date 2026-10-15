package server

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// Static first-match assertions over the shipped reference policy. These
// protect the evidence contract; live dataplane enforcement is a smoke gate.
func TestEUImprovedPolicyConduitEvidence(t *testing.T) {
	type rule struct {
		containd.FirewallRule
		Destinations []string `json:"destinations"`
		Log          bool     `json:"log"`
	}
	data, err := os.ReadFile(filepath.Join(manifest.Dir(repoRoot(), euPackageID), "firewall", "improved.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Firewall struct {
			DefaultAction string `json:"defaultAction"`
			Rules         []rule `json:"rules"`
		} `json:"firewall"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Firewall.DefaultAction != "DENY" {
		t.Fatal("improved must retain default DENY")
	}
	addressMatches := func(cidrs []string, ip string) bool {
		if len(cidrs) == 0 {
			return true
		}
		for _, cidr := range cidrs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil {
				t.Fatalf("invalid policy prefix %q: %v", cidr, err)
			}
			if prefix.Contains(netip.MustParseAddr(ip)) {
				return true
			}
		}
		return false
	}
	firstMatch := func(sourceZone, destZone string, flow conduitDeny) rule {
		for _, candidate := range policy.Firewall.Rules {
			if !slices.Contains(candidate.SourceZones, sourceZone) || !slices.Contains(candidate.DestZones, destZone) ||
				!addressMatches(candidate.Sources, flow.source) || !addressMatches(candidate.Destinations, flow.dest) {
				continue
			}
			for _, protocol := range candidate.Protocols {
				if protocol.Name == "tcp" && protocol.Port == strconv.Itoa(flow.port) {
					return candidate
				}
			}
		}
		return rule{}
	}

	// All 20 lab-2 proof pairs must hit explicit logged denies, including
	// three ingest sources and the two unauthorised PLC maintenance sources.
	denies, err := conduitDenyMatrix(euGeneration(t, nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	sourceZones := map[string]string{"10.10.10.50": "wan", "10.20.20.10": "dmz", "10.20.20.20": "dmz"}
	destZones := map[int]string{502: "lan2", 2404: "lan1", 8080: "lan1", 8081: "lan5"}
	denyIDs := map[int]string{502: "deny-unauthorized-modbus", 2404: "deny-unauthorized-iec104", 8080: "deny-unauthorized-plc-web", 8081: "deny-unauthorized-historian-ingest"}
	httpDenies := 0
	for _, flow := range denies {
		t.Run(fmt.Sprintf("deny/%s/%s/%d", flow.source, flow.dest, flow.port), func(t *testing.T) {
			got := firstMatch(sourceZones[flow.source], destZones[flow.port], flow)
			if got.ID != denyIDs[flow.port] || got.Action != "DENY" || !got.Log {
				t.Fatalf("first rule = %+v; want logged %s", got, denyIDs[flow.port])
			}
		})
		if flow.port == 8080 || flow.port == 8081 {
			httpDenies++
		}
	}
	if len(denies) != 20 || httpDenies != 5 {
		t.Fatalf("proof matrix = %d pairs, %d HTTP; want 20, 5", len(denies), httpDenies)
	}

	// The allows must still win before the logged denies. Enterprise read
	// is authorised; neither it nor engineering PLC maintenance is a deny proof.
	for _, allow := range []struct {
		id, sourceZone, destZone string
		flow                     conduitDeny
	}{
		{"control-centre-to-historian-ingest", "lan4", "lan5", conduitDeny{"10.60.60.20", "10.70.70.40", 8081}},
		{"enterprise-to-historian-read", "wan", "lan5", conduitDeny{"10.10.10.50", "10.70.70.40", 8080}},
		{"engineering-to-plc-web", "dmz", "lan1", conduitDeny{"10.20.20.20", "10.30.30.30", 8080}},
		{"control-centre-to-rtu-iec104", "lan4", "lan1", conduitDeny{"10.60.60.20", "10.30.30.20", 2404}},
		{"rtu-to-feeder-modbus", "lan1", "lan2", conduitDeny{"10.30.30.20", "10.40.40.21", 502}},
		{"plc-to-oltc-modbus", "lan1", "lan2", conduitDeny{"10.30.30.30", "10.40.40.23", 502}},
	} {
		t.Run("allow/"+allow.id, func(t *testing.T) {
			got := firstMatch(allow.sourceZone, allow.destZone, allow.flow)
			if got.ID != allow.id || got.Action != "ALLOW" {
				t.Fatalf("first rule = %+v; want %s ALLOW", got, allow.id)
			}
		})
	}
}
