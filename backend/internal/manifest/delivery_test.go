package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestManifestRejectsOldSchema(t *testing.T) {
	root := t.TempDir()
	dir := Dir(root, "valid")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixtureRoot, "lab-definitions/packages/valid/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m.Schema = 1
	data, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "valid"); err == nil || !strings.Contains(err.Error(), "schema 1, want 2") {
		t.Fatalf("old schema accepted: %v", err)
	}
}

func deliveryFixture(t *testing.T) (*Manifest, string, map[string]any) {
	t.Helper()
	root := fixturePath(t, fixtureRoot)
	m, err := Load(root, "valid")
	if err != nil {
		t.Fatal(err)
	}
	m.Networks = append(m.Networks, Network{
		Key: "desk_vendor_net", Name: "rangerdanger_desk_vendor",
		Subnet: "10.98.2.0/28", Gateway: "10.98.2.1", Platform: true, Delivery: true,
	})
	m.Services = append(m.Services, Service{
		Key: "vendor_jump", Container: "fixture-vendor", Node: "vendor-1", Roles: []Role{RoleJumpHost},
		Interfaces: []Interface{{Network: "desk_vendor_net", IPv4: "10.98.2.3"}},
	})
	model := jsonObject(t, fixtureNormalized(t, root, "platform.source.json"))
	object(t, model["networks"])["desk_vendor_net"] = map[string]any{
		"name": "rangerdanger_desk_vendor", "internal": true,
		"ipam": map[string]any{"config": []any{map[string]any{"subnet": "10.98.2.0/28", "gateway": "10.98.2.1"}}},
	}
	object(t, model["services"])["proxy"] = map[string]any{
		"networks": map[string]any{"desk_vendor_net": map[string]any{"ipv4_address": "10.98.2.2"}},
	}
	return m, root, model
}

func TestManifestDeliveryInvariants(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     func(*Manifest)
	}{
		{"valid", "", func(*Manifest) {}},
		{"range owned", "platform-owned", func(m *Manifest) { m.Networks[2].Platform = false }},
		{"teaching zone", "have no zone", func(m *Manifest) { m.Networks[2].Zone = "vendor_net" }},
		{"wrong name", "fixed platform name and IPAM", func(m *Manifest) { m.Networks[2].Name = "other" }},
		{"wrong IPAM", "fixed platform name and IPAM", func(m *Manifest) { m.Networks[2].Subnet = "10.98.2.0/24" }},
		{"unmarked delivery", "must be a delivery network", func(m *Manifest) { m.Networks[2].Delivery = false }},
		{"management also attached", "must not attach to management", func(m *Manifest) {
			m.Services[1].Interfaces = append(m.Services[1].Interfaces, Interface{Network: "mgmt_net", IPv4: "10.99.99.60"})
		}},
		{"delivery endpoint", "must not declare endpoints", func(m *Manifest) {
			m.Services[1].Endpoints = map[string]string{"api": "http://10.98.2.3:3000"}
		}},
		{"wrong IP", "reserved desktop IP", func(m *Manifest) { m.Services[1].Interfaces[0].IPv4 = "10.98.2.4" }},
		{"missing node", "designated desktop node", func(m *Manifest) { m.Services[1].Node = "" }},
		{"second node", "at most one", func(m *Manifest) {
			m.Services = append(m.Services, Service{
				Key: "extra", Container: "extra", Node: "extra",
				Interfaces: []Interface{{Network: "desk_vendor_net", IPv4: "10.98.2.4"}},
			})
		}},
		{"desktop back on management", "must attach to delivery", func(m *Manifest) {
			m.Services[1].Interfaces = []Interface{{Network: "mgmt_net", IPv4: "10.99.99.60"}}
		}},
		{"delivery not management", "exactly one management", func(m *Manifest) { m.Networks = m.Networks[1:] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, root, _ := deliveryFixture(t)
			tc.mutate(m)
			err := Validate(m, root)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("Validate = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestManifestDeliveryPlatform(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     func(map[string]any)
	}{
		{"valid", "", func(map[string]any) {}},
		{"missing network", "missing network", func(model map[string]any) { delete(object(t, model["networks"]), "desk_vendor_net") }},
		{"not internal", "must be internal", func(model map[string]any) {
			object(t, object(t, model["networks"])["desk_vendor_net"])["internal"] = false
		}},
		{"external owner", "owned by the platform", func(model map[string]any) {
			object(t, object(t, model["networks"])["desk_vendor_net"])["external"] = true
		}},
		{"missing proxy", "attach the proxy", func(model map[string]any) { delete(object(t, model["services"]), "proxy") }},
		{"wrong proxy IP", "attach the proxy", func(model map[string]any) {
			object(t, object(t, object(t, object(t, model["services"])["proxy"])["networks"])["desk_vendor_net"])["ipv4_address"] = "10.98.2.5"
		}},
		{"backend exposed", "only permits the platform proxy", func(model map[string]any) {
			object(t, object(t, object(t, model["services"])["backend"])["networks"])["desk_vendor_net"] = map[string]any{"ipv4_address": "10.98.2.4"}
		}},
		{"IP collision", "collides", func(model map[string]any) {
			object(t, object(t, object(t, object(t, model["services"])["proxy"])["networks"])["desk_vendor_net"])["ipv4_address"] = "10.98.2.3"
		}},
		{"IPAM mismatch", "IPAM", func(model map[string]any) {
			object(t, object(t, model["networks"])["desk_vendor_net"])["ipam"] = nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, model := deliveryFixture(t)
			tc.mutate(model)
			err := CheckPlatform(m, marshalObject(t, model))
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("CheckPlatform = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestManifestShippedDeliveryModels(t *testing.T) {
	root := fixturePath(t, "../../..")
	for _, pkg := range []string{"us-dnp3-substation", "eu-iec104-substation"} {
		t.Run(pkg, func(t *testing.T) {
			m, err := Load(root, pkg)
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(m, root); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"docker-compose.yml", "docker-compose.release.yml"} {
				model := readComposeYAML(t, filepath.Join(root, name))
				// CheckPlatform consumes only project/network/attachment
				// fields. Short-form ports and mounts need Docker to expand.
				for _, raw := range object(t, model["services"]) {
					svc := object(t, raw)
					delete(svc, "volumes")
					delete(svc, "ports")
				}
				if err := CheckPlatform(m, marshalObject(t, model)); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				// Even EU's unused corporate delivery bridge persists.
				for key := range deliveryDesktops {
					if object(t, object(t, model["networks"])[key])["internal"] != true {
						t.Errorf("%s: %s is not internal", name, key)
					}
				}
			}
			for _, mode := range []Mode{ModeSource, ModeRelease} {
				model := readComposeYAML(t, filepath.Join(Dir(root, pkg), ComposeFile(mode)))
				for _, svc := range m.Services {
					actual := object(t, object(t, model["services"])[svc.Key])
					attachments := mapValue(actual, "networks")
					if len(attachments) != len(svc.Interfaces) {
						t.Fatalf("%s %s attachments disagree", mode, svc.Key)
					}
					for _, iface := range svc.Interfaces {
						if object(t, attachments[iface.Network])["ipv4_address"] != iface.IPv4 {
							t.Errorf("%s %s %s IP mismatch", mode, svc.Key, iface.Network)
						}
					}
				}
				for _, network := range m.Networks {
					if network.Platform {
						actual := object(t, object(t, model["networks"])[network.Key])
						if actual["external"] != true || actual["name"] != network.Name || actual["ipam"] != nil {
							t.Errorf("%s: platform ownership mismatch for %s", mode, network.Key)
						}
					}
				}
			}
		})
	}
}

// This is a source-level attachment test, not a substitute for Compose
// normalization or packagelint (which starts Docker).
func readComposeYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var model map[string]any
	if err := yaml.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	return model
}
