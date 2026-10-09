package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

func TestSwitchFromNoneReachesReady(t *testing.T) {
	h := newHarness(t)
	m := h.manager(h.options())
	if st := m.Status(); st.Phase != PhaseNone || st.UpdatedAt != nil {
		t.Fatalf("initial status = %+v, want phase none without updated_at", st)
	}
	if got := m.ActivePackage(); got != "pkg-a" {
		t.Fatalf("ActivePackage() before any range = %q, want the default pkg-a", got)
	}

	st, err := m.Request("pkg-b")
	if err != nil {
		t.Fatal(err)
	}
	if st.Phase != PhasePreflight || st.Target != "pkg-b" {
		t.Fatalf("Request status = %+v, want preflight with target pkg-b", st)
	}
	m.wait()

	st = m.Status()
	if st.Phase != PhaseReady || st.Package != "pkg-b" || st.Target != "" || st.Generation != 1 || st.Error != "" {
		t.Fatalf("final status = %+v, want ready pkg-b generation 1", st)
	}
	wantCalls := []string{"config pkg-b", "images pkg-b", "down", "up pkg-b"}
	if got := h.compose.callLog(); !reflect.DeepEqual(got, wantCalls) {
		t.Errorf("compose calls = %q, want %q", got, wantCalls)
	}
	wantExecs := []string{proxyContainer + " nginx -t", proxyContainer + " nginx -s reload"}
	if got := h.engine.execLog(); !reflect.DeepEqual(got, wantExecs) {
		t.Errorf("proxy execs = %q, want %q", got, wantExecs)
	}
	if got := h.installedRoutes(); got != "# routes of pkg-b\n" {
		t.Errorf("installed routes = %q", got)
	}
	if got := h.firewall.imports.Load(); got != 1 {
		t.Errorf("policy imports = %d, want 1", got)
	}
	gen := <-h.activated
	if gen.ID != 1 || gen.Package.ID != "pkg-b" || gen.Containd().BaseURL != h.firewall.server.URL {
		t.Errorf("activated generation %d %s %s", gen.ID, gen.Package.ID, gen.Containd().BaseURL)
	}
	rec := h.loadRecord()
	if rec.Phase != PhaseReady || rec.Package != "pkg-b" || rec.Revision != 3 || rec.Generation != 1 ||
		rec.Mode != "release" || rec.Root != h.root || rec.Version != "test" {
		t.Errorf("record = %+v", rec)
	}
	if got := m.ActivePackage(); got != "pkg-b" {
		t.Errorf("ActivePackage() = %q, want pkg-b", got)
	}
	entered, release, _, ok := m.Enter("GET /x")
	if !ok || entered != gen {
		t.Fatal("Enter refused on a ready range")
	}
	release()
	if _, err := os.Stat(filepath.Join(h.routesDir, "."+RoutesFile+".tmp")); !os.IsNotExist(err) {
		t.Errorf("temporary routes file left behind: %v", err)
	}
}

func TestRequestDefaultsToActivePackage(t *testing.T) {
	h := newHarness(t)
	m := h.ready(h.options(), "pkg-b")
	if st := h.switchTo(m, ""); st.Phase != PhaseReady || st.Package != "pkg-b" || st.Generation != 2 {
		t.Fatalf("status = %+v, want pkg-b ready again as generation 2", st)
	}
}

func TestRequestRejectsUnknownAndBusy(t *testing.T) {
	h := newHarness(t)
	m := h.manager(h.options())
	if _, err := m.Request("pkg-z"); !errors.Is(err, ErrUnknownPackage) {
		t.Fatalf("unknown package error = %v", err)
	}
	block := make(chan struct{})
	h.compose.onDown = func() { <-block }
	if _, err := m.Request("pkg-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Request("pkg-b"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second request error = %v, want ErrBusy", err)
	}
	close(block)
	m.wait()
}

func TestPreflightFailureKeepsOldRangeServing(t *testing.T) {
	h := newHarness(t)
	m := h.ready(h.options(), "pkg-a")
	h.engine.missing["rd-test/plc:1"] = true

	st := h.switchTo(m, "pkg-b")
	if st.Phase != PhaseReady || st.Package != "pkg-a" || st.Target != "" || st.Generation != 1 {
		t.Fatalf("status = %+v, want pkg-a still ready", st)
	}
	if !strings.Contains(st.Error, "preflight pkg-b") || !strings.Contains(st.Error, "rd-test/plc:1") {
		t.Errorf("error = %q, want the preflight and the missing image", st.Error)
	}
	if got := h.compose.callLog(); !reflect.DeepEqual(got, []string{"config pkg-b", "images pkg-b"}) {
		t.Errorf("compose calls = %q, want config and images only", got)
	}
	if rec := h.loadRecord(); rec.Phase != PhaseReady || rec.Package != "pkg-a" {
		t.Errorf("record = %+v, want untouched", rec)
	}
	if _, release, _, ok := m.Enter("GET /x"); !ok {
		t.Error("old range stopped serving after a failed preflight")
	} else {
		release()
	}
}

func TestPreflightServesOldGenerationAndStoppingRefuses(t *testing.T) {
	h := newHarness(t)
	m := h.ready(h.options(), "pkg-a")
	inPreflight, resumePreflight := make(chan struct{}), make(chan struct{})
	inStopping, resumeStopping := make(chan struct{}), make(chan struct{})
	h.compose.onConfig = func() { close(inPreflight); <-resumePreflight }
	h.compose.onDown = func() { close(inStopping); <-resumeStopping }
	if _, err := m.Request("pkg-b"); err != nil {
		t.Fatal(err)
	}

	<-inPreflight
	if st := m.Status(); st.Phase != PhasePreflight || st.Package != "pkg-a" || st.Target != "pkg-b" {
		t.Errorf("status in preflight = %+v", st)
	}
	gen, release, _, ok := m.Enter("GET /during-preflight")
	if !ok || gen.ID != 1 {
		t.Fatal("preflight refused the old generation")
	}
	release()
	close(resumePreflight)

	<-inStopping
	if _, _, st, ok := m.Enter("GET /during-stopping"); ok || st.Phase != PhaseStopping || st.Package != "pkg-b" || st.Target != "" {
		t.Errorf("Enter during stopping = %v with status %+v, want refused, package pkg-b", ok, st)
	}
	close(resumeStopping)
	m.wait()
	if st := m.Status(); st.Phase != PhaseReady || st.Generation != 2 {
		t.Errorf("final status = %+v", st)
	}
}

func TestFailedUpTearsDownAndFails(t *testing.T) {
	h := newHarness(t)
	m := h.ready(h.options(), "pkg-a")
	h.compose.upErr = errors.New("dependency failed to start: container rd-test-pkg-b-plc is unhealthy")

	st := h.switchTo(m, "pkg-b")
	if st.Phase != PhaseFailed || st.Package != "pkg-b" || st.Generation != 2 {
		t.Fatalf("status = %+v, want pkg-b failed as generation 2", st)
	}
	if !strings.Contains(st.Error, "starting") || !strings.Contains(st.Error, "unhealthy") {
		t.Errorf("error = %q", st.Error)
	}
	want := []string{"config pkg-b", "images pkg-b", "down", "up pkg-b", "down"}
	if got := h.compose.callLog(); !reflect.DeepEqual(got, want) {
		t.Errorf("compose calls = %q, want %q", got, want)
	}
	if containers, networks, _ := h.engine.ProjectResources(t.Context(), "rangerdanger"); len(containers)+len(networks) > 0 {
		t.Errorf("residue after failure: %v %v", containers, networks)
	}
	if rec := h.loadRecord(); rec.Phase != PhaseFailed || rec.Error != st.Error {
		t.Errorf("record = %+v", rec)
	}
	if _, _, _, ok := m.Enter("GET /x"); ok {
		t.Error("a failed range admitted a request")
	}
	if got := h.firewall.imports.Load(); got != 1 {
		t.Errorf("policy imports = %d, want only the first switch's", got)
	}
}

func TestFailedNginxTestRestoresRoutes(t *testing.T) {
	h := newHarness(t)
	m := h.ready(h.options(), "pkg-a")
	h.engine.execCodes["nginx -t"] = 1

	st := h.switchTo(m, "pkg-b")
	if st.Phase != PhaseFailed || !strings.Contains(st.Error, "nginx -t") || !strings.Contains(st.Error, "exited 1") {
		t.Fatalf("status = %+v, want failed on nginx -t", st)
	}
	if got := h.installedRoutes(); got != "# routes of pkg-a\n" {
		t.Errorf("routes after rejected install = %q, want pkg-a's restored", got)
	}
	if got := h.engine.execLog(); !reflect.DeepEqual(got, []string{proxyContainer + " nginx -t"}) {
		t.Errorf("proxy execs = %q, want no reload after a failed test", got)
	}
	if got := h.compose.callLog(); got[len(got)-1] != "down" {
		t.Errorf("compose calls = %q, want a teardown last", got)
	}
}

func TestFailedNginxTestWithoutPreviousRemovesRoutes(t *testing.T) {
	h := newHarness(t)
	m := h.manager(h.options())
	h.engine.execCodes["nginx -t"] = 1
	if st := h.switchTo(m, "pkg-a"); st.Phase != PhaseFailed {
		t.Fatalf("status = %+v, want failed", st)
	}
	if _, err := os.Stat(filepath.Join(h.routesDir, RoutesFile)); !os.IsNotExist(err) {
		t.Errorf("rejected routes left installed: %v", err)
	}
}

func TestBarrierDrainsInFlightBeforeTeardown(t *testing.T) {
	h := newHarness(t)
	m := h.ready(h.options(), "pkg-a")
	gen, release, _, ok := m.Enter("GET /api/workshop/test-suite")
	if !ok {
		t.Fatal("Enter refused")
	}
	var handlerDone, workerDone atomic.Bool
	go func() {
		<-gen.Context().Done() // the barrier cancels the handler's context
		time.Sleep(20 * time.Millisecond)
		handlerDone.Store(true)
		release()
	}()
	gen.Go("traffic", func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		workerDone.Store(true)
	})
	var drained atomic.Bool
	h.compose.onDown = func() { drained.Store(handlerDone.Load() && workerDone.Load()) }

	if st := h.switchTo(m, "pkg-b"); st.Phase != PhaseReady || st.Generation != 2 {
		t.Fatalf("status = %+v, want generation 2 ready", st)
	}
	if !drained.Load() {
		t.Error("teardown ran before the handler and the worker drained")
	}
	if gen.Go("late", func(context.Context) { t.Error("a stopped generation ran a worker") }) {
		t.Error("Go on a stopped generation reported success")
	}
}

func TestBarrierTimeoutNamesStragglers(t *testing.T) {
	h := newHarness(t)
	opts := h.options()
	opts.Timeouts.Drain = 30 * time.Millisecond
	m := h.ready(opts, "pkg-a")
	_, release, _, ok := m.Enter("GET /api/stuck")
	if !ok {
		t.Fatal("Enter refused")
	}
	defer release()

	st := h.switchTo(m, "pkg-b")
	if st.Phase != PhaseFailed || !strings.Contains(st.Error, "did not drain") || !strings.Contains(st.Error, "GET /api/stuck") {
		t.Fatalf("status = %+v, want a drain failure naming the straggler", st)
	}
	if got := h.compose.callLog(); !reflect.DeepEqual(got, []string{"config pkg-b", "images pkg-b", "down"}) {
		t.Errorf("compose calls = %q, want only the failure teardown", got)
	}
}

func TestTeardownVerificationFailsClosed(t *testing.T) {
	h := newHarness(t)
	m := h.ready(h.options(), "pkg-a")
	h.compose.downLeaves = true
	st := h.switchTo(m, "pkg-b")
	if st.Phase != PhaseFailed || !strings.Contains(st.Error, "rd-test-stuck") {
		t.Fatalf("status = %+v, want failed naming the leftover container", st)
	}
	if got := h.compose.callLog(); contains(got, "up pkg-b") {
		t.Errorf("compose calls = %q: up ran after a failed teardown", got)
	}
}

func TestForeignContainerHoldingTargetNameFails(t *testing.T) {
	h := newHarness(t)
	m := h.manager(h.options())
	h.engine.foreign = []string{"rd-test-pkg-a-plc"}
	st := h.switchTo(m, "pkg-a")
	if st.Phase != PhaseFailed || !strings.Contains(st.Error, "rd-test-pkg-a-plc") {
		t.Fatalf("status = %+v, want failed naming the held container", st)
	}
	if got := h.compose.callLog(); contains(got, "up pkg-a") {
		t.Errorf("compose calls = %q: up ran over a held name", got)
	}
}

func TestCrashAtPersistedPhaseTearsDown(t *testing.T) {
	for _, phase := range []Phase{PhaseStopping, PhaseStarting, PhaseConfiguring} {
		t.Run(string(phase), func(t *testing.T) {
			h := newHarness(t)
			// The previous process died here, mid-transition, leaving a
			// half-started range behind.
			if err := h.store.Save(Record{Schema: RecordSchema, Generation: 4, Phase: phase, Package: "pkg-b",
				Revision: 3, Mode: "release", Root: h.root, Version: "old"}); err != nil {
				t.Fatal(err)
			}
			h.engine.project = testContainers("pkg-b")[:1]
			h.engine.networks = []string{"rangerdanger_field_net"}

			m := h.manager(h.options())
			if st := m.Status(); st.Phase != phase {
				t.Fatalf("status before Start = %+v, want the recorded phase", st)
			}
			gate := make(chan struct{})
			h.compose.onDown = func() { <-gate }
			m.Start()
			if _, err := m.Request("pkg-a"); !errors.Is(err, ErrBusy) {
				t.Errorf("Request during reconcile = %v, want ErrBusy", err)
			}
			close(gate)
			m.wait()

			st := m.Status()
			if st.Phase != PhaseFailed || st.Package != "pkg-b" || st.Generation != 4 || !strings.Contains(st.Error, "interrupted during "+string(phase)) {
				t.Fatalf("status = %+v, want failed interrupted", st)
			}
			if got := h.compose.callLog(); !reflect.DeepEqual(got, []string{"down"}) {
				t.Errorf("compose calls = %q, want one label-only down", got)
			}
			if rec := h.loadRecord(); rec.Phase != PhaseFailed || rec.Error != st.Error {
				t.Errorf("record = %+v", rec)
			}
			if got := h.firewall.imports.Load(); got != 0 {
				t.Errorf("reconcile imported a policy %d time(s)", got)
			}
		})
	}
}

func TestReconcileReadyAdoptsRunningRange(t *testing.T) {
	h := newHarness(t)
	h.ready(h.options(), "pkg-a")
	imports := h.firewall.imports.Load()

	// A new backend process finds the range it left running.
	m := h.manager(h.options())
	m.Start()
	m.wait()
	st := m.Status()
	if st.Phase != PhaseReady || st.Package != "pkg-a" || st.Generation != 1 || st.Error != "" {
		t.Fatalf("status = %+v, want pkg-a ready", st)
	}
	gen := <-h.activated
	if gen.ID != 1 || gen.Package.ID != "pkg-a" {
		t.Errorf("activated generation %d %s", gen.ID, gen.Package.ID)
	}
	if got := h.compose.callLog(); len(got) != 0 {
		t.Errorf("compose calls = %q, want none", got)
	}
	if got := h.firewall.imports.Load(); got != imports {
		t.Errorf("reconcile reseeded the policy")
	}
	if _, release, _, ok := m.Enter("GET /x"); !ok {
		t.Error("reconciled range refused a request")
	} else {
		release()
	}
}

func TestReconcileReadyMissing(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(h *harness, opts *Options)
		want   string
	}{
		{"container gone", func(h *harness, _ *Options) { h.engine.project = h.engine.project[:1] }, "rd-test-pkg-a-plc"},
		{"routes differ", func(h *harness, _ *Options) {
			mustWrite(h.t, filepath.Join(h.routesDir, RoutesFile), []byte("# edited\n"))
		}, "proxy routes"},
		{"mode changed", func(_ *harness, opts *Options) { opts.Mode = "source" }, "source mode"},
		{"package gone", func(h *harness, _ *Options) { h.catalog.Packages = h.catalog.Packages[1:] }, "not in the catalog"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.ready(h.options(), "pkg-a")
			opts := h.options()
			tt.break_(h, &opts)

			m := h.manager(opts)
			m.Start()
			m.wait()
			st := m.Status()
			if st.Phase != PhaseFailed || !strings.HasPrefix(st.Error, "range missing: ") || !strings.Contains(st.Error, tt.want) {
				t.Fatalf("status = %+v, want range missing mentioning %q", st, tt.want)
			}
			if rec := h.loadRecord(); rec.Phase != PhaseFailed {
				t.Errorf("record phase = %s", rec.Phase)
			}
			if got := h.compose.callLog(); len(got) != 0 {
				t.Errorf("compose calls = %q, want none", got)
			}
			select {
			case <-h.activated:
				t.Error("a missing range was activated")
			default:
			}
		})
	}
}

func TestStartWithoutRecordOrAfterFailureDoesNothing(t *testing.T) {
	h := newHarness(t)
	m := h.manager(h.options())
	m.Start()
	m.wait()
	if st := m.Status(); st.Phase != PhaseNone {
		t.Fatalf("status = %+v, want none", st)
	}
	if err := h.store.Save(Record{Schema: RecordSchema, Phase: PhaseFailed, Package: "pkg-a", Error: "earlier"}); err != nil {
		t.Fatal(err)
	}
	m = h.manager(h.options())
	m.Start()
	m.wait()
	if st := m.Status(); st.Phase != PhaseFailed || st.Error != "earlier" {
		t.Fatalf("status = %+v, want the recorded failure", st)
	}
	if got := h.compose.callLog(); len(got) != 0 {
		t.Errorf("compose calls = %q, want none", got)
	}
}

func TestUnreadableRecordTearsDown(t *testing.T) {
	h := newHarness(t)
	mustWrite(t, h.store.Path, []byte("{not json"))
	m := h.manager(h.options())
	if st := m.Status(); st.Phase != PhaseFailed {
		t.Fatalf("status = %+v, want failed", st)
	}
	m.Start()
	m.wait()
	st := m.Status()
	if st.Phase != PhaseFailed || !strings.Contains(st.Error, "runtime record unreadable") {
		t.Fatalf("status = %+v", st)
	}
	if got := h.compose.callLog(); !reflect.DeepEqual(got, []string{"down"}) {
		t.Errorf("compose calls = %q, want one down", got)
	}
	if rec := h.loadRecord(); rec.Phase != PhaseFailed {
		t.Errorf("record = %+v", rec)
	}
	if st := h.switchTo(m, ""); st.Phase != PhaseReady || st.Package != "pkg-a" {
		t.Errorf("switch after recovery = %+v, want the default package ready", st)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestPackageChecksFailPreflight(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(h *harness)
		want   string
	}{
		{"manifest invalid", func(h *harness) {
			man := testManifest("pkg-b", "http://10.0.0.9:8080")
			h.writeManifest(man)
		}, "manifest: "},
		{"manifest disagrees with topology", func(h *harness) {
			man := testManifest("pkg-b", h.firewall.server.URL)
			man.Services[1].Node = "plc-9"
			h.writeManifest(man)
		}, "manifest against topology: "},
		{"manifest disagrees with compose", func(h *harness) {
			model, err := normalizedModel(manifest.Dir(h.root, "pkg-b"))
			if err != nil {
				t.Fatal(err)
			}
			h.compose.models = map[string][]byte{"pkg-b": []byte(strings.Replace(string(model), "rd-test-pkg-b-plc", "rd-test-renamed", 1))}
		}, "manifest against compose release model: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			m := h.ready(h.options(), "pkg-a")
			tt.break_(h)

			st := h.switchTo(m, "pkg-b")
			if st.Phase != PhaseReady || st.Package != "pkg-a" || st.Generation != 1 {
				t.Fatalf("status = %+v, want pkg-a still ready", st)
			}
			if !strings.Contains(st.Error, "preflight pkg-b: "+tt.want) {
				t.Errorf("error = %q, want it to name %q", st.Error, tt.want)
			}
			if got := h.compose.callLog(); !reflect.DeepEqual(got, []string{"config pkg-b"}) {
				t.Errorf("compose calls = %q, want only config before the failed check", got)
			}
			if rec := h.loadRecord(); rec.Phase != PhaseReady || rec.Package != "pkg-a" {
				t.Errorf("record = %+v, want untouched", rec)
			}
		})
	}
}

func TestTeardownRemovesAndVerifiesVolumes(t *testing.T) {
	h := newHarness(t)
	m := h.ready(h.options(), "pkg-a")
	if st := h.switchTo(m, "pkg-b"); st.Phase != PhaseReady {
		t.Fatalf("status = %+v", st)
	}
	for _, container := range testContainers("pkg-a") {
		if h.engine.volumes[anonymousVolume(container)] {
			t.Errorf("volume of %s survived the switch", container)
		}
	}

	h.compose.keepVolume = true
	st := h.switchTo(m, "pkg-a")
	if st.Phase != PhaseFailed || !strings.Contains(st.Error, anonymousVolume("rd-test-pkg-b-plc")) || !strings.Contains(st.Error, "remain after down") {
		t.Fatalf("status = %+v, want failed naming the leftover volume", st)
	}
	if got := h.compose.callLog(); contains(got, "up pkg-a") {
		t.Errorf("compose calls = %q: up ran after a failed teardown", got)
	}
}
