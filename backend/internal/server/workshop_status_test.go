package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

func TestWorkshopDockerStatus(t *testing.T) {
	for _, gen := range []*lifecycle.Generation{usGeneration(nil), euGeneration(t, nil, nil)} {
		t.Run(gen.Package.ID, func(t *testing.T) {
			for _, state := range []string{"running", "exited", "restarting", "paused", "created", "missing", "unavailable"} {
				t.Run(state, func(t *testing.T) {
					node := gen.Package.Template.Nodes[0]
					service, ok := gen.Manifest.ServiceByNode(node.ID)
					if !ok {
						t.Fatalf("no service for %s", node.ID)
					}
					daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodGet {
							t.Errorf("unexpected Docker write: %s", r.Method)
						}
						if state == "unavailable" {
							http.Error(w, "offline", 503)
							return
						}
						if r.URL.Query().Get("all") != "1" {
							t.Error("must include stopped containers")
						}
						if r.URL.Query().Get("filters") == "" {
							t.Error("must scope inventory")
						}
						items := []map[string]any{}
						if state != "missing" {
							items = append(items, map[string]any{"Names": []string{"/" + service.Container}, "State": state, "Labels": map[string]string{"com.docker.compose.project": manifest.RangeProject}})
						}
						// Same name in another project must not supply health.
						items = append(items, map[string]any{"Names": []string{"/" + service.Container}, "State": "running", "Labels": map[string]string{"com.docker.compose.project": manifest.PlatformProject}})
						json.NewEncoder(w).Encode(items)
					}))
					defer daemon.Close()
					t.Setenv("DOCKER_HOST", daemon.URL)
					t.Setenv("DOCKER_API_VERSION", "1.47")
					t.Setenv("DOCKER_TLS_VERIFY", "")
					t.Setenv("DOCKER_CERT_PATH", "")
					s := rangeServer(gen, "weak")
					s.orchestrator = orchestrator.New()
					defer s.orchestrator.DockerClient().Close()
					want := state
					if state == "created" || state == "unavailable" {
						want = "unknown"
					}
					if got := s.workshopNodeStatuses(t.Context(), gen)[node.ID]; got != want {
						t.Fatalf("status = %s, want %s", got, want)
					}
				})
			}
			s := rangeServer(gen, "weak")
			if got := s.workshopNodeStatuses(t.Context(), gen)[gen.Package.Template.Nodes[0].ID]; got != "unknown" {
				t.Fatalf("no Docker = %s", got)
			}
		})
	}
}
