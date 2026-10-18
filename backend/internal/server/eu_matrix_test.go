package server

import (
	"strings"
	"testing"
)

func TestEUMatrixOneDefinitionAndSourceIdentity(t *testing.T) {
	gen := euGeneration(t, nil, nil)
	report, err := euRecipe.resolveValidation(gen)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	negatives := map[validationProbe]bool{}
	for _, row := range report {
		if label, exists := labels[row.Src]; exists && label != row.SrcLabel {
			t.Fatalf("inconsistent source label: %+v", row)
		}
		labels[row.Src] = row.SrcLabel
		if row.Expected == "deny" {
			negatives[row] = true
		}
	}
	for _, tc := range []struct {
		slot  string
		count int
	}{
		{"eu-firewall-implementation", 19},
		{"eu-hardening-configurations", 1},
		{"eu-vendor-rdp-compromise", 7},
	} {
		matrix, err := euResolveMatrix(gen, euRecipe.validation, tc.slot)
		if err != nil || len(matrix) != tc.count {
			t.Fatalf("%s: %d rows, %v", tc.slot, len(matrix), err)
		}
		for _, row := range matrix {
			if !negatives[row] || !strings.Contains(row.Note, "topology source") {
				t.Fatalf("validator tuple/identity absent from report: %+v", row)
			}
		}
	}
	// Changing the authored source zone (and matching manifest interface)
	// changes the resolved identity without any hard-coded network override.
	for i := range gen.Package.Template.Nodes {
		if gen.Package.Template.Nodes[i].ID == "kali-1" {
			gen.Package.Template.Nodes[i].Networks = []string{"vendor_net"}
		}
	}
	for i := range gen.Manifest.Services {
		if gen.Manifest.Services[i].Node == "kali-1" {
			gen.Manifest.Services[i].Interfaces[0].Network = "vendor_net"
			gen.Manifest.Services[i].Interfaces[0].IPv4 = "10.20.20.99"
		}
	}
	matrix, err := euResolveMatrix(gen, euRecipe.validation, "eu-hardening-configurations")
	if err != nil || len(matrix) != 1 || !strings.Contains(matrix[0].Note, "10.20.20.99 on vendor_net") {
		t.Fatalf("source zone not derived: %+v %v", matrix, err)
	}
	again, err := euRecipe.resolveValidation(gen)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range again {
		if labels[row.Src] != row.SrcLabel {
			t.Fatal("source identity changed display label")
		}
	}
}

func TestEUMatrixMissingTopologyIdentityFails(t *testing.T) {
	for _, mode := range []string{"unknown node", "multiple networks", "missing interface", "no zone", "missing name"} {
		t.Run(mode, func(t *testing.T) {
			gen := euGeneration(t, nil, nil)
			for i := range gen.Package.Template.Nodes {
				node := &gen.Package.Template.Nodes[i]
				if node.ID != "kali-1" {
					continue
				}
				switch mode {
				case "unknown node":
					node.ID = "other"
				case "multiple networks":
					node.Networks = append(node.Networks, "station_net")
				case "missing interface":
					node.Networks = []string{"station_net"}
				case "no zone":
					for i := range gen.Package.Template.Networks {
						gen.Package.Template.Networks[i].Zone = ""
					}
				case "missing name":
					node.Name = ""
				}
			}
			if _, err := euResolveMatrix(gen, euRecipe.validation, "eu-hardening-configurations"); err == nil {
				t.Fatal("missing/ambiguous source identity accepted")
			}
		})
	}
}
