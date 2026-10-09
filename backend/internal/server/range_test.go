package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// fakeRange stands in for the lifecycle manager: one fixed generation,
// served while the status phase admits it.
type fakeRange struct {
	gen       *lifecycle.Generation
	status    lifecycle.Status
	active    string
	requested []string
	err       error
}

// servingRange is a ready range whose firewall is client.
func servingRange(client *containd.Client) *fakeRange {
	return &fakeRange{
		gen:    lifecycle.NewGeneration(1, labs.Package{ID: testPackageID}, &manifest.Manifest{Package: testPackageID}, client),
		status: lifecycle.Status{Generation: 1, Phase: lifecycle.PhaseReady, Package: testPackageID, Mode: manifest.ModeSource},
		active: testPackageID,
	}
}

func (f *fakeRange) Start()                   {}
func (f *fakeRange) Status() lifecycle.Status { return f.status }
func (f *fakeRange) ActivePackage() string    { return f.active }

func (f *fakeRange) Request(packageID string) (lifecycle.Status, error) {
	f.requested = append(f.requested, packageID)
	if f.err != nil {
		return f.status, f.err
	}
	f.status.Phase = lifecycle.PhasePreflight
	f.status.Target = packageID
	return f.status, nil
}

func (f *fakeRange) Enter(name string) (*lifecycle.Generation, func(), lifecycle.Status, bool) {
	if f.gen == nil || (f.status.Phase != lifecycle.PhaseReady && f.status.Phase != lifecycle.PhasePreflight) {
		return nil, nil, f.status, false
	}
	release, ok := f.gen.Enter(name)
	return f.gen, release, f.status, ok
}

func rangeRequest(s *Server, method, path, body string) (*httptest.ResponseRecorder, gin.H) {
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var parsed gin.H
	_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	return rec, parsed
}

func TestGetRangeReturnsStatus(t *testing.T) {
	s := routedServer(t)
	rec, body := rangeRequest(s, http.MethodGet, "/api/range", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/range = %d %s", rec.Code, rec.Body)
	}
	for _, key := range []string{"generation", "phase", "package", "target", "mode", "error", "updated_at"} {
		if _, ok := body[key]; !ok {
			t.Errorf("status body %v lacks %q", body, key)
		}
	}
	if body["phase"] != "ready" || body["package"] != testPackageID || body["mode"] != "source" {
		t.Errorf("status body = %v", body)
	}
}

func TestPostRange(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		err       error
		wantCode  int
		wantAsked []string
	}{
		{"named package", `{"package":"other-package"}`, nil, http.StatusAccepted, []string{"other-package"}},
		{"no body takes the active package", ``, nil, http.StatusAccepted, []string{""}},
		{"empty object", `{}`, nil, http.StatusAccepted, []string{""}},
		{"busy", `{"package":"other-package"}`, lifecycle.ErrBusy, http.StatusConflict, []string{"other-package"}},
		{"unknown package", `{"package":"nope"}`, fmt.Errorf("%w %q", lifecycle.ErrUnknownPackage, "nope"), http.StatusBadRequest, []string{"nope"}},
		{"bad JSON", `{"package":`, nil, http.StatusBadRequest, nil},
		{"unknown field", `{"pkg":"x"}`, nil, http.StatusBadRequest, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := routedServer(t)
			fake := s.rng.(*fakeRange)
			fake.err = tt.err
			rec, body := rangeRequest(s, http.MethodPost, "/api/range", tt.body)
			if rec.Code != tt.wantCode {
				t.Fatalf("POST /api/range = %d %s, want %d", rec.Code, rec.Body, tt.wantCode)
			}
			if fmt.Sprint(fake.requested) != fmt.Sprint(tt.wantAsked) {
				t.Errorf("requested = %q, want %q", fake.requested, tt.wantAsked)
			}
			if tt.wantCode == http.StatusAccepted && body["phase"] != "preflight" {
				t.Errorf("202 body = %v, want the preflight status", body)
			}
			if tt.wantCode == http.StatusConflict && body["phase"] != "ready" {
				t.Errorf("409 body = %v, want the running phase", body)
			}
		})
	}
}

func TestRangeBoundRoutesRefuseWhenNotServing(t *testing.T) {
	for _, phase := range []lifecycle.Phase{lifecycle.PhaseNone, lifecycle.PhaseStopping, lifecycle.PhaseStarting, lifecycle.PhaseConfiguring, lifecycle.PhaseFailed} {
		t.Run(string(phase), func(t *testing.T) {
			s := routedServer(t)
			s.rng.(*fakeRange).status.Phase = phase
			rec, body := rangeRequest(s, http.MethodGet, "/api/firewall/active", "")
			if rec.Code != http.StatusServiceUnavailable || body["error"] != "range not ready" || body["phase"] != string(phase) {
				t.Errorf("GET /api/firewall/active = %d %v", rec.Code, body)
			}
			// Platform routes keep serving.
			if rec, _ := rangeRequest(s, http.MethodGet, "/api/packages", ""); rec.Code != http.StatusOK {
				t.Errorf("GET /api/packages = %d during %s", rec.Code, phase)
			}
		})
	}
}

func TestRangeBoundRouteServesDuringPreflight(t *testing.T) {
	s := routedServer(t)
	s.rng.(*fakeRange).status.Phase = lifecycle.PhasePreflight
	if rec, _ := rangeRequest(s, http.MethodGet, "/api/firewall/active", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/firewall/active in preflight = %d, want the old range served", rec.Code)
	}
}

func TestRangeBoundContextEndsWithGeneration(t *testing.T) {
	fake := servingRange(nil)
	s := &Server{rng: fake}
	router := gin.New()
	entered := make(chan struct{})
	ended := make(chan error, 1)
	router.GET("/stream", s.rangeBound(), func(c *gin.Context) {
		if rangeOf(c) != fake.gen {
			t.Error("handler did not get the serving generation")
		}
		close(entered)
		<-c.Request.Context().Done()
		ended <- c.Request.Context().Err()
	})
	go router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/stream", nil))
	<-entered

	// The barrier waits for the handler, which returns once its context
	// ends with the generation.
	if err := fake.gen.Stop(2 * time.Second); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if err := <-ended; err != context.Canceled {
		t.Errorf("handler context error = %v, want context.Canceled", err)
	}
}

func TestActivateRangeResetsStateAndStartsObserver(t *testing.T) {
	s := &Server{activeConfig: "custom", policySource: "manual-custom", lastAppliedHash: "old",
		pcap: pcapState{Capturing: true}, traffic: trafficState{Generating: true}}
	gen := lifecycle.NewGeneration(2, labs.Package{ID: "p", FirewallConfigPath: "firewall/substation-improved.json"}, &manifest.Manifest{}, containd.NewClient("http://127.0.0.1:1"))
	s.activateRange(gen)
	s.activeConfigMu.RLock()
	activeConfig, source, hash := s.activeConfig, s.policySource, s.lastAppliedHash
	s.activeConfigMu.RUnlock()
	if activeConfig != "improved" || source != "" || hash != "" {
		t.Errorf("policy state = %q %q %q, want the package default and no apply", activeConfig, source, hash)
	}
	if s.pcap.Capturing || s.traffic.Generating {
		t.Error("capture or traffic state survived the new generation")
	}
	// The observer is a registered worker: stopping drains it.
	if err := gen.Stop(2 * time.Second); err != nil {
		t.Errorf("Stop = %v", err)
	}
}

func TestPackagesActiveFollowsTheRange(t *testing.T) {
	s := routedServer(t)
	// The record names a package other than RANGERDANGER_PACKAGE.
	s.rng.(*fakeRange).active = "other-package"
	rec := serve(s, http.MethodGet, "/api/packages")
	var got []packageSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range got {
		if pkg.Active != (pkg.ID == "other-package") {
			t.Errorf("package %s active = %v", pkg.ID, pkg.Active)
		}
	}
	if info := s.activePackage(); info.ID != "other-package" || info.TemplateID != "other-workshop" {
		t.Errorf("activePackage() = %+v, want other-package", info)
	}
}
