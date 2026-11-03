package lifecycle

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

func TestLifecycleWaitsForProtection(t *testing.T) {
	for _, state := range []string{"protecting", "degraded", "failed", "disabled"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t)
			seen, release := make(chan struct{}), make(chan struct{})
			var reads atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/health":
					json.NewEncoder(w).Encode(containd.HealthStatus{Status: "ok", Build: containd.MinimumVersion})
				case "/api/v1/config":
					w.Header().Set("X-Containd-Revision", "read")
					fmt.Fprint(w, `{}`)
				case "/api/v1/config/candidate", "/api/v1/config/commit":
					want := "read"
					if strings.HasSuffix(r.URL.Path, "/commit") {
						want = "staged"
					}
					got := r.Header.Get("X-Containd-If-Revision")
					if got != want {
						if got == "" {
							w.WriteHeader(428)
						} else {
							w.WriteHeader(412)
						}
						return
					}
					w.Header().Set("X-Containd-Revision", "staged")
					if strings.HasSuffix(r.URL.Path, "/commit") {
						fmt.Fprint(w, `{"status":"committed","applyId":"lifecycle-apply","runningRevision":"runtime-lifecycle","protection":{"state":"pending","subsystems":[]}}`)
					}
				case "/api/v1/dataplane/protection":
					n := reads.Add(1)
					if n == 1 {
						close(seen)
					}
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					observed := state
					if state == "degraded" && n > 1 {
						observed = "protecting"
					}
					revision := "runtime-lifecycle"
					status := containd.ProtectionStatus{
						ProtectingRevision: &revision, DesiredRevision: &revision, State: observed, Ready: observed == "protecting",
						Subsystems: []containd.ProtectionSubsystem{},
					}
					if state == "failed" {
						status.Subsystems = []containd.ProtectionSubsystem{{Required: true, State: "failed", Fault: &containd.ProtectionFault{Kind: "render", Reason: "fixture reason"}}}
					}
					json.NewEncoder(w).Encode(status)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			opts := h.options()
			opts.NewContaind = func(string) *containd.Client { return containd.NewClient(srv.URL) }
			m := h.manager(opts)
			if _, err := m.Request("pkg-a"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-seen:
			case <-time.After(3 * time.Second):
				t.Fatal("did not poll protection")
			}
			st := m.Status()
			if st.Phase != PhaseConfiguring || len(h.activated) != 0 {
				t.Errorf("activated before readiness: %+v", st)
			}
			close(release)
			m.wait()
			st = m.Status()
			wantPhase := PhaseReady
			if state == "failed" || state == "disabled" {
				wantPhase = PhaseFailed
			}
			wantState := state
			if state == "degraded" {
				wantState = "protecting"
				if reads.Load() != 2 {
					t.Fatalf("degraded did not poll: %d", reads.Load())
				}
			}
			if st.Phase != wantPhase || st.Containd == nil || st.Containd.ApplyID != "lifecycle-apply" || st.Containd.Protection != wantState {
				t.Fatalf("status = %+v", st)
			}
			if h.loadRecord().Containd.Protection != wantState {
				t.Fatal("receipt was not retained")
			}
			if wantPhase == PhaseFailed && (len(h.activated) != 0 || (state == "failed" && !strings.Contains(st.Error, "fixture reason"))) {
				t.Fatal("failed protection activated generation or lost reasons")
			}
		})
	}
}

func TestLifecycleRevisionConflictIsNotRetried(t *testing.T) {
	h := newHarness(t)
	h.firewall.candidateStatus = http.StatusPreconditionFailed
	m := h.manager(h.options())
	st := h.switchTo(m, "pkg-a")
	if st.Phase != PhaseFailed || st.Code != containd.ConfigConflictCode ||
		!strings.Contains(st.Error, containd.ConfigConflictMessage) || h.firewall.imports.Load() != 1 || len(h.activated) != 0 {
		t.Fatalf("status=%+v imports=%d", st, h.firewall.imports.Load())
	}
}
