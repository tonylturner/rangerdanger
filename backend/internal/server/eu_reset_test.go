package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

var resetRevisions = map[string]uint64{"feeder-breaker": 10, "rmu-1": 11, "rmu-2": 12, "oltc-1": 13}

func deliveredState(t *testing.T, revisions map[string]uint64) string {
	t.Helper()
	devices := map[string]any{}
	for id, revision := range revisions {
		devices[id] = map[string]any{"delivered": true, "solved_revision": revision, "revision": revision}
	}
	state, err := json.Marshal(map[string]any{"schema": 2, "truth_stale": false, "devices": devices})
	if err != nil {
		t.Fatal(err)
	}
	return string(state)
}

func TestEUWorkshopReset(t *testing.T) {
	order := []string{
		"/process/api/process/actions", "/oltc-1/api/sim/actions",
		"/rmu-2/api/sim/actions", "/rmu-1/api/sim/actions",
		"/breaker-1/api/sim/actions", "/gps-1/api/command",
	}
	for _, failed := range append([]string{""}, order...) {
		t.Run("failure="+failed, func(t *testing.T) {
			fw, counters := fakeContaind(t, http.StatusOK, http.StatusOK)
			var mu sync.Mutex
			var requests []string
			var bodies []string
			polls := 0
			readyState := deliveredState(t, resetRevisions)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if r.Method == http.MethodGet && r.URL.Path == "/process/api/electrical" {
					polls++
					if polls == 1 {
						// Equal but pre-reset revisions must not satisfy the wait.
						fmt.Fprint(w, `{"schema":2,"devices":{"feeder-breaker":{"delivered":true,"solved_revision":9,"revision":9}}}`)
					} else {
						fmt.Fprint(w, readyState)
					}
					return
				}
				requests = append(requests, r.URL.Path)
				bodies = append(bodies, string(body))
				if r.URL.Path == failed {
					w.WriteHeader(http.StatusConflict)
					fmt.Fprint(w, `{"reason":"refused"}`)
					return
				}
				node := strings.Split(r.URL.Path, "/")[1]
				if node == "gps-1" {
					fmt.Fprint(w, `{"result":"executed","detail":"offset reset"}`)
					return
				}
				if node == "process" {
					fmt.Fprint(w, electricalFixture)
					return
				}
				id := node
				if node == "breaker-1" {
					id = "feeder-breaker"
				}
				fmt.Fprintf(w, `{"schema":2,"device_id":%q,"revision":%d}`, id, resetRevisions[id])
			}))
			t.Cleanup(upstream.Close)
			gen := multiDeviceGeneration(t, upstream.URL)
			gen = lifecycle.NewGeneration(gen.ID, gen.Package, gen.Manifest, containd.NewClient(fw.URL), gen.StartedAt)
			s := rangeServer(gen, "improved")
			s.orchestrator = &orchestrator.Orchestrator{}
			rec := callRoute(s, s.handleWorkshopReset, http.MethodPost, "/api/workshop/reset", "/api/workshop/reset", "")
			var result struct {
				Success bool          `json:"success"`
				Actions []resetAction `json:"actions"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			wantCode := http.StatusOK
			if failed != "" {
				wantCode = http.StatusBadGateway
			}
			if rec.Code != wantCode || result.Success != (failed == "") || len(result.Actions) != 8 {
				t.Fatalf("reset = %d %s", rec.Code, rec.Body)
			}
			if counters.commitCount != 1 || s.activeConfig != "weak" {
				t.Errorf("baseline commits = %d, active = %s", counters.commitCount, s.activeConfig)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(requests, order) {
				t.Fatalf("reset order = %v, want %v", requests, order)
			}
			for i, body := range bodies {
				want := `{"action":"reset"}`
				if i == 5 {
					want = `{"command":"reset_offset","source":"lab"}`
				}
				if body != want {
					t.Errorf("step %d wire = %s, want %s", i, body, want)
				}
			}
			if failed == "" && polls < 2 {
				t.Errorf("reset returned before post-reset delivery: polls=%d", polls)
			}
			for _, action := range result.Actions {
				if action.Detail == "" {
					t.Errorf("missing per-step result: %+v", action)
				}
			}
		})
	}
}

func TestResetDeliveryReady(t *testing.T) {
	good := deliveredState(t, resetRevisions)
	tests := []struct {
		name, state string
		ready       bool
		err         bool
	}{
		{"ready", good, true, false},
		{"later revision", strings.ReplaceAll(good, `"revision":10`, `"revision":14`), false, false},
		{"stale", strings.Replace(good, `"truth_stale":false`, `"truth_stale":true`, 1), false, false},
		{"missing", `{"schema":2,"truth_stale":false}`, false, false},
		{"old schema", strings.Replace(good, `"schema":2`, `"schema":1`, 1), false, true},
		{"missing truth quality", strings.Replace(good, `,"truth_stale":false`, ``, 1), false, false},
		{"not delivered", strings.Replace(good, `"delivered":true`, `"delivered":false`, 1), false, false},
		{"not solved", strings.Replace(good, `"solved_revision":10`, `"solved_revision":9`, 1), false, false},
		{"missing revision", strings.Replace(good, `"revision":10,`, ``, 1), false, false},
		{"malformed", `{"devices":`, false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ready, err := resetDeliveryReady([]byte(test.state), resetRevisions)
			if ready != test.ready || (err != nil) != test.err {
				t.Errorf("ready=%v err=%v", ready, err)
			}
		})
	}
	newer := map[string]uint64{"feeder-breaker": 14, "rmu-1": 15, "rmu-2": 16, "oltc-1": 17}
	if ready, err := resetDeliveryReady([]byte(deliveredState(t, newer)), resetRevisions); !ready || err != nil {
		t.Fatalf("newer consistent revisions = %v %v", ready, err)
	}
	if ready, err := resetDeliveryReady([]byte(good), newer); ready || err != nil {
		t.Fatalf("pre-reset revisions = %v %v", ready, err)
	}
}

func TestResetDeliveryDeadlineAndUpstreamFailure(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"not delivered", `{"schema":2,"truth_stale":false,"devices":{}}`, 200},
		{"upstream failure", `{"error":"unavailable"}`, 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, upstream := newUpstreamStub(t, map[string]stubResponse{
				"GET /process/api/electrical": {test.status, test.body},
			})
			gen := multiDeviceGeneration(t, upstream.URL)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			start := time.Now()
			if err := waitResetDelivery(ctx, gen, resetRevisions); err == nil {
				t.Fatal("wait reported success")
			}
			if time.Since(start) > time.Second {
				t.Fatal("wait ignored request deadline")
			}
		})
	}
}
