package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

func TestEUVendorCollectorActiveProbesWithoutDenyLogs(t *testing.T) {
	for _, tc := range []struct {
		name, stderr string
		rc           int
		pass         bool
	}{
		{"custom default deny without logs", "", 124, true},
		{"explicit refusal without logs", "Connection refused", 1, true},
		{"tool error fails honestly", "timeout: missing tool", 127, false},
		{"open tuple fails", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			sources := map[string]int{}
			_, host := euProbeDockerFixture(t, tc.rc, tc.stderr, false, func(source string, options container.ExecOptions) {
				mu.Lock()
				defer mu.Unlock()
				sources[source]++
				if len(options.Cmd) != 6 || options.Cmd[2] != euTCPCommand {
					t.Errorf("wrong strict probe command: %+v", options.Cmd)
				}
			})
			t.Setenv("DOCKER_HOST", host)
			t.Setenv("DOCKER_API_VERSION", "1.47")
			t.Setenv("DOCKER_TLS_VERIFY", "")
			t.Setenv("DOCKER_CERT_PATH", "")
			ev := passingEUWorkshop(t, euWorkshopSlots[3])
			// Intentionally no log:true rules, no events endpoint and no
			// firewall.rule.hit records of any kind.
			policy := []byte(`{"firewall":{"defaultAction":"DENY","rules":[]}}`)
			hash, err := containd.FirewallHashFromBytes(policy)
			if err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected range write: %s", r.Method)
				}
				if r.URL.Path == "/api/v1/config" {
					w.Write(policy)
					return
				}
				if r.URL.Path == "/process/api/electrical" {
					ev.process.SolvedAt = time.Now().UTC()
					json.NewEncoder(w).Encode(ev.process)
					return
				}
				node := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/api/state")
				device := ev.devices[node]
				if device == nil {
					t.Errorf("unexpected endpoint (no deny-log reads allowed): %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				json.NewEncoder(w).Encode(device)
			}))
			defer upstream.Close()
			base := multiDeviceGeneration(t, upstream.URL)
			gen := lifecycle.NewGeneration(base.ID, base.Package, base.Manifest, containd.NewClient(upstream.URL), base.StartedAt)
			s := rangeServer(gen, "custom")
			s.lastAppliedHash = hash
			s.policyChangedAt = time.Now().Add(-time.Minute).UTC()
			s.orchestrator = orchestrator.New()
			t.Cleanup(func() { s.orchestrator.DockerClient().Close() })
			collected := s.collectEUWorkshop(t.Context(), gen, euWorkshopSlots[3])
			assertEUChecks(t, euWorkshopSlots[3], collected, tc.pass)
			if collected.negativeExpected != 6 || len(collected.negative) != 6 || sources["rangerdanger-kali"] != 2 || sources["rangerdanger-vendor-jump"] != 4 {
				t.Fatalf("wrong real-source matrix: %+v %+v", sources, collected.negative)
			}
			if tc.rc == 127 && !strings.Contains(euWorkshopNegative(collected).Detail, "probe tool exit 127") {
				t.Fatal("lost tool failure detail")
			}
		})
	}
}

func TestEUPositiveCollectorOnlyRequiredSourcesAndStrictQuality(t *testing.T) {
	for _, partialQuality := range []bool{false, true} {
		t.Run(fmt.Sprint(partialQuality), func(t *testing.T) {
			ev := passingEUWorkshop(t, euWorkshopSlots[1])
			cc, _ := json.Marshal(ev.cc)
			if partialQuality {
				cc = []byte(strings.ReplaceAll(string(cc), `"iv":false,`, ""))
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body any
				switch r.URL.Path {
				case "/oltc-1/api/audit":
					body = ev.conduits.audit
				case "/oltc-1/api/state":
					body = ev.conduits.device
				case "/historian-1/api/history":
					body = ev.conduits.history
				case "/cc-1/api/journal":
					body = ev.journal
				case "/cc-1/api/state":
					w.Write(cc)
					return
				default:
					t.Errorf("unconsumed evidence read: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				json.NewEncoder(w).Encode(body)
			}))
			defer upstream.Close()
			gen := multiDeviceGeneration(t, upstream.URL)
			s := rangeServer(gen, "custom")
			execs := 0
			s.execInContainer = func(ctx context.Context, source string, cmd []string, budget int) (string, string, int, error) {
				execs++
				body := "<html>PLC login</html>"
				if source == "rangerdanger-kali" {
					if cmd[3] != "10.10.10.50" {
						t.Error("historian read not bound to topology source")
					}
					points, _ := json.Marshal(ev.cc.Points)
					body = string(points)
				} else if source != "rangerdanger-eng-ws" || cmd[3] != "10.20.20.20" {
					t.Errorf("unexpected source: %s %v", source, cmd)
				}
				result, _ := json.Marshal(map[string]any{"status": 200, "body": body})
				return string(result), "", 0, nil
			}
			collected := s.collectEUWorkshopConduits(t.Context(), gen)
			if execs != 2 || len(collected.reads) != 2 || (collected.stateErr != nil) != partialQuality {
				t.Fatalf("execs=%d err=%v reads=%+v", execs, collected.stateErr, collected.reads)
			}
			for _, read := range collected.reads {
				if read.Status != "pass" {
					t.Fatal(read)
				}
			}
		})
	}
}
