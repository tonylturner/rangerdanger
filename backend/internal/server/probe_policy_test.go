package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

func probeTestPolicy(defaultAction string) []byte {
	return []byte(fmt.Sprintf(`{"firewall":{"defaultAction":%q,"rules":[]}}`, defaultAction))
}

func useProbePolicy(t testing.TB, s *Server, document []byte) {
	t.Helper()
	firewall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/config" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(document)
	}))
	t.Cleanup(firewall.Close)

	fake := s.rng.(*fakeRange)
	previous := fake.gen
	fake.gen = lifecycle.NewGeneration(previous.ID, previous.Package, previous.Manifest, containd.NewClient(firewall.URL), previous.StartedAt)
}

func TestProbeSuccessFollowsRunningPolicy(t *testing.T) {
	s := activePackageServer(nil)
	gen := s.rng.(*fakeRange).gen
	weak, err := os.ReadFile(filepath.Join(repoRoot(), "lab-definitions/firewall/substation-weak.json"))
	if err != nil {
		t.Fatal(err)
	}
	partial, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, []string{"block-enterprise-to-ot"})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Label != "partial" {
		t.Fatalf("partial plan label = %q", partial.Label)
	}
	deferredAllow := false
	for _, rule := range partial.Rules {
		deferredAllow = deferredAllow || rule.Origin.Kind == "deferred" &&
			rule.Origin.Action == "block-enterprise-to-field" && rule.Rule.Action == "ALLOW"
	}
	if !deferredAllow {
		t.Fatal("partial plan does not retain the deferred enterprise-to-field allow")
	}
	actionIDs := make([]string, 0, len(gen.Package.Curriculum.Catalog))
	for _, action := range gen.Package.Curriculum.Catalog {
		actionIDs = append(actionIDs, action.ID)
	}
	hardened, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, actionIDs)
	if err != nil {
		t.Fatal(err)
	}
	if hardened.Label != "hardened" {
		t.Fatalf("full plan label = %q", hardened.Label)
	}

	tests := []struct {
		name       string
		policy     []byte
		source     string
		host       string
		port       int
		rc         int
		wantDetail string
	}{
		{
			name:   "Weak",
			policy: weak,
			source: "kali-1", host: "10.40.40.20", port: 502, rc: 0,
			wantDetail: "expected reachable by running policy, observed reachable",
		},
		{
			name:   "partial plan preserves deferred allow",
			policy: partial.Config,
			source: "kali-1", host: "10.40.40.20", port: 502, rc: 0,
			wantDetail: "expected reachable by running policy, observed reachable",
		},
		{
			name:   "Hardened",
			policy: hardened.Config,
			source: "kali-1", host: "10.40.40.20", port: 502, rc: 124,
			wantDetail: "expected blocked by running policy, observed blocked (timeout 3 s)",
		},
		{
			name:   "bridged route bypasses firewall evaluation",
			policy: probeTestPolicy("DENY"),
			source: "relay-1", host: "10.40.40.20", port: 502, rc: 0,
			wantDetail: "expected reachable by running policy, observed reachable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useProbePolicy(t, s, tt.policy)
			s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
				return "", "", tt.rc, nil
			}
			step := labs.ScenarioStep{
				Node: tt.source,
				Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{
					{Host: tt.host, Port: tt.port},
				}},
			}
			rows := s.executeProbe(context.Background(), s.rng.(*fakeRange).gen, step)
			if len(rows) != 1 || !rows[0].Success || rows[0].Detail != tt.wantDetail {
				t.Fatalf("rows = %+v, want success with detail %q", rows, tt.wantDetail)
			}
		})
	}
}

func TestProbeUnresolvableTargetFailsWithoutExecution(t *testing.T) {
	s := activePackageServer(nil)
	useProbePolicy(t, s, probeTestPolicy("ALLOW"))
	s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
		t.Fatal("probe executed for a target outside the topology")
		return "", "", 0, nil
	}
	step := labs.ScenarioStep{
		Node:   "kali-1",
		Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.40.40.99", Port: 502}}},
	}
	rows := s.executeProbe(context.Background(), s.rng.(*fakeRange).gen, step)
	if len(rows) != 1 || rows[0].Success || !strings.Contains(rows[0].Detail, "cannot resolve policy tuple") {
		t.Fatalf("rows = %+v", rows)
	}
}
