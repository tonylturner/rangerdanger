package manifest

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"gopkg.in/yaml.v3"
)

const fixtureRoot = "testdata/valid"

func TestValidFixture(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	m, err := Load(root, "valid")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(m, root); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	pkg := loadFixtureTopology(t, root)
	if err := CheckTopology(m, pkg); err != nil {
		t.Fatalf("CheckTopology: %v", err)
	}
	platformSource := fixtureNormalized(t, root, "platform.source.json")
	platformRelease := fixtureNormalized(t, root, "platform.release.json")
	if err := CheckPlatform(m, platformSource); err != nil {
		t.Fatalf("CheckPlatform source: %v", err)
	}
	if err := CheckPlatform(m, platformRelease); err != nil {
		t.Fatalf("CheckPlatform release: %v", err)
	}
	source := fixtureNormalized(t, root, "range.source.json")
	release := fixtureNormalized(t, root, "range.release.json")
	if err := CheckCompose(m, ModeSource, root, source); err != nil {
		t.Fatalf("CheckCompose source: %v", err)
	}
	if err := CheckCompose(m, ModeRelease, root, release); err != nil {
		t.Fatalf("CheckCompose release: %v", err)
	}
	if err := CheckCrossMode(source, release); err != nil {
		t.Fatalf("CheckCrossMode: %v", err)
	}
	if err := CheckCrossPackage("valid", source, "other", source); err != nil {
		t.Fatalf("CheckCrossPackage: %v", err)
	}
}

func TestValidateNegativeManifestFixtures(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	files, err := filepath.Glob(filepath.Join("testdata", "invalid", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no negative manifest fixtures")
	}
	for _, file := range files {
		t.Run(strings.TrimSuffix(filepath.Base(file), ".json"), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var m Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			if err := Validate(&m, root); err == nil {
				t.Fatal("Validate accepted negative fixture")
			}
		})
	}
}

func TestValidateHasSpecificFailures(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	cases := []struct {
		file string
		want string
	}{
		{"duplicate-service-key.json", `duplicate service key "firewall"`},
		{"duplicate-container.json", `share container "fixture-firewall"`},
		{"duplicate-node.json", `share node "fw-1"`},
		{"duplicate-ip.json", `IP "10.40.40.2" is shared`},
		{"unknown-role.json", `unknown role "unknown"`},
		{"missing-platform-network.json", "exactly one platform network"},
		{"wrong-platform-name.json", "must be named rangerdanger_mgmt_net"},
		{"unknown-interface-network.json", `undeclared network "missing_net"`},
		{"ip-outside-subnet.json", "is outside network"},
		{"gateway-ip.json", "uses network"},
		{"endpoint-not-management-ip.json", "must be its management IP"},
		{"firewall-api-missing.json", "firewall service with an api endpoint is required"},
	}
	for _, test := range cases {
		t.Run(test.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "invalid", test.file))
			if err != nil {
				t.Fatal(err)
			}
			var m Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			err = Validate(&m, root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateRequiresAllPackageFiles(t *testing.T) {
	root := t.TempDir()
	packageDir := Dir(root, "valid")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.yml", FileName, ComposeFile(ModeSource), ComposeFile(ModeRelease)} {
		if err := os.WriteFile(filepath.Join(packageDir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Load(fixturePath(t, fixtureRoot), "valid")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(m, root); err == nil || !strings.Contains(err.Error(), RoutesFile) {
		t.Fatalf("Validate error = %v, want missing route file", err)
	}
}

func TestCheckPlatformFailures(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	m, err := Load(root, "valid")
	if err != nil {
		t.Fatal(err)
	}
	source := fixtureNormalized(t, root, "platform.source.json")
	t.Run("network IPAM", func(t *testing.T) {
		model := jsonObject(t, source)
		networks := object(t, model["networks"])
		networks["mgmt_net"].(map[string]any)["ipam"].(map[string]any)["config"].([]any)[0].(map[string]any)["subnet"] = "10.98.99.0/24"
		err := CheckPlatform(m, marshalObject(t, model))
		if err == nil || !strings.Contains(err.Error(), "IPAM") {
			t.Fatalf("CheckPlatform error = %v, want IPAM mismatch", err)
		}
	})
	t.Run("IP collision", func(t *testing.T) {
		model := jsonObject(t, source)
		service := object(t, object(t, model["services"])["backend"])
		object(t, service["networks"])["mgmt_net"].(map[string]any)["ipv4_address"] = "10.99.99.2"
		err := CheckPlatform(m, marshalObject(t, model))
		if err == nil || !strings.Contains(err.Error(), "collides") {
			t.Fatalf("CheckPlatform error = %v, want collision", err)
		}
	})
}

func TestCheckTopologyFailures(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	m, err := Load(root, "valid")
	if err != nil {
		t.Fatal(err)
	}
	pkg := loadFixtureTopology(t, root)
	t.Run("manifest node missing", func(t *testing.T) {
		bad := *m
		bad.Services = append([]Service(nil), m.Services...)
		bad.Services[0].Node = "missing"
		err := CheckTopology(&bad, pkg)
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("CheckTopology error = %v, want missing node", err)
		}
	})
	t.Run("container mapping missing", func(t *testing.T) {
		bad := *m
		bad.Services = append([]Service(nil), m.Services...)
		bad.Services[0].Container = "other-container"
		err := CheckTopology(&bad, pkg)
		if err == nil || !strings.Contains(err.Error(), "does not match topology node") {
			t.Fatalf("CheckTopology error = %v, want container mismatch", err)
		}
	})
	t.Run("zone missing", func(t *testing.T) {
		bad := *m
		bad.Networks = append([]Network(nil), m.Networks...)
		bad.Networks[1].Zone = "wrong-zone"
		err := CheckTopology(&bad, pkg)
		if err == nil || !strings.Contains(err.Error(), "unknown topology zone") {
			t.Fatalf("CheckTopology error = %v, want unknown zone", err)
		}
	})
}

func TestCheckComposeSafetyAndAgreement(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	m, err := Load(root, "valid")
	if err != nil {
		t.Fatal(err)
	}
	original := fixtureNormalized(t, root, "range.source.json")
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"missing service", func(value map[string]any) { delete(object(t, value["services"]), "firewall") }, "service set"},
		{"wrong container", func(value map[string]any) {
			object(t, object(t, value["services"])["firewall"])["container_name"] = "wrong"
		}, "container_name"},
		{"wrong static IP", func(value map[string]any) {
			object(t, object(t, object(t, value["services"])["firewall"])["networks"])["field_net"].(map[string]any)["ipv4_address"] = "10.40.40.3"
		}, "IPv4"},
		{"wrong network name", func(value map[string]any) {
			object(t, value["networks"])["field_net"].(map[string]any)["name"] = "wrong"
		}, "network"},
		{"wrong range subnet", func(value map[string]any) {
			object(t, object(t, value["networks"])["field_net"])["ipam"].(map[string]any)["config"].([]any)[0].(map[string]any)["subnet"] = "10.41.40.0/24"
		}, "IPAM"},
		{"platform not external", func(value map[string]any) {
			object(t, value["networks"])["mgmt_net"].(map[string]any)["external"] = false
		}, "must be external"},
		{"platform has IPAM", func(value map[string]any) {
			object(t, object(t, value["networks"])["mgmt_net"])["ipam"] = map[string]any{"config": []any{map[string]any{"subnet": "10.99.99.0/24"}}}
		}, "must not declare IPAM"},
		{"published outside loopback", func(value map[string]any) {
			object(t, object(t, object(t, value["services"])["firewall"])["ports"].([]any)[0])["host_ip"] = "0.0.0.0"
		}, "published port binds"},
		{"relative bind", func(value map[string]any) {
			object(t, object(t, object(t, value["services"])["firewall"])["volumes"].([]any)[0])["source"] = "./data/firewall"
		}, "not resolved to an absolute path"},
		{"outside bind", func(value map[string]any) {
			object(t, object(t, object(t, value["services"])["firewall"])["volumes"].([]any)[0])["source"] = "/tmp/outside"
		}, "outside installation root"},
		{"docker socket", func(value map[string]any) {
			volume := object(t, object(t, object(t, value["services"])["firewall"])["volumes"].([]any)[0])
			volume["source"] = "/var/run/docker.sock"
			volume["target"] = "/var/run/docker.sock"
		}, "Docker socket"},
		{"named volume", func(value map[string]any) {
			volume := object(t, object(t, object(t, value["services"])["firewall"])["volumes"].([]any)[0])
			volume["type"] = "volume"
			volume["source"] = "persistent-data"
		}, "named volume"},
		{"top-level named volume", func(value map[string]any) { value["volumes"] = map[string]any{"persistent-data": map[string]any{}} }, "declares named volumes"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := jsonObject(t, original)
			test.mutate(model)
			err := CheckCompose(m, ModeSource, root, marshalObject(t, model))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckCompose error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestRawComposeFailures(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{"profiles.yml", "forbidden \"profiles\""},
		{"extends.yml", "forbidden \"extends\""},
		{"include.yml", "forbidden \"include\""},
		{"wrong-name.yml", `top-level name is "rangerdanger-platform"`},
	}
	for _, test := range cases {
		t.Run(test.file, func(t *testing.T) {
			err := CheckRawCompose(filepath.Join("testdata", "invalid", "raw", test.file))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckRawCompose error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCrossModeAndCrossPackageFailures(t *testing.T) {
	root := fixturePath(t, fixtureRoot)
	source := fixtureNormalized(t, root, "range.source.json")
	release := fixtureNormalized(t, root, "range.release.json")
	differentMode := jsonObject(t, release)
	object(t, object(t, differentMode["services"])["firewall"])["container_name"] = "release-container"
	if err := CheckCrossMode(source, marshalObject(t, differentMode)); err == nil || !strings.Contains(err.Error(), "differ outside") {
		t.Fatalf("CheckCrossMode error = %v, want semantic divergence", err)
	}
	differentBuild := jsonObject(t, source)
	object(t, object(t, object(t, differentBuild["services"])["firewall"])["build"])["context"] = "/other"
	if err := CheckCrossPackage("valid", source, "other", marshalObject(t, differentBuild)); err == nil || !strings.Contains(err.Error(), "inconsistent source build") {
		t.Fatalf("CheckCrossPackage error = %v, want build divergence", err)
	}
}

func fixturePath(t *testing.T, relative string) string {
	t.Helper()
	path, err := filepath.Abs(relative)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func fixtureNormalized(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "normalized", name))
	if err != nil {
		t.Fatal(err)
	}
	var model map[string]any
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	if name == "range.source.json" || name == "range.release.json" {
		service := object(t, object(t, model["services"])["firewall"])
		volume := object(t, service["volumes"].([]any)[0])
		source := volume["source"].(string)
		oldRoot := strings.TrimSuffix(source, "/data/firewall")
		replaceRoot(model, oldRoot, root)
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func replaceRoot(value any, oldRoot, newRoot string) {
	switch current := value.(type) {
	case map[string]any:
		for key, nested := range current {
			switch text := nested.(type) {
			case string:
				if strings.HasPrefix(text, oldRoot) {
					suffix := strings.TrimPrefix(text, oldRoot)
					current[key] = filepath.Join(newRoot, filepath.FromSlash(strings.TrimPrefix(suffix, "/")))
				}
			default:
				replaceRoot(nested, oldRoot, newRoot)
			}
		}
	case []any:
		for index := range current {
			replaceRoot(current[index], oldRoot, newRoot)
		}
	}
}

func loadFixtureTopology(t *testing.T, root string) labs.Package {
	t.Helper()
	path := filepath.Join(root, "lab-definitions", "packages", "valid", "topology.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var topology labs.LabYAML
	if err := yaml.Unmarshal(data, &topology); err != nil {
		t.Fatal(err)
	}
	return labs.Package{ID: "valid", Template: topology}
}

func jsonObject(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T (%v)", value, value)
	}
	return result
}

func marshalObject(t *testing.T, value map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFixtureContainsDockerNormalizedOutputs(t *testing.T) {
	for _, name := range []string{"platform.source.json", "platform.release.json", "range.source.json", "range.release.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtureRoot, "normalized", name))
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if value["name"] == nil || value["services"] == nil || value["networks"] == nil {
				t.Fatalf("normalized Compose fixture lacks required fields: %s", fmt.Sprint(value))
			}
		})
	}
}

func TestIPv4SubnetRejectsIPv6(t *testing.T) {
	if _, _, err := parseIPv4Subnet("2001:db8::/64"); err == nil {
		t.Fatal("parseIPv4Subnet accepted IPv6")
	}
	if ip := net.ParseIP("10.0.0.1"); ip == nil || ip.To4() == nil {
		t.Fatal("unexpected IPv4 parser failure")
	}
}
