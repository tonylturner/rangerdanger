package models

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestModelsJSONRoundTripAndTags(t *testing.T) {
	created := time.Date(2025, time.January, 2, 3, 4, 5, 678900000, time.UTC)
	updated := time.Date(2025, time.February, 3, 4, 5, 6, 789000000, time.UTC)
	cases := []struct {
		name string
		in   any
		out  any
		keys []string
	}{
		{
			name: "lab template",
			in:   &LabTemplate{ID: "template-1", Name: "Substation", Description: "Practice", Topology: `{"nodes":[]}`, DefaultScenarios: `["baseline"]`, ComposeFile: "compose.yml", FirewallConfigPath: "weak.json", CreatedAt: created, UpdatedAt: updated},
			out:  &LabTemplate{},
			keys: []string{"id", "name", "description", "topology", "default_scenarios", "compose_file", "firewall_config_path", "created_at", "updated_at"},
		},
		{
			name: "lab instance",
			in:   &LabInstance{ID: "instance-1", TemplateID: "template-1", Name: "Range 1", Status: "running", DockerStackName: "range-1", RuntimeConfig: `{"mode":"safe"}`, CreatedAt: created, UpdatedAt: updated, Template: LabTemplate{ID: "template-1", Name: "Substation"}, Nodes: []NodeDefinition{{ID: "node-1", Name: "RTAC"}}},
			out:  &LabInstance{},
			keys: []string{"id", "template_id", "name", "status", "docker_stack_name", "runtime_config", "created_at", "updated_at", "template", "nodes"},
		},
		{
			name: "node definition",
			in:   &NodeDefinition{ID: "node-1", LabInstanceID: "instance-1", Type: "controller", Name: "RTAC", IP: "10.30.30.20", Status: "running", Metadata: `{"zone":"ot"}`, ContainerID: "docker-id", ContainerName: "rtac_sim", CreatedAt: created, UpdatedAt: updated},
			out:  &NodeDefinition{},
			keys: []string{"id", "lab_instance_id", "type", "name", "ip", "status", "metadata", "container_id", "container_name", "created_at", "updated_at"},
		},
		{
			name: "scenario",
			in:   &Scenario{ID: "scenario-1", Name: "Baseline", Summary: "Review access", Description: "Check flows", Order: "1.2", LabTemplateID: "template-1", Tags: `["segmentation"]`, Steps: `[{}]`, Nodes: `["rtac-1"]`, EstimatedMinutes: 15, CreatedAt: created, UpdatedAt: updated},
			out:  &Scenario{},
			keys: []string{"id", "name", "summary", "description", "order", "lab_template_id", "tags", "steps", "nodes", "estimated_minutes", "created_at", "updated_at"},
		},
		{
			name: "scenario run",
			in:   &ScenarioRun{ID: "run-1", ScenarioID: "scenario-1", LabInstanceID: "instance-1", Status: "complete", Events: `[{"step":1}]`, CreatedAt: created, UpdatedAt: updated},
			out:  &ScenarioRun{},
			keys: []string{"id", "scenario_id", "lab_instance_id", "status", "events", "created_at", "updated_at"},
		},
		{
			name: "telemetry point",
			in:   &TelemetryPoint{ID: "point-1", LabInstanceID: "instance-1", SourceNodeID: "node-1", Metric: "voltage", Value: "120.5", Timestamp: created},
			out:  &TelemetryPoint{},
			keys: []string{"id", "lab_instance_id", "source_node_id", "metric", "value", "timestamp"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal(): %v", err)
			}
			if err := json.Unmarshal(data, tc.out); err != nil {
				t.Fatalf("Unmarshal(%s): %v", data, err)
			}
			if !reflect.DeepEqual(tc.in, tc.out) {
				t.Errorf("round trip = %#v, want %#v", tc.out, tc.in)
			}

			var object map[string]json.RawMessage
			if err := json.Unmarshal(data, &object); err != nil {
				t.Fatalf("decode JSON object: %v", err)
			}
			gotKeys := make([]string, 0, len(object))
			for key := range object {
				gotKeys = append(gotKeys, key)
			}
			sort.Strings(gotKeys)
			wantKeys := append([]string(nil), tc.keys...)
			sort.Strings(wantKeys)
			if !reflect.DeepEqual(gotKeys, wantKeys) {
				t.Errorf("JSON keys = %v, want %v", gotKeys, wantKeys)
			}
		})
	}

	var scenarioJSON map[string]json.RawMessage
	data, err := json.Marshal(cases[3].in)
	if err != nil {
		t.Fatalf("Marshal(scenario): %v", err)
	}
	if err := json.Unmarshal(data, &scenarioJSON); err != nil {
		t.Fatalf("decode scenario JSON: %v", err)
	}
	for key, want := range map[string]string{
		"order":           "1.2",
		"lab_template_id": "template-1",
		"created_at":      created.Format(time.RFC3339Nano),
		"updated_at":      updated.Format(time.RFC3339Nano),
	} {
		var got string
		if err := json.Unmarshal(scenarioJSON[key], &got); err != nil {
			t.Fatalf("decode scenario %s: %v", key, err)
		}
		if got != want {
			t.Errorf("scenario %s = %q, want %q", key, got, want)
		}
	}

	var telemetryJSON map[string]json.RawMessage
	data, err = json.Marshal(cases[5].in)
	if err != nil {
		t.Fatalf("Marshal(telemetry): %v", err)
	}
	if err := json.Unmarshal(data, &telemetryJSON); err != nil {
		t.Fatalf("decode telemetry JSON: %v", err)
	}
	var gotTimestamp string
	if err := json.Unmarshal(telemetryJSON["timestamp"], &gotTimestamp); err != nil {
		t.Fatalf("decode telemetry timestamp: %v", err)
	}
	if want := created.Format(time.RFC3339Nano); gotTimestamp != want {
		t.Errorf("telemetry timestamp = %q, want %q", gotTimestamp, want)
	}

	var templateJSON map[string]json.RawMessage
	data, err = json.Marshal(cases[0].in)
	if err != nil {
		t.Fatalf("Marshal(template): %v", err)
	}
	if err := json.Unmarshal(data, &templateJSON); err != nil {
		t.Fatalf("decode template JSON: %v", err)
	}
	var topology, defaultScenarios string
	if err := json.Unmarshal(templateJSON["topology"], &topology); err != nil {
		t.Fatalf("decode topology string: %v", err)
	}
	if err := json.Unmarshal(templateJSON["default_scenarios"], &defaultScenarios); err != nil {
		t.Fatalf("decode default scenarios string: %v", err)
	}
	if topology != `{"nodes":[]}` || defaultScenarios != `["baseline"]` {
		t.Errorf("template embedded JSON fields = (%q, %q), want string values", topology, defaultScenarios)
	}
}

func TestLabInstanceEmptyRelationshipsJSON(t *testing.T) {
	data, err := json.Marshal(LabInstance{ID: "instance-1", Name: "Range 1"})
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("decode JSON object: %v", err)
	}
	templateJSON, exists := object["template"]
	if !exists {
		t.Fatalf("JSON omits zero-value template struct: %s", data)
	}
	var template LabTemplate
	if err := json.Unmarshal(templateJSON, &template); err != nil {
		t.Fatalf("decode template object: %v", err)
	}
	if !reflect.DeepEqual(template, LabTemplate{}) {
		t.Errorf("template JSON = %#v, want zero-value object", template)
	}
	if _, exists := object["nodes"]; exists {
		t.Errorf("JSON includes nil nodes: %s", data)
	}
}
