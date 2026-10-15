package server

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

var updateEUFlowGraph = flag.Bool("update-eu-flow-graph", false, "regenerate the EU frontend graph fixture from the served package")

func TestWorkshopFlowsServingGeneration(t *testing.T) {
	for _, gen := range []*lifecycle.Generation{usGeneration(nil), euGeneration(t, nil, nil)} {
		t.Run(gen.Package.ID, func(t *testing.T) {
			s := rangeServer(gen, "weak")
			rec := callRoute(s, s.handleGetWorkshopGraph, http.MethodGet, "/api/workshop/graph", "/api/workshop/graph", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("graph = %d %s", rec.Code, rec.Body)
			}
			var body struct {
				Nodes      []graphNode                     `json:"nodes"`
				Flows      []labs.TopologyFlow             `json:"flows"`
				Liveness   map[string]labs.LivenessBinding `json:"liveness"`
				Networks   []labs.NetworkYAML              `json:"networks"`
				Operations struct {
					TrafficGeneration bool `json:"traffic_generation"`
				} `json:"operations"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body.Flows, gen.Package.Template.Flows) ||
				!reflect.DeepEqual(body.Liveness, gen.Package.Template.Liveness) ||
				!reflect.DeepEqual(body.Networks, gen.Package.Template.Networks) {
				t.Fatalf("graph inventory is not the serving generation: %s", rec.Body)
			}
			if body.Operations.TrafficGeneration != (gen.Package.ID == usPackageID) {
				t.Fatalf("wrong generation availability: %s", rec.Body)
			}
			fixtureName := "eu-flow-graph.json"
			if gen.Package.ID == usPackageID {
				fixtureName = "us-flow-graph.json"
			}
			fixturePath := filepath.Join("..", "labs", "testdata", fixtureName)
			if *updateEUFlowGraph && gen.Package.ID == euPackageID {
				var formatted bytes.Buffer
				if err := json.Indent(&formatted, rec.Body.Bytes(), "", "  "); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fixturePath, []byte(formatted.String()+"\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Nodes    []graphNode                     `json:"nodes"`
				Flows    []labs.TopologyFlow             `json:"flows"`
				Liveness map[string]labs.LivenessBinding `json:"liveness"`
				Networks []labs.NetworkYAML              `json:"networks"`
			}
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body.Flows, fixture.Flows) || !reflect.DeepEqual(body.Liveness, fixture.Liveness) || !reflect.DeepEqual(body.Networks, fixture.Networks) {
				t.Fatal("frontend fixture diverges from the served package inventory")
			}
			if gen.Package.ID == euPackageID && len(body.Nodes) != len(fixture.Nodes) {
				t.Fatalf("fixture has %d nodes; serving generation has %d", len(fixture.Nodes), len(body.Nodes))
			}
			for _, want := range fixture.Nodes {
				found := false
				for _, got := range body.Nodes {
					if got.ID == want.ID {
						found = got.Data.IP == want.Data.IP && got.Data.Zone == want.Data.Zone &&
							reflect.DeepEqual(got.Data.Networks, want.Data.Networks)
					}
				}
				if !found {
					t.Errorf("fixture endpoint %s no longer matches the served address or zone", want.ID)
				}
			}
		})
	}
}

func TestWorkshopFlowsEmptyCollections(t *testing.T) {
	s := routedServer(t)
	s.rng.(*fakeRange).gen.Package.Template = labs.LabYAML{}
	rec := serve(s, http.MethodGet, "/api/workshop/graph")
	if rec.Code != http.StatusOK {
		t.Fatalf("graph = %d %s", rec.Code, rec.Body)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"edges": "[]", "flows": "[]", "networks": "[]", "liveness": "{}"} {
		if string(body[key]) != want {
			t.Errorf("%s = %s, want %s", key, body[key], want)
		}
	}
}
