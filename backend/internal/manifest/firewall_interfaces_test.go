package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

// fixturePolicies are the fixture package's policies: the management
// interface plus the field_net topology zone.
func fixturePolicies() map[string][]byte {
	return map[string][]byte{
		"weak":     []byte(`{"interfaces":[{"name":"lan3","zone":"lan3"},{"name":"lan1","zone":"lan1"}]}`),
		"improved": []byte(`{"interfaces":[{"name":"lan1","zone":"lan1"},{"name":"lan3","zone":"lan3"}]}`),
	}
}

func TestCheckFirewallInterfacesFixture(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	m, err := Load(root, "valid")
	if err != nil {
		t.Fatal(err)
	}
	pkg := loadFixtureTopology(t, root)
	for _, name := range []string{"range.source.json", "range.release.json"} {
		if err := CheckFirewallInterfaces(m, pkg, fixturePolicies(), fixtureNormalized(t, root, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestCheckFirewallInterfacesFailures(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	m, err := Load(root, "valid")
	if err != nil {
		t.Fatal(err)
	}
	pkg := loadFixtureTopology(t, root)
	original := fixtureNormalized(t, root, "range.source.json")
	attachment := func(t *testing.T, value map[string]any, network string) map[string]any {
		return object(t, object(t, object(t, object(t, value["services"])["firewall"])["networks"])[network])
	}
	cases := []struct {
		name     string
		mutate   func(*testing.T, map[string]any)
		policies map[string][]byte
		want     string
	}{
		{"missing", func(t *testing.T, value map[string]any) {
			delete(attachment(t, value, "field_net"), "interface_name")
		}, nil, `network "field_net" has no interface_name`},
		{"empty", func(t *testing.T, value map[string]any) {
			attachment(t, value, "mgmt_net")["interface_name"] = ""
		}, nil, `network "mgmt_net" has no interface_name`},
		{"duplicate", func(t *testing.T, value map[string]any) {
			attachment(t, value, "field_net")["interface_name"] = "lan3"
		}, nil, `share interface_name "lan3"`},
		{"zone mismatch", func(t *testing.T, value map[string]any) {
			attachment(t, value, "field_net")["interface_name"] = "lan2"
		}, nil, `"field_net" interface_name is "lan2", want "lan1"`},
		{"management mismatch", func(t *testing.T, value map[string]any) {
			attachment(t, value, "mgmt_net")["interface_name"] = "eth0"
		}, nil, `"mgmt_net" interface_name is "eth0", want "lan3"`},
		{"policy lacks a pinned interface", nil, map[string][]byte{
			"weak": []byte(`{"interfaces":[{"name":"lan3","zone":"lan3"}]}`),
		}, "policy weak declares interfaces [lan3], firewall interface_name values are [lan1 lan3]"},
		{"policy has an unpinned interface", nil, map[string][]byte{
			"improved": []byte(`{"interfaces":[{"name":"lan1"},{"name":"lan2"},{"name":"lan3"}]}`),
		}, "policy improved declares interfaces [lan1 lan2 lan3]"},
		{"policy is not JSON", nil, map[string][]byte{"weak": []byte(`{`)}, "policy weak: decode interfaces"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := jsonObject(t, original)
			if test.mutate != nil {
				test.mutate(t, model)
			}
			policies := test.policies
			if policies == nil {
				policies = fixturePolicies()
			}
			err := CheckFirewallInterfaces(m, pkg, policies, marshalObject(t, model))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckFirewallInterfaces error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCheckFirewallInterfacesRejectsUnzonedNetwork(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	m, err := Load(root, "valid")
	if err != nil {
		t.Fatal(err)
	}
	pkg := loadFixtureTopology(t, root)
	for index := range m.Networks {
		if m.Networks[index].Key == "field_net" {
			m.Networks[index].Zone = ""
		}
	}
	err = CheckFirewallInterfaces(m, pkg, fixturePolicies(), fixtureNormalized(t, root, "range.source.json"))
	if err == nil || !strings.Contains(err.Error(), "neither management nor a teaching zone") {
		t.Fatalf("CheckFirewallInterfaces error = %v, want unzoned network rejection", err)
	}
}

// TestShippedFirewallInterfaces checks the shipped packages from their raw
// Compose firewall attachments. packagelint runs the same check on Compose's
// normalized model.
func TestShippedFirewallInterfaces(t *testing.T) {
	root := fixturePath(t, "../../..")
	definitions := filepath.Join(root, "lab-definitions")
	for _, id := range []string{"us-dnp3-substation", "eu-iec104-substation"} {
		t.Run(id, func(t *testing.T) {
			m, err := Load(root, id)
			if err != nil {
				t.Fatal(err)
			}
			pkg, err := labs.ReadDefinition(definitions, filepath.Join(Dir(root, id), "package.yml"))
			if err != nil {
				t.Fatal(err)
			}
			policies := map[string][]byte{}
			for name, relative := range pkg.Policies {
				data, err := os.ReadFile(filepath.Join(definitions, relative))
				if err != nil {
					t.Fatal(err)
				}
				policies[name] = data
			}
			firewall, ok := m.ServiceByRole(RoleFirewall)
			if !ok {
				t.Fatal("no firewall service")
			}
			for _, mode := range []Mode{ModeSource, ModeRelease} {
				raw := readComposeYAML(t, filepath.Join(Dir(root, id), ComposeFile(mode)))
				service := object(t, object(t, raw["services"])[firewall.Key])
				model := map[string]any{"services": map[string]any{
					firewall.Key: map[string]any{"networks": service["networks"]},
				}}
				if err := CheckFirewallInterfaces(m, pkg, policies, marshalObject(t, model)); err != nil {
					t.Errorf("%s: %v", mode, err)
				}
			}
		})
	}
}
