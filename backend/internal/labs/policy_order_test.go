package labs

import (
	"encoding/json"
	"net/netip"
	"slices"
	"testing"
)

// This checks all selector intersections, not just the finite plan proof tuples.
// Monitor ALLOWs after diagnostic DENYs are observations, not grants.
func TestShippedPolicyFirstMatchCutover(t *testing.T) {
	for _, id := range []string{"us-dnp3-substation", "eu-iec104-substation"} {
		for _, name := range []string{"weak", "improved"} {
			t.Run(id+"/"+name, func(t *testing.T) {
				pkg := planPackage(t, id)
				rules, _, data := planPolicy(t, pkg, name)
				var envelope struct {
					Schema string `json:"schema_version"`
				}
				if err := json.Unmarshal(data, &envelope); err != nil || envelope.Schema != "0.1.10" {
					t.Fatalf("schema = %q, error %v", envelope.Schema, err)
				}
				for i, deny := range rules {
					if deny.Action != "DENY" || deny.ICS != nil && deny.ICS.Mode == "monitor" {
						continue
					}
					for _, allow := range rules[i+1:] {
						if allow.Action != "ALLOW" || allow.ICS != nil && allow.ICS.Mode == "monitor" {
							continue
						}
						if zonesOverlap(deny.SourceZones, allow.SourceZones) &&
							zonesOverlap(deny.DestZones, allow.DestZones) &&
							addressesOverlap(t, deny.Sources, allow.Sources) &&
							addressesOverlap(t, deny.Destinations, allow.Destinations) &&
							protocolsOverlap(t, deny.Protocols, allow.Protocols) {
							t.Errorf("earlier DENY %s overlaps ALLOW %s", deny.ID, allow.ID)
						}
					}
				}
				for _, ids := range [][]string{nil, {pkg.Curriculum.Catalog[0].ID}} {
					compiled := mustCompilePlan(t, pkg, ids)
					if err := json.Unmarshal(compiled.Config, &envelope); err != nil || envelope.Schema != "0.1.10" {
						t.Fatalf("compiled schema = %q, error %v", envelope.Schema, err)
					}
				}
			})
		}
	}
}

func zonesOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	return slices.ContainsFunc(a, func(zone string) bool { return slices.Contains(b, zone) })
}

func addressesOverlap(t *testing.T, a, b []string) bool {
	t.Helper()
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	prefix := func(value string) netip.Prefix {
		if addr, err := netip.ParseAddr(value); err == nil {
			return netip.PrefixFrom(addr, addr.BitLen())
		}
		p, err := netip.ParsePrefix(value)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, x := range a {
		for _, y := range b {
			if prefix(x).Overlaps(prefix(y)) {
				return true
			}
		}
	}
	return false
}

func protocolsOverlap(t *testing.T, a, b []PlanProtocol) bool {
	t.Helper()
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	bounds := func(port string) (int, int) {
		if port == "" {
			return 1, 65535
		}
		lo, hi, err := planPortRange(port)
		if err != nil {
			t.Fatal(err)
		}
		return lo, hi
	}
	for _, x := range a {
		for _, y := range b {
			if x.Name != y.Name {
				continue
			}
			xlo, xhi := bounds(x.Port)
			ylo, yhi := bounds(y.Port)
			if xlo <= yhi && ylo <= xhi {
				return true
			}
		}
	}
	return false
}
