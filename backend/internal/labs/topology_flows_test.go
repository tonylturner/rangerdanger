package labs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func validFlowDefinition() LabYAML {
	return LabYAML{
		Nodes:    []NodeYAML{{ID: "source"}, {ID: "target"}},
		Flows:    []TopologyFlow{{ID: "flow", Source: "source", Target: "target", Protocol: "Modbus", Port: 502, Cadence: "~1s", Category: "baseline", LivenessKey: "opaque"}},
		Liveness: map[string]LivenessBinding{"opaque": {Provider: "unobserved"}},
	}
}

func TestTopologyFlowsValidation(t *testing.T) {
	tests := []struct {
		name string
		edit func(*LabYAML)
	}{
		{"empty id", func(d *LabYAML) { d.Flows[0].ID = "" }},
		{"duplicate id", func(d *LabYAML) { d.Flows = append(d.Flows, d.Flows[0]) }},
		{"duplicate tuple", func(d *LabYAML) { f := d.Flows[0]; f.ID = "other"; d.Flows = append(d.Flows, f) }},
		{"source", func(d *LabYAML) { d.Flows[0].Source = "missing" }},
		{"target", func(d *LabYAML) { d.Flows[0].Target = "missing" }},
		{"self", func(d *LabYAML) { d.Flows[0].Target = "source" }},
		{"zero port", func(d *LabYAML) { d.Flows[0].Port = 0 }},
		{"large port", func(d *LabYAML) { d.Flows[0].Port = 65536 }},
		{"protocol", func(d *LabYAML) { d.Flows[0].Protocol = " " }},
		{"cadence", func(d *LabYAML) { d.Flows[0].Cadence = "" }},
		{"category", func(d *LabYAML) { d.Flows[0].Category = "invented" }},
		{"binding ref", func(d *LabYAML) { d.Flows[0].LivenessKey = "absent" }},
		{"provider", func(d *LabYAML) { d.Liveness["opaque"] = LivenessBinding{Provider: "invented"} }},
		{"empty key", func(d *LabYAML) { d.Liveness[""] = LivenessBinding{Provider: "unobserved"} }},
		{"device required", func(d *LabYAML) { d.Liveness["opaque"] = LivenessBinding{Provider: "workshop-device"} }},
		{"points required", func(d *LabYAML) { d.Liveness["opaque"] = LivenessBinding{Provider: "operator-device"} }},
		{"duplicate points", func(d *LabYAML) {
			d.Liveness["opaque"] = LivenessBinding{Provider: "operator-device", Points: []string{"p", "p"}}
		}},
		{"blank point", func(d *LabYAML) {
			d.Liveness["opaque"] = LivenessBinding{Provider: "operator-device", Points: []string{""}}
		}},
		{"transport required", func(d *LabYAML) { d.Liveness["opaque"] = LivenessBinding{Provider: "session-presence"} }},
		{"invalid transport", func(d *LabYAML) {
			d.Liveness["opaque"] = LivenessBinding{Provider: "session-presence", Transport: "icmp"}
		}},
		{"extraneous device", func(d *LabYAML) {
			d.Liveness["opaque"] = LivenessBinding{Provider: "operator-link", Device: "relay"}
		}},
		{"extraneous points", func(d *LabYAML) {
			d.Liveness["opaque"] = LivenessBinding{Provider: "unobserved", Points: []string{"p"}}
		}},
		{"extraneous transport", func(d *LabYAML) {
			d.Liveness["opaque"] = LivenessBinding{Provider: "traffic-generator", Transport: "tcp"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := validFlowDefinition()
			tt.edit(&def)
			if err := ValidateTopologyFlows(&def); err == nil {
				t.Fatal("accepted invalid flow definition")
			}
		})
	}
}

func TestTopologyFlowsProvidersAndEmptySet(t *testing.T) {
	for _, binding := range []LivenessBinding{
		{Provider: "unobserved"}, {Provider: "workshop-supervisor"}, {Provider: "workshop-device", Device: "relay"},
		{Provider: "traffic-generator"}, {Provider: "operator-link"}, {Provider: "operator-device", Points: []string{"status"}},
		{Provider: "session-presence", Transport: "tcp"}, {Provider: "session-presence", Transport: "udp"},
	} {
		t.Run(binding.Provider+binding.Transport, func(t *testing.T) {
			def := validFlowDefinition()
			def.Liveness["opaque"] = binding
			if err := ValidateTopologyFlows(&def); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := ValidateTopologyFlows(&LabYAML{}); err != nil {
		t.Fatal(err)
	}
}

func TestTopologyFlowsReadDefinitionStrict(t *testing.T) {
	for _, extra := range []string{
		"\nflows:\n  - {id: f, source: rtac-1, target: missing, protocol: HTTP, port: 8080, cadence: burst, category: baseline, liveness_key: k}\nliveness: {k: {provider: unobserved}}\n",
		"\nflows: [{unknown: true}]\n",
		"\nliveness: {k: {provider: unobserved, unknown: true}}\n",
	} {
		t.Run(extra, func(t *testing.T) {
			root := t.TempDir()
			dir := writePackage(t, root, "example", "substation", nil)
			writeDefinition(t, filepath.Join(dir, "topology.yml"), validTopologyYAML+extra)
			if _, err := ReadDefinition(root, filepath.Join(dir, "package.yml")); err == nil {
				t.Fatal("ReadDefinition accepted invalid flows")
			}
		})
	}
}

func TestTopologyFlowsShippedInventory(t *testing.T) {
	definitions := filepath.Join("..", "..", "..", "lab-definitions")
	for _, id := range []string{"us-dnp3-substation", "eu-iec104-substation"} {
		t.Run(id, func(t *testing.T) {
			pkg, err := ReadDefinition(definitions, filepath.Join(definitions, "packages", id, "package.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if id == "us-dnp3-substation" {
				data, err := os.ReadFile("testdata/us-flow-graph.json")
				if err != nil {
					t.Fatal(err)
				}
				var frozen struct {
					Flows []TopologyFlow `json:"flows"`
				}
				if err := json.Unmarshal(data, &frozen); err != nil {
					t.Fatal(err)
				}
				if len(frozen.Flows) != 27 || !reflect.DeepEqual(pkg.Template.Flows, frozen.Flows) {
					t.Fatal("US flows differ from the frozen former TS inventory")
				}
				return
			}
			want := []TopologyFlow{
				{"cc-rtu", "cc-1", "rtu-1", "IEC104", 2404, "persistent", "baseline", "cc-rtu"},
				{"rtu-breaker", "rtu-1", "breaker-1", "Modbus", 502, "~500ms", "baseline", "rtu-breaker"},
				{"station-clock", "rtu-1", "gps-1", "HTTP", 8080, "~1s", "baseline", "station-clock"},
				{"attacker-rtu", "kali-1", "rtu-1", "IEC104", 2404, "manual", "scenario", "attacker-rtu"},
				{"rtu-rmu-1", "rtu-1", "rmu-1", "Modbus", 502, "~500ms", "baseline", "rtu-rmu-1"},
				{"rtu-rmu-2", "rtu-1", "rmu-2", "Modbus", 502, "~500ms", "baseline", "rtu-rmu-2"},
				{"rtu-oltc-1", "rtu-1", "oltc-1", "Modbus", 502, "~500ms", "baseline", "rtu-oltc-1"},
				{"rtu-plc-1", "rtu-1", "plc-1", "Modbus", 502, "~500ms", "baseline", "rtu-plc-1"},
				{"plc-oltc", "plc-1", "oltc-1", "Modbus", 502, "~100ms", "baseline", "plc-oltc"},
				{"cc-historian", "cc-1", "historian-1", "HTTP", 8081, "~1s", "baseline", "cc-historian"},
				{"enterprise-historian", "kali-1", "historian-1", "HTTP", 8080, "manual", "scenario", "enterprise-historian"},
				{"engineering-plc", "eng-ws-1", "plc-1", "HTTP", 8080, "manual", "scenario", "engineering-plc"},
			}
			if !reflect.DeepEqual(pkg.Template.Flows, want) {
				t.Fatalf("EU flows = %+v", pkg.Template.Flows)
			}
			wantLiveness := map[string]LivenessBinding{
				"cc-rtu":               {Provider: "operator-link"},
				"rtu-breaker":          {Provider: "operator-device", Points: []string{"feeder-breaker-position", "feeder-protection-trip"}},
				"station-clock":        {Provider: "unobserved"},
				"attacker-rtu":         {Provider: "session-presence", Transport: "tcp"},
				"rtu-rmu-1":            {Provider: "operator-device", Points: []string{"rmu-1-position", "rmu-1-fault-passage"}},
				"rtu-rmu-2":            {Provider: "operator-device", Points: []string{"rmu-2-position", "rmu-2-fault-passage"}},
				"rtu-oltc-1":           {Provider: "operator-device", Points: []string{"oltc-1-tap", "oltc-1-auto"}},
				"rtu-plc-1":            {Provider: "operator-device", Points: []string{"plc-1-running", "plc-1-input-valid"}},
				"plc-oltc":             {Provider: "unobserved"},
				"cc-historian":         {Provider: "unobserved"},
				"enterprise-historian": {Provider: "unobserved"},
				"engineering-plc":      {Provider: "unobserved"},
			}
			if !reflect.DeepEqual(pkg.Template.Liveness, wantLiveness) {
				t.Fatalf("EU liveness = %+v; want %+v", pkg.Template.Liveness, wantLiveness)
			}
		})
	}
}
