package orchestrator

import (
	"strings"
	"testing"
)

// TestResolveNetworkName_KnownZones confirms every entry in
// networkNameMap resolves to its Docker network name. If a zone is
// renamed in the map without updating its docker-compose counterpart
// (or vice versa), this test won't catch that — but it pins the
// shape of the lookup so accidental key deletions get caught.
func TestResolveNetworkName_KnownZones(t *testing.T) {
	cases := []struct {
		zone string
		want string
	}{
		{"enterprise_net", "rangerdanger_enterprise_net"},
		{"vendor_net", "rangerdanger_vendor_net"},
		{"ot_ops_net", "rangerdanger_ot_ops_net"},
		{"field_net", "rangerdanger_field_net"},
		{"physics_net", "rangerdanger_physics_net"},
	}
	for _, tc := range cases {
		t.Run(tc.zone, func(t *testing.T) {
			got, err := resolveNetworkName(tc.zone)
			if err != nil {
				t.Fatalf("resolveNetworkName(%q) returned error: %v", tc.zone, err)
			}
			if got != tc.want {
				t.Errorf("resolveNetworkName(%q) = %q, want %q", tc.zone, got, tc.want)
			}
		})
	}
}

// TestResolveNetworkName_UnknownZoneFailsFast pins the fail-fast
// behavior on misspelled or legacy zone names. Before this changed
// in v0.1.2, createContainer silently no-op'd on unknown zones and
// the container landed on Docker's default bridge — a non-obvious
// failure mode for custom lab definitions. The error message must
// include the offending zone and the list of valid ones.
func TestResolveNetworkName_UnknownZoneFailsFast(t *testing.T) {
	cases := []struct {
		name string
		zone string
	}{
		{"empty zone", ""},
		{"misspelled zone", "enteprise_net"},
		{"legacy oil-plant zone", "it_net"},
		{"legacy oil-plant zone (dmz)", "dmz_net"},
		{"legacy oil-plant zone (ot_control)", "ot_control_net"},
		{"legacy oil-plant zone (ot_safety)", "ot_safety_net"},
		{"unrelated string", "some_other_value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveNetworkName(tc.zone)
			if err == nil {
				t.Fatalf("resolveNetworkName(%q) = %q, want error", tc.zone, got)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.zone) {
				t.Errorf("error %q must mention the bad zone name %q", msg, tc.zone)
			}
			// Error must list the valid zones so a user authoring a
			// custom lab can spot their typo immediately.
			for _, valid := range []string{"enterprise_net", "vendor_net", "ot_ops_net", "field_net", "physics_net"} {
				if !strings.Contains(msg, valid) {
					t.Errorf("error %q must list valid zone %q", msg, valid)
				}
			}
		})
	}
}
