package containd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestImportRevisionChain(t *testing.T) {
	for _, phase := range []string{"candidate", "commit"} {
		for _, status := range []int{200, 400, 412, 428} {
			t.Run(fmt.Sprintf("%s/%d", phase, status), func(t *testing.T) {
				var calls []string
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls = append(calls, r.URL.Path)
					switch r.URL.Path {
					case "/api/v1/config":
						w.Header().Set("X-Containd-Revision", `"opaque-running"`)
						fmt.Fprint(w, `{}`)
					case "/api/v1/config/candidate", "/api/v1/config/commit":
						want := `"opaque-running"`
						if strings.HasSuffix(r.URL.Path, "/commit") {
							want = `"opaque-candidate"`
						}
						if got := r.Header.Get("X-Containd-If-Revision"); got != want {
							t.Errorf("revision = %q, want %q", got, want)
						}
						if strings.HasSuffix(r.URL.Path, "/"+phase) && status != 200 {
							w.WriteHeader(status)
							fmt.Fprint(w, `{"code":"injected-precondition-failure"}`)
							return
						}
						w.Header().Set("X-Containd-Revision", `"opaque-candidate"`)
						if strings.HasSuffix(r.URL.Path, "/commit") {
							json.NewEncoder(w).Encode(RuntimeApplyResponse{
								Status: "ok", ApplyID: "ours",
								Protection: ProtectionStatus{ApplyID: "ours", InstalledApplyID: "ours", State: "protecting"},
							})
						}
					default:
						t.Errorf("unexpected request %s", r.URL.Path)
					}
				}))
				defer srv.Close()
				client := newTestClient(srv.URL)
				applied, err := client.ImportConfig(t.Context(), []byte(`{"interfaces":[{"name":"lan3","zone":"lan3"}]}`))
				if status == 200 {
					if err != nil || applied.Receipt.ApplyID != "ours" || applied.Receipt.Protection != "protecting" {
						t.Fatalf("applied=%+v err=%v", applied, err)
					}
				} else {
					var upstream *ConfigWriteError
					if !errors.As(err, &upstream) || upstream.Status != status || !strings.Contains(upstream.Body, "injected-precondition") {
						t.Fatalf("lost typed upstream failure: %v", err)
					}
				}
				want := []string{"/api/v1/config", "/api/v1/config/candidate"}
				if phase == "commit" || status == 200 {
					want = append(want, "/api/v1/config/commit")
				}
				if !reflect.DeepEqual(calls, want) {
					t.Fatalf("retry or unexpected request: %v, want %v", calls, want)
				}
			})
		}
	}
}

func TestImportMissingRevisionFailsClosed(t *testing.T) {
	for _, missing := range []string{"running", "candidate"} {
		t.Run(missing, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if missing == "candidate" && r.URL.Path == "/api/v1/config" {
					w.Header().Set("X-Containd-Revision", "read")
				}
				fmt.Fprint(w, `{}`)
			}))
			defer srv.Close()
			_, err := newTestClient(srv.URL).ImportConfig(t.Context(), []byte(`{"interfaces":[{"name":"lan3","zone":"lan3"}]}`))
			wantCalls := 1
			if missing == "candidate" {
				wantCalls = 2
			}
			if err == nil || !strings.Contains(err.Error(), "missing X-Containd-Revision") || calls != wantCalls {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestProtectionReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, state, applyID, installed, wantError string
		timeout                                    bool
	}{
		{name: "protecting", state: "protecting", applyID: "ours", installed: "ours"},
		{name: "degraded", state: "degraded", applyID: "ours", installed: "ours"},
		{name: "failed", state: "failed", applyID: "ours", wantError: "failed"},
		{name: "disabled", state: "disabled", applyID: "ours", wantError: "disabled"},
		{name: "superseded", state: "pending", applyID: "newer", wantError: "superseded"},
		{name: "newer installed", state: "protecting", applyID: "ours", installed: "newer", wantError: "superseded"},
		{name: "pending timeout", state: "pending", applyID: "ours", timeout: true, wantError: "deadline"},
		{name: "not installed timeout", state: "protecting", applyID: "ours", timeout: true, wantError: "deadline"},
		{name: "unknown timeout", state: "unknown", applyID: "ours", timeout: true, wantError: "deadline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertAuthHeader(t, r)
				if r.URL.Path != "/api/v1/dataplane/protection" {
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				calls++
				json.NewEncoder(w).Encode(ProtectionStatus{ApplyID: tc.applyID, InstalledApplyID: tc.installed, State: tc.state,
					Components: []ProtectionComponent{{Name: "nflog", State: "failed", Reason: "test consumer failure"}}})
			}))
			defer srv.Close()
			budget := 2 * time.Second
			if tc.timeout {
				budget = 600 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			start := time.Now()
			receipt, err := newTestClient(srv.URL).waitProtection(ctx, RuntimeApplyResponse{
				ApplyID: "ours", Protection: ProtectionStatus{ApplyID: "ours", State: "pending"},
			})
			if receipt.ApplyID != "ours" || receipt.Protection != tc.state || calls != 1 {
				t.Fatalf("receipt=%+v calls=%d", receipt, calls)
			}
			if tc.wantError == "" {
				if err != nil || (receipt.Warning != "") != (tc.state == "degraded") {
					t.Fatalf("receipt=%+v err=%v", receipt, err)
				}
			} else {
				var failure *ProtectionError
				if !errors.As(err, &failure) || !strings.Contains(failure.Reason, tc.wantError) ||
					!strings.Contains(err.Error(), "test consumer failure") {
					t.Fatalf("lost observation/reason: %v", err)
				}
			}
			if time.Since(start) >= 1500*time.Millisecond {
				t.Fatal("readiness ignored parent deadline")
			}
		})
	}
}

func TestProtectionAcceptsCommitReadinessWithoutPoll(t *testing.T) {
	for _, state := range []string{"protecting", "degraded"} {
		t.Run(state, func(t *testing.T) {
			client := newTestClient("http://unused.invalid")
			receipt, err := client.waitProtection(t.Context(), RuntimeApplyResponse{
				ApplyID: "ours", Protection: ProtectionStatus{ApplyID: "ours", InstalledApplyID: "ours", State: state},
			})
			if err != nil || receipt.Protection != state {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
		})
	}
}
