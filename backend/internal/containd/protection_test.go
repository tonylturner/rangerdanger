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
	"sync/atomic"
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
								Status: "committed", ApplyID: "ours", RunningRevision: "runtime-ours",
								Protection: readyProtection("runtime-ours"),
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

func readyProtection(revision string) ProtectionStatus {
	return ProtectionStatus{State: "protecting", Ready: true, ProtectingRevision: &revision, Subsystems: []ProtectionSubsystem{}}
}

func TestProtectionReadiness(t *testing.T) {
	ours, other := "runtime-ours", "opaque-other"
	fault := func(required bool, kind string) ProtectionStatus {
		return ProtectionStatus{State: "failed", Subsystems: []ProtectionSubsystem{{Required: required, State: "failed", Fault: &ProtectionFault{Kind: kind, Reason: "consumer diagnostic"}}}}
	}
	for _, tc := range []struct {
		name        string
		observation ProtectionStatus
		status      int
		wantError   string
		timeout     bool
	}{
		{name: "ready", observation: readyProtection(ours)},
		{name: "older protecting revision", observation: readyProtection(other)},
		{name: "degraded timeout", observation: ProtectionStatus{State: "degraded"}, timeout: true, wantError: "deadline"},
		{name: "failed without fault", observation: ProtectionStatus{State: "failed"}},
		{name: "superseded", observation: ProtectionStatus{State: "pending", DesiredRevision: &other}, wantError: "superseded"},
		{name: "disabled", observation: ProtectionStatus{State: "disabled"}, wantError: "disabled"},
		{name: "required config", observation: fault(true, "config"), wantError: "config fault"},
		{name: "required render", observation: fault(true, "render"), wantError: "render fault"},
		{name: "optional config", observation: fault(false, "config")},
		{name: "delivery", observation: fault(true, "delivery")},
		{name: "unknown fault", observation: fault(true, "unknown")},
		{name: "502", status: 502, observation: ProtectionStatus{State: "unknown"}},
		{name: "503", status: 503},
		{name: "pending timeout", observation: ProtectionStatus{State: "pending"}, timeout: true, wantError: "deadline"},
		{name: "unknown timeout", observation: ProtectionStatus{State: "unknown"}, timeout: true, wantError: "deadline"},
		{name: "bad status", status: 401, wantError: "returned 401"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertAuthHeader(t, r)
				if r.URL.Path != "/api/v1/dataplane/protection" {
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				n := calls.Add(1)
				if n > 1 {
					json.NewEncoder(w).Encode(readyProtection(ours))
					return
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				if tc.status == 503 {
					fmt.Fprint(w, `{"error":"runtime coordinator unavailable"}`)
					return
				}
				json.NewEncoder(w).Encode(tc.observation)
			}))
			defer srv.Close()
			budget := 3 * time.Second
			if tc.timeout {
				budget = 650 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			initial := ProtectionStatus{State: "pending", DesiredRevision: &ours}
			receipt, err := newTestClient(srv.URL).waitProtection(ctx, RuntimeApplyResponse{
				ApplyID: "informational-only", RunningRevision: ours, Protection: initial,
			})
			if receipt.ApplyID != "informational-only" {
				t.Fatalf("receipt=%+v", receipt)
			}
			if tc.wantError == "" {
				wantCalls := int32(2)
				if tc.name == "ready" {
					wantCalls = 1
				}
				if err != nil || receipt.Protection != "protecting" || calls.Load() != wantCalls {
					t.Fatalf("receipt=%+v calls=%d err=%v", receipt, calls.Load(), err)
				}
			} else {
				var failure *ProtectionError
				wantObservation := tc.observation
				if tc.name == "bad status" {
					wantObservation = initial
				}
				if !errors.As(err, &failure) || !strings.Contains(failure.Reason, tc.wantError) ||
					!reflect.DeepEqual(failure.Observation, wantObservation) || calls.Load() != 1 {
					t.Fatalf("lost last observation/reason: calls=%d err=%v", calls.Load(), err)
				}
			}
		})
	}
}

func TestProtectionAcceptsCommitReadinessWithoutApplyID(t *testing.T) {
	receipt, err := newTestClient("http://unused.invalid").waitProtection(t.Context(), RuntimeApplyResponse{
		RunningRevision: "runtime-ours", Protection: readyProtection("runtime-ours"),
	})
	if err != nil || receipt.Protection != "protecting" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
}

func TestImportCommitRequiresStatusAndRunningRevision(t *testing.T) {
	for _, body := range []string{`{"status":"committed","applyId":"informational"}`, `{"runningRevision":"ours"}`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Containd-Revision", "cas-revision")
				if strings.HasSuffix(r.URL.Path, "/commit") {
					fmt.Fprint(w, body)
				} else {
					fmt.Fprint(w, `{}`)
				}
			}))
			defer srv.Close()
			_, err := newTestClient(srv.URL).ImportConfig(t.Context(), []byte(`{"interfaces":[{"name":"lan3","zone":"lan3"}]}`))
			if err == nil || !strings.Contains(err.Error(), "missing status or runningRevision") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
