package server

import (
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
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
		matrix, err := referenceProof(gen, tc.slot)
		if err != nil || len(matrix) != tc.count {
			t.Fatalf("%s: %d rows, %v", tc.slot, len(matrix), err)
		}
		for _, row := range matrix {
			if !negatives[row] || !strings.Contains(row.Note, "topology source") {
				t.Fatalf("validator tuple/identity absent from report: %+v", row)
			}
		}
	}
	// Changing the authored primary address (and matching manifest interface)
	// changes the resolved identity without a hard-coded source override.
	for i := range gen.Package.Template.Nodes {
		if gen.Package.Template.Nodes[i].ID == "kali-1" {
			gen.Package.Template.Nodes[i].IP = "10.10.10.99"
		}
	}
	for i := range gen.Manifest.Services {
		if gen.Manifest.Services[i].Node == "kali-1" {
			gen.Manifest.Services[i].Interfaces[0].IPv4 = "10.10.10.99"
		}
	}
	matrix, err := referenceProof(gen, "eu-hardening-configurations")
	if err != nil || len(matrix) != 1 || !strings.Contains(matrix[0].Note, "10.10.10.99 on enterprise_net") {
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
	for _, mode := range []string{"unknown node", "ambiguous primary subnet", "missing interface", "no zone", "missing name"} {
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
				case "ambiguous primary subnet":
					for _, network := range gen.Package.Template.Networks {
						if network.ID == "enterprise_net" {
							network.ID = "overlap"
							gen.Package.Template.Networks = append(gen.Package.Template.Networks, network)
							break
						}
					}
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
			if _, err := referenceProof(gen, "eu-hardening-configurations"); err == nil {
				t.Fatal("missing/ambiguous source identity accepted")
			}
		})
	}
}

func referenceProof(gen *lifecycle.Generation, slot string) ([]validationProbe, error) {
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, nil)
	if err != nil {
		return nil, err
	}
	return resolveProof(gen, compiled.Expectations, slot, false)
}

func TestEUMatrixPrimaryIPSelectsMultihomedSource(t *testing.T) {
	gen := euGeneration(t, nil, nil)
	for i := range gen.Package.Template.Nodes {
		if gen.Package.Template.Nodes[i].ID == "kali-1" {
			gen.Package.Template.Nodes[i].Networks = append(gen.Package.Template.Nodes[i].Networks, "station_net")
		}
	}
	matrix, err := referenceProof(gen, "eu-hardening-configurations")
	if err != nil || len(matrix) != 1 || !strings.Contains(matrix[0].Note, "10.10.10.50 on enterprise_net") {
		t.Fatalf("multihomed primary identity: %+v %v", matrix, err)
	}
}
