package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

func TestEUF2IndicationContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(manifest.Dir(repoRoot(), euPackageID), "protocols", "points.json"))
	if err != nil {
		t.Fatal(err)
	}
	type source struct {
		Device  string `json:"device"`
		Table   string `json:"table"`
		Address int    `json:"address"`
		Format  string `json:"format"`
		Bit     int    `json:"bit"`
	}
	type point struct {
		ID          string  `json:"id"`
		IOA         int     `json:"ioa"`
		ASDU        string  `json:"asdu"`
		Kind        string  `json:"kind"`
		CyclicMS    int     `json:"cyclic_ms"`
		Deadband    float64 `json:"deadband"`
		Measurement bool    `json:"measurement"`
		Source      source  `json:"source"`
	}
	var config struct {
		Devices []struct {
			ID   string `json:"id"`
			Host string `json:"host"`
		} `json:"devices"`
		Points   []point `json:"points"`
		Commands []struct {
			Target source `json:"target"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Devices) != 6 || len(config.Points) != 42 || len(config.Commands) != 5 {
		t.Fatalf("sources/points/commands = %d/%d/%d", len(config.Devices), len(config.Points), len(config.Commands))
	}
	want := map[string]point{
		"f2-position":        {ID: "f2-position", IOA: 1501, ASDU: "M_DP_TB_1", Kind: "double-point", Source: source{"f2-breaker", "input", 0, "dp", 0}},
		"f2-protection-trip": {ID: "f2-protection-trip", IOA: 1502, ASDU: "M_SP_TB_1", Kind: "single-point", Source: source{"f2-breaker", "input", 1, "bit", 3}},
		"f2-current":         {ID: "f2-current", IOA: 2501, ASDU: "M_ME_NC_1", Kind: "float", CyclicMS: 10000, Deadband: 2, Measurement: true, Source: source{"f2-breaker", "input", 3, "f32", 0}},
		"f2-p-mw":            {ID: "f2-p-mw", IOA: 2502, ASDU: "M_ME_NC_1", Kind: "float", CyclicMS: 10000, Deadband: .05, Measurement: true, Source: source{"f2-breaker", "input", 11, "f32", 0}},
	}
	for _, p := range config.Points {
		if p.Source.Device != "f2-breaker" {
			continue
		}
		if expected, ok := want[p.ID]; !ok || p != expected {
			t.Fatalf("F2 indication contract changed: %+v", p)
		}
		delete(want, p.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing F2 indications: %+v", want)
	}
	for _, command := range config.Commands {
		if command.Target.Device == "f2-breaker" {
			t.Fatal("F2 must remain indication-only at IEC 104")
		}
	}
	found := false
	for _, device := range config.Devices {
		if device.ID == "f2-breaker" {
			found = device.Host == "10.40.40.24"
		}
	}
	if !found {
		t.Fatal("F2 Modbus poll identity/address missing")
	}
}

func TestEUF2CurriculumDestinationInventory(t *testing.T) {
	c := euGeneration(t, nil, nil).Package.Curriculum
	var got []string
	for _, slot := range c.Plan.Rules {
		if slot.Required != nil && slot.Required.ID == "rtu-to-feeder-modbus" {
			got = slot.Required.Destinations
		}
	}
	if len(got) != 5 {
		t.Fatalf("field destinations %v; want all five", got)
	}
	for _, host := range []string{"20", "21", "22", "23", "24"} {
		if !slices.Contains(got, "10.40.40."+host+"/32") {
			t.Fatalf("missing field endpoint .%s: %v", host, got)
		}
	}
}
