package labs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
)

// An external test package can use both packages without adding the forbidden
// production dependency labs -> containd -> labs.
func TestFullPlanFirewallHashOracle(t *testing.T) {
	definitions := filepath.Join("..", "..", "..", "lab-definitions")
	for _, id := range []string{"us-dnp3-substation", "eu-iec104-substation"} {
		t.Run(id, func(t *testing.T) {
			pkg, err := labs.ReadDefinition(definitions, filepath.Join(definitions, "packages", id, "package.yml"))
			if err != nil {
				t.Fatal(err)
			}
			actions := make([]string, 0, len(pkg.Curriculum.Catalog))
			for _, action := range pkg.Curriculum.Catalog {
				actions = append(actions, action.ID)
			}
			plan, err := labs.CompilePlan(pkg, pkg.Curriculum, actions)
			if err != nil {
				t.Fatal(err)
			}
			reference, err := os.ReadFile(filepath.Join(definitions, pkg.Policies["improved"]))
			if err != nil {
				t.Fatal(err)
			}
			want, err := containd.FirewallHashFromBytes(reference)
			if err != nil {
				t.Fatal(err)
			}
			got, err := containd.FirewallHashFromBytes(plan.Config)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("full plan hash %s != improved hash %s", got, want)
			}
		})
	}
}
