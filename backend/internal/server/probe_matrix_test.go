package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/containd/containdtest"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

func TestProbeMatrixRouteBoundary(t *testing.T) {
	s := routedServer(t)
	old := "/api/firewall/validation-report"
	if rec := serve(s, http.MethodPost, old); rec.Code != http.StatusNotFound {
		t.Fatalf("retired route: %d", rec.Code)
	}
	gen := usGeneration(nil)
	s = rangeServer(gen, "weak")
	for _, body := range []string{"{}", `{"rows":[]}`, " ", strings.Repeat("x", 10000)} {
		rec := callRoute(s, s.handleProbeMatrix, "POST", "/api/workshop/probe-matrix", "/api/workshop/probe-matrix", body)
		if rec.Code != 400 {
			t.Fatalf("body accepted: %d %s", rec.Code, rec.Body)
		}
	}
	s.engine = nil
	s.rng.(*fakeRange).status.Phase = lifecycle.PhaseStopping
	s.rng.(*fakeRange).gen = nil
	rec := callRoute(s, s.handleProbeMatrix, "POST", "/api/workshop/probe-matrix", "/api/workshop/probe-matrix", "")
	if rec.Code != 503 {
		t.Fatalf("range not ready: %d", rec.Code)
	}
}

func TestProbeMatrixMeasurementsAndIdentity(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		for _, mutation := range []string{"none", "live change", "hash unavailable", "recorded change", "same hash recommit", "missing provenance", "provenance mismatch", "cancel"} {
			t.Run(pkg+"/"+mutation, func(t *testing.T) {
				var gen *lifecycle.Generation
				if pkg == euPackageID {
					gen = euGeneration(t, nil, nil)
				} else {
					gen = usGeneration(nil)
				}
				compiled, err := labs.CompilePlan(gen.Package, gen.Package.Course.Features.SegmentationPlan.Model, []string{"block-enterprise-to-field"})
				if err != nil {
					t.Fatal(err)
				}
				hash, err := containd.FirewallHashFromBytes(compiled.Config)
				if err != nil {
					t.Fatal(err)
				}
				reads := 0
				upstream := containdtest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" || r.URL.Path != "/api/v1/config" {
						t.Errorf("unexpected write or call: %s %s", r.Method, r.URL.Path)
						w.WriteHeader(500)
						return
					}
					reads++
					if reads > 1 && mutation == "live change" {
						io.WriteString(w, `{"firewall":{"defaultAction":"DENY","rules":[]}}`)
						return
					}
					if mutation == "hash unavailable" {
						w.WriteHeader(500)
						return
					}
					w.Write(compiled.Config)
				}))
				defer upstream.Close()
				// Build the generation with the real HTTP hash reader, never a mocked hash.
				if pkg == euPackageID {
					gen = euGeneration(t, containd.NewClient(upstream.URL), nil)
				} else {
					gen = usGeneration(containd.NewClient(upstream.URL))
				}
				gen.StartedAt = time.Now().UTC().Add(-time.Hour)
				s := rangeServer(gen, "custom")
				s.policySource, s.lastAppliedHash, s.policyChangedAt = "plan", hash, time.Now().UTC().Add(-time.Minute)
				s.appliedPlan = &compiled
				s.planProvenance = &planProvenance{PackageID: pkg, Revision: gen.Package.Revision, PolicyHash: hash}
				if mutation == "missing provenance" {
					s.planProvenance = nil
				}
				if mutation == "provenance mismatch" {
					s.planProvenance.PolicyHash = hash + "-different"
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var once sync.Once
				result, err := s.measureProbeMatrix(ctx, gen, func(ctx context.Context, p validationProbe) validationRow {
					once.Do(func() {
						s.activeConfigMu.Lock()
						defer s.activeConfigMu.Unlock()
						switch mutation {
						case "recorded change":
							s.lastAppliedHash = hash + "-different"
						case "same hash recommit":
							s.policyChangedAt = s.policyChangedAt.Add(time.Second)
						case "cancel":
							cancel()
						}
					})
					return validationRow{validationProbe: p, Actual: p.Expected, Pass: true}
				})
				if err != nil {
					t.Fatal(err)
				}
				if result.PolicyStable != (mutation == "none") {
					t.Fatalf("identity: %+v", result)
				}
				if len(result.Rows) != len(compiled.Expectations) || result.PolicyHash == "" && mutation != "hash unavailable" {
					t.Fatalf("incomplete: %+v", result)
				}
				for i, row := range result.Rows {
					if row.ID != compiled.Expectations[i].ID {
						t.Fatalf("order: %+v", result.Rows)
					}
					if mutation == "cancel" || mutation == "hash unavailable" {
						if row.Observed != "untested" || row.Error == "" {
							t.Fatalf("cancelled: %+v", row)
						}
					} else {
						want := compiled.Expectations[i].PlanVerdict
						if mutation == "missing provenance" {
							want = compiled.Expectations[i].HardenedVerdict
						}
						if row.Observed != string(want) {
							t.Fatalf("wrong target: %+v", row)
						}
					}
				}
				raw, _ := json.Marshal(result)
				for _, key := range []string{"markdown", "summary", "acceptance", "planProof", "pcap"} {
					if strings.Contains(string(raw), key) {
						t.Fatalf("non-measurement field %s", key)
					}
				}
			})
		}
	}
}

func TestProbeMatrixUntestedAndToolErrorsSurvive(t *testing.T) {
	upstream := containdtest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{ "firewall": {} }`) }))
	defer upstream.Close()
	gen := usGeneration(containd.NewClient(upstream.URL))
	s := rangeServer(gen, "weak")
	// No Docker client: real strict engine returns explicit tool errors for every tuple.
	result, err := s.measureProbeMatrix(t.Context(), gen, func(ctx context.Context, p validationProbe) validationRow { return s.strictTCPProbe(ctx, nil, p) })
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range result.Rows {
		if row.Observed != "untested" || row.Error == "" {
			t.Fatal(row)
		}
	}
}

func TestProbeMatrixHTTPMeasurements(t *testing.T) {
	_, host := euProbeDockerFixture(t, 0, "", false, nil)
	t.Setenv("DOCKER_HOST", host)
	t.Setenv("DOCKER_API_VERSION", "1.47")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	upstream := containdtest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/config" {
			t.Errorf("unexpected write: %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"firewall":{"defaultAction":"DENY","rules":[]}}`)
	}))
	defer upstream.Close()
	hash, _ := containd.FirewallHashFromBytes([]byte(`{"firewall":{"defaultAction":"DENY","rules":[]}}`))
	for _, pkg := range []string{usPackageID, euPackageID} {
		var gen *lifecycle.Generation
		if pkg == euPackageID {
			gen = euGeneration(t, containd.NewClient(upstream.URL), nil)
		} else {
			gen = usGeneration(containd.NewClient(upstream.URL))
		}
		gen.StartedAt = time.Now().Add(-time.Hour)
		s := rangeServer(gen, "custom")
		s.orchestrator = orchestrator.New()
		t.Cleanup(func() { s.orchestrator.DockerClient().Close() })
		s.policySource, s.lastAppliedHash, s.policyChangedAt = "manual-custom", hash, time.Now().Add(-time.Minute)
		s.registerRoutes()
		rec := httptest.NewRecorder()
		s.engine.ServeHTTP(rec, httptest.NewRequest("POST", "/api/workshop/probe-matrix", nil))
		var result probeMatrixResult
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || !result.PolicyStable || result.PolicySource != "manual-custom" {
			t.Fatalf("%s: %d %s", pkg, rec.Code, rec.Body)
		}
		if len(result.Rows) != len(gen.Package.SegmentationPlan().Plan.Proof) {
			t.Fatal("incomplete universe")
		}
		for i, row := range result.Rows {
			if row.ID != gen.Package.SegmentationPlan().Plan.Proof[i].ID || row.Observed != "allow" || row.Error != "" {
				t.Fatalf("%+v", row)
			}
		}
	}
}
