package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

// receiptFirewall owns the complete wire contract rather than returning a
// successful empty commit. Revisions deliberately differ from application IDs.
func receiptFirewall(t *testing.T, state string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	commits := &atomic.Int32{}
	running := []byte(`{"interfaces":[{"name":"lan3","device":"eth8"}]}`)
	revision := "read-revision"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/config":
			w.Header().Set("X-Containd-Revision", revision)
			w.Write(running)
		case "/api/v1/config/candidate", "/api/v1/config/commit":
			got := r.Header.Get("X-Containd-If-Revision")
			if got == "" {
				http.Error(w, `{"code":"config_revision_required"}`, 428)
				return
			}
			if got != revision {
				http.Error(w, `{"code":"config_revision_mismatch"}`, 412)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/candidate") {
				running, _ = io.ReadAll(r.Body)
				revision = "candidate-revision"
				w.Header().Set("X-Containd-Revision", revision)
				return
			}
			commits.Add(1)
			revision = "committed-revision"
			w.Header().Set("X-Containd-Revision", revision)
			fmt.Fprint(w, `{"status":"ok","applyId":"route-apply","protection":{"applyId":"route-apply","state":"pending"}}`)
		case "/api/v1/dataplane/protection":
			id, observed := "route-apply", state
			if state == "superseded" {
				id, observed = "newer-apply", "protecting"
			}
			json.NewEncoder(w).Encode(containd.ProtectionStatus{
				ApplyID: id, InstalledApplyID: id, State: observed,
				Components: []containd.ProtectionComponent{{Name: "nflog", State: "failed", Reason: "consumer unavailable"}},
			})
		default:
			t.Errorf("unexpected containd request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, commits
}

func TestApplyRoutesProtectionReceipts(t *testing.T) {
	for _, route := range []string{"named", "plan"} {
		for _, state := range []string{"protecting", "degraded", "failed", "disabled", "superseded", "pending"} {
			t.Run(route+"/"+state, func(t *testing.T) {
				fw, commits := receiptFirewall(t, state)
				s := newTestServer(t, fw.URL)
				handler := s.handleFirewallApply
				var request any = map[string]string{"config": "improved"}
				if route == "plan" {
					handler, request = s.handleFirewallApplyPlan, planRequest(s)
				}
				if state == "pending" {
					original := handler
					handler = func(c *gin.Context) {
						ctx, cancel := context.WithTimeout(c.Request.Context(), 600*time.Millisecond)
						defer cancel()
						c.Request = c.Request.WithContext(ctx)
						original(c)
					}
				}
				rec, body := invoke(s, handler, "POST", "/apply", request)
				want := 502
				if state == "protecting" || state == "degraded" {
					want = 200
				}
				if rec.Code != want || commits.Load() != 1 {
					t.Fatalf("commits=%d response=%d %s", commits.Load(), rec.Code, rec.Body)
				}
				observed := state
				if state == "superseded" {
					observed = "protecting"
				}
				receipt, ok := body["containd"].(map[string]any)
				if !ok || receipt["applyId"] != "route-apply" || receipt["protection"] != observed {
					t.Fatalf("missing or inferred receipt: %s", rec.Body)
				}
				if (receipt["warning"] != nil) != (state == "degraded") {
					t.Fatalf("warning must appear only for degraded: %v", receipt)
				}
				if want != 200 {
					if body["observation"] == nil || s.activeConfig != "weak" || s.planProvenance != nil {
						t.Fatalf("failed apply published success or lost observation: %s", rec.Body)
					}
				}
				if last := s.rng.(*fakeRange).gen.Containd().LastApply(); last == nil || last.Protection != observed {
					t.Fatalf("range's last apply = %+v", last)
				}
			})
		}
	}
}

func TestApplyRoutesPreconditionFailures(t *testing.T) {
	for _, route := range []string{"named", "plan"} {
		for _, phase := range []string{"candidate", "commit"} {
			for _, status := range []int{400, 412, 428} {
				t.Run(fmt.Sprintf("%s/%s/%d", route, phase, status), func(t *testing.T) {
					candidateStatus, commitStatus := 200, 200
					if phase == "candidate" {
						candidateStatus = status
					} else {
						commitStatus = status
					}
					fw, counts := fakeContaind(t, candidateStatus, commitStatus)
					s := newTestServer(t, fw.URL)
					handler := s.handleFirewallApply
					var request any = map[string]string{"config": "improved"}
					if route == "plan" {
						handler, request = s.handleFirewallApplyPlan, planRequest(s)
					}
					rec, body := invoke(s, handler, "POST", "/apply", request)
					want := 502
					if status == 412 {
						want = 409
						if body["code"] != containd.ConfigConflictCode || body["error"] != containd.ConfigConflictMessage {
							t.Fatalf("conflict contract: %s", rec.Body)
						}
					} else if !strings.Contains(body["error"].(string), fmt.Sprint(status)) {
						t.Fatalf("upstream status lost: %s", rec.Body)
					}
					if rec.Code != want || counts.candidateCount != 1 || counts.commitCount > 1 || s.activeConfig != "weak" {
						t.Fatalf("retry or wrong response: counts=%+v response=%d %s", counts, rec.Code, rec.Body)
					}
				})
			}
		}
	}
}

func TestScenarioApplyReceiptAndConflict(t *testing.T) {
	for _, state := range []string{"protecting", "degraded", "failed", "conflict"} {
		t.Run(state, func(t *testing.T) {
			fw, _ := receiptFirewall(t, state)
			if state == "conflict" {
				fw, _ = fakeContaind(t, 412, 200)
			}
			s := rangeServer(euGeneration(t, containd.NewClient(fw.URL), nil), "weak")
			seedEUScenario(t, s)
			steps, err := json.Marshal([]labs.ScenarioStep{{
				ID: "apply", Title: "Apply policy", Action: &labs.StepAction{Type: "firewall", Config: "improved"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.db.Model(&models.Scenario{}).Where("id = ?", "eu-hardening-configurations").Update("steps", string(steps)).Error; err != nil {
				t.Fatal(err)
			}
			rec := callRoute(s, s.handleExecuteStep, "POST", "/api/scenarios/:id/steps/:stepIdx/execute",
				"/api/scenarios/eu-hardening-configurations/steps/0/execute", "")
			var result struct {
				StepExecutionResult
				Code string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			want := 200
			if state == "failed" {
				want = 502
			} else if state == "conflict" {
				want = 409
			}
			if rec.Code != want || result.Success != (want == 200) {
				t.Fatalf("response=%d %s", rec.Code, rec.Body)
			}
			if state == "conflict" {
				if result.Code != containd.ConfigConflictCode || result.Containd != nil {
					t.Fatalf("incorrect conflict receipt: %s", rec.Body)
				}
			} else if result.Containd == nil || result.Containd.ApplyID != "route-apply" || result.Containd.Protection != state ||
				len(result.Results) != 1 || result.Results[0].Containd == nil {
				t.Fatalf("missing scenario receipt: %s", rec.Body)
			}
		})
	}
}
