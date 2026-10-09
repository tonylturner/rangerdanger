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
			in:   &LabTemplate{ID: "template-1", PackageID: "package-1", Name: "Substation", Description: "Practice", Topology: `{"nodes":[]}`, CreatedAt: created, UpdatedAt: updated},
			out:  &LabTemplate{},
			keys: []string{"id", "package_id", "name", "description", "topology", "created_at", "updated_at"},
		},
		{
			name: "scenario",
			in:   &Scenario{ID: "scenario-1", PackageID: "package-1", Name: "Baseline", Summary: "Review access", Description: "Check flows", Order: "1.2", LabTemplateID: "template-1", Tags: `["segmentation"]`, Steps: `[{}]`, Nodes: `["rtac-1"]`, EstimatedMinutes: 15, Validator: "baseline", CreatedAt: created, UpdatedAt: updated},
			out:  &Scenario{},
			keys: []string{"id", "package_id", "name", "summary", "description", "order", "lab_template_id", "tags", "steps", "nodes", "estimated_minutes", "validator", "created_at", "updated_at"},
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
	data, err := json.Marshal(cases[1].in)
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

	var templateJSON map[string]json.RawMessage
	data, err = json.Marshal(cases[0].in)
	if err != nil {
		t.Fatalf("Marshal(template): %v", err)
	}
	if err := json.Unmarshal(data, &templateJSON); err != nil {
		t.Fatalf("decode template JSON: %v", err)
	}
	var topology string
	if err := json.Unmarshal(templateJSON["topology"], &topology); err != nil {
		t.Fatalf("decode topology string: %v", err)
	}
	if topology != `{"nodes":[]}` {
		t.Errorf("template embedded topology = %q, want string value", topology)
	}
}
