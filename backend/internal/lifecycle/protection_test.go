package lifecycle

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

func TestLifecycleWaitsForProtection(t *testing.T) {
	for _, state := range []string{"protecting", "degraded", "failed", "disabled"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t)
			seen, release := make(chan struct{}), make(chan struct{})
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
						fmt.Fprint(w, `{"status":"ok","applyId":"lifecycle-apply","protection":{"applyId":"lifecycle-apply","state":"pending"}}`)
					}
				case "/api/v1/dataplane/protection":
					close(seen)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					json.NewEncoder(w).Encode(containd.ProtectionStatus{
						ApplyID: "lifecycle-apply", InstalledApplyID: "lifecycle-apply", State: state,
						Components: []containd.ProtectionComponent{{Name: "nflog", State: "failed", Reason: "fixture reason"}},
					})
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
			if st.Phase != wantPhase || st.Containd == nil || st.Containd.ApplyID != "lifecycle-apply" || st.Containd.Protection != state {
				t.Fatalf("status = %+v", st)
			}
			if (st.Containd.Warning != "") != (state == "degraded") || h.loadRecord().Containd.Protection != state {
				t.Fatal("warning/receipt was not retained")
			}
			if wantPhase == PhaseFailed && (len(h.activated) != 0 || !strings.Contains(st.Error, "fixture reason")) {
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
