package models

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestScenarioJSONRoundTripAndTags(t *testing.T) {
	created := time.Date(2025, time.January, 2, 3, 4, 5, 678900000, time.UTC)
	updated := time.Date(2025, time.February, 3, 4, 5, 6, 789000000, time.UTC)
	in := &Scenario{ID: "scenario-1", PackageID: "package-1", Name: "Baseline", Summary: "Review access", Description: "Check flows", Order: "1.2", LabTemplateID: "template-1", Tags: `["segmentation"]`, Steps: `[{}]`, Nodes: `["rtac-1"]`, EstimatedMinutes: 15, Validator: "baseline", CreatedAt: created, UpdatedAt: updated}
	wantKeys := []string{"id", "package_id", "name", "summary", "description", "order", "lab_template_id", "tags", "steps", "nodes", "estimated_minutes", "validator", "created_at", "updated_at"}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	out := &Scenario{}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("Unmarshal(%s): %v", data, err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip = %#v, want %#v", out, in)
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
	sort.Strings(wantKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("JSON keys = %v, want %v", gotKeys, wantKeys)
	}

	for key, want := range map[string]string{
		"order":           "1.2",
		"lab_template_id": "template-1",
		"created_at":      created.Format(time.RFC3339Nano),
		"updated_at":      updated.Format(time.RFC3339Nano),
	} {
		var got string
		if err := json.Unmarshal(object[key], &got); err != nil {
			t.Fatalf("decode scenario %s: %v", key, err)
		}
		if got != want {
			t.Errorf("scenario %s = %q, want %q", key, got, want)
		}
	}
}
