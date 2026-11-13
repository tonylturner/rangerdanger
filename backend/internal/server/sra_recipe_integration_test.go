package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// TestSRAShippedPackage checks the SRA recipe against the shipped package.
func TestSRAShippedPackage(t *testing.T) {
	man, err := manifest.Load(repoRoot(), sraPackageID)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := labs.ReadDefinition(filepath.Join(repoRoot(), "lab-definitions"),
		filepath.Join(manifest.Dir(repoRoot(), sraPackageID), "package.yml"))
	if err != nil {
		t.Fatal(err)
	}
	gen := lifecycle.NewGeneration(1, pkg, man, nil, time.Now())
	if policy, err := defaultPolicy(gen); err != nil || policy != "weak" {
		t.Fatalf("default = %s %v", policy, err)
	}
	if pkg.SegmentationPlan() != nil {
		t.Fatal("SRA must not require the segmentation planner")
	}
	for i, node := range sraRecipe.resetRestartNodes {
		service, err := nodeService(gen, node)
		if err != nil || service.Key != []string{"sra_guacamole", "sra_guacd"}[i] {
			t.Fatalf("restart node %s = %+v %v", node, service, err)
		}
	}
	for _, cmd := range sraRecipe.resetCommands {
		if _, err := commandRoute(gen, cmd.device, cmd.command, "reset-script"); err != nil {
			t.Fatalf("reset tuple %s/%s: %v", cmd.device, cmd.command, err)
		}
	}
	if _, err := nodeIP(gen, sraRecipe.canary.toNode, sraRecipe.canary.network); err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"openplc-1", "eng-ws-1", "corp-ws-1"} {
		if _, err := nodeService(gen, absent); err == nil {
			t.Fatalf("unexpected node %s", absent)
		}
	}
}
