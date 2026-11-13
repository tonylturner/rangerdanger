package server

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

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func firewallWaitTestServer(t *testing.T, pkg, active string, hashMatches bool) *Server {
	t.Helper()

	action := "DENY"
	if active == "weak" {
		action = "ALLOW"
	}
	wantedFirewall := map[string]any{"defaultAction": action, "rules": []any{}}
	observedFirewall := wantedFirewall
	if !hashMatches {
		other := "ALLOW"
		if action == other {
			other = "DENY"
		}
		observedFirewall = map[string]any{"defaultAction": other, "rules": []any{}}
	}
	return firewallWaitPolicyTestServer(t, pkg, active, wantedFirewall, observedFirewall)
}

func firewallWaitPolicyTestServer(t *testing.T, pkg, active string, wantedFirewall, observedFirewall map[string]any) *Server {
	t.Helper()
	configBytes, err := json.Marshal(map[string]any{"firewall": wantedFirewall})
	if err != nil {
		t.Fatalf("marshal firewall config: %v", err)
	}
	wantHash, err := containd.FirewallHashFromBytes(configBytes)
	if err != nil {
		t.Fatalf("hash firewall config: %v", err)
	}
	containdServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/config" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"firewall": observedFirewall})
	}))
	t.Cleanup(containdServer.Close)

	client := containd.NewClient(containdServer.URL)
	gen := usGeneration(client)
	if pkg == euPackageID {
		gen = euGeneration(t, client, nil)
	}
	s := rangeServer(gen, active)
	s.lastAppliedHash = wantHash
	return s
}

func setFirewallWaitBudget(t *testing.T, budget time.Duration) {
	t.Helper()
	previous := firewallWaitBudget
	firewallWaitBudget = budget
	t.Cleanup(func() { firewallWaitBudget = previous })
}

type canaryObservation struct {
	stdout, stderr string
	rc             int
	err            error
}

func scriptedFirewallCanary(t *testing.T, s *Server, observations []canaryObservation) *int {
	t.Helper()
	calls := 0
	port := "502"
	if s.rng.(*fakeRange).gen.Package.ID == euPackageID {
		port = "2404"
	}
	wantCommand := []string{"sh", "-c", euTCPCommand, "probe", "10.30.30.20", port}
	s.execInContainer = func(_ context.Context, container string, cmd []string, timeout int) (string, string, int, error) {
		if container != "rangerdanger-kali" || timeout != 5 || !reflect.DeepEqual(cmd, wantCommand) {
			t.Errorf("exec arguments: container=%q timeout=%d cmd=%q", container, timeout, cmd)
		}
		index := calls
		calls++
		if index >= len(observations) {
			index = len(observations) - 1
		}
		observation := observations[index]
		return observation.stdout, observation.stderr, observation.rc, observation.err
	}
	return &calls
}

func TestWaitForFirewallPolicyCanary(t *testing.T) {
	tests := []struct {
		name         string
		active       string
		observations []canaryObservation
	}{
		{name: "weak waits for allow", active: "weak", observations: []canaryObservation{{rc: 124}, {rc: 124}, {rc: 0}}},
		{name: "improved waits for deny", active: "improved", observations: []canaryObservation{{rc: 0}, {rc: 1, stderr: "Connection refused"}, {rc: 124}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := firewallWaitTestServer(t, usPackageID, tt.active, true)
			calls := scriptedFirewallCanary(t, s, tt.observations)
			if err := s.waitForFirewallPolicy(context.Background(), s.rng.(*fakeRange).gen); err != nil {
				t.Fatalf("waitForFirewallPolicy() error = %v", err)
			}
			if *calls != 3 {
				t.Errorf("canary calls = %d, want 3", *calls)
			}
		})
	}
}

func TestWaitForFirewallPolicyCanaryTimeout(t *testing.T) {
	setFirewallWaitBudget(t, 25*time.Millisecond)
	s := firewallWaitTestServer(t, usPackageID, "improved", true)
	calls := scriptedFirewallCanary(t, s, []canaryObservation{{rc: 0}})
	started := time.Now()
	err := s.waitForFirewallPolicy(context.Background(), s.rng.(*fakeRange).gen)
	if err == nil || !strings.Contains(err.Error(), "still connected") {
		t.Fatalf("waitForFirewallPolicy() error = %v, want still-allow timeout", err)
	}
	if *calls == 0 {
		t.Fatal("canary was never called")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("timeout took %s, want within shrunk budget", elapsed)
	}
}

func TestWaitForFirewallPolicyStrictCanary(t *testing.T) {
	setFirewallWaitBudget(t, 25*time.Millisecond)
	for _, tc := range []struct {
		name        string
		observation canaryObservation
		verdict     string
	}{
		{name: "connected", observation: canaryObservation{rc: 0}, verdict: "allow"},
		{name: "refused", observation: canaryObservation{rc: 1, stderr: "Connection refused"}, verdict: "allow"},
		{name: "timeout status", observation: canaryObservation{rc: 124}, verdict: "deny"},
		{name: "timeout diagnostic", observation: canaryObservation{rc: 1, stderr: "Connection timed out"}, verdict: "deny"},
		{name: "exec error", observation: canaryObservation{rc: 0, err: errors.New("inspect failed")}, verdict: "skipped"},
		{name: "exec timeout", observation: canaryObservation{rc: 124, err: context.DeadlineExceeded}, verdict: "skipped"},
		{name: "missing tool", observation: canaryObservation{rc: 127, stderr: "timeout: not found"}, verdict: "skipped"},
		{name: "silent failure", observation: canaryObservation{rc: 1}, verdict: "skipped"},
		{name: "killed", observation: canaryObservation{rc: 137}, verdict: "skipped"},
		{name: "unnormalized signal", observation: canaryObservation{rc: 143}, verdict: "skipped"},
	} {
		for _, active := range []string{"weak", "improved"} {
			t.Run(tc.name+"/"+active, func(t *testing.T) {
				s := firewallWaitTestServer(t, usPackageID, active, true)
				calls := scriptedFirewallCanary(t, s, []canaryObservation{tc.observation})
				err := s.waitForFirewallPolicy(t.Context(), s.rng.(*fakeRange).gen)
				expected := "allow"
				if active == "improved" {
					expected = "deny"
				}
				if (err == nil) != (tc.verdict == expected) || *calls != 1 {
					t.Fatalf("err=%v calls=%d, expected %s observed %s", err, *calls, expected, tc.verdict)
				}
				if tc.verdict == "skipped" && !strings.Contains(err.Error(), "unavailable") {
					t.Fatalf("untested canary was not reported unavailable: %v", err)
				}
			})
		}
	}
}

func TestWaitForFirewallPolicyCustomCanaryUsesCommittedPolicy(t *testing.T) {
	setFirewallWaitBudget(t, 25*time.Millisecond)
	for _, pkg := range []string{usPackageID, euPackageID} {
		for _, tc := range []struct {
			name           string
			applied        bool
			committedAllow bool
			rc             int
			pass           bool
		}{
			{name: "manual allow differs from deny request and reconciles", committedAllow: true, rc: 0, pass: true},
			{name: "manual allow differs from deny request but dataplane denies", committedAllow: true, rc: 124},
			{name: "manual deny differs from allow request and reconciles", rc: 124, pass: true},
			{name: "manual deny differs from allow request but dataplane allows", rc: 0},
			{name: "applied deferred plan allows", applied: true, committedAllow: true, rc: 0, pass: true},
			{name: "applied deferred plan rejects timeout", applied: true, committedAllow: true, rc: 124},
			{name: "applied full plan denies", applied: true, rc: 124, pass: true},
			{name: "applied full plan rejects connection", applied: true, rc: 0},
		} {
			t.Run(pkg+"/"+tc.name, func(t *testing.T) {
				port := 502
				if pkg == euPackageID {
					port = 2404
				}
				// Opposite defaults ensure the ordered rule, not just the
				// default action, determines the committed tuple's verdict.
				action, defaultAction := "DENY", "ALLOW"
				if tc.committedAllow {
					action, defaultAction = "ALLOW", "DENY"
				}
				firewall := map[string]any{"defaultAction": defaultAction, "rules": []any{map[string]any{
					"id": "manual-canary", "sourceZones": []string{"wan"}, "destZones": []string{"lan1"},
					"protocols": []any{map[string]any{"name": "tcp", "port": fmt.Sprint(port)}}, "action": action,
				}}}
				s := firewallWaitPolicyTestServer(t, pkg, "custom", firewall, firewall)
				gen := s.rng.(*fakeRange).gen
				s.policySource = "manual-custom"
				var all []string
				for _, action := range gen.Package.SegmentationPlan().Catalog {
					all = append(all, action.ID)
				}
				var requestIDs []string
				if tc.committedAllow {
					requestIDs = all
				}
				ctx := context.WithValue(t.Context(), changeRequestKey{}, compiledChangeRequest{compileProbePlan(t, gen, requestIDs)})
				if tc.applied {
					var ids []string
					if !tc.committedAllow {
						ids = all
					}
					useAppliedProbePlan(t, s, compileProbePlan(t, gen, ids))
					var document struct {
						Firewall map[string]any `json:"firewall"`
					}
					if err := json.Unmarshal(s.appliedPlan.Config, &document); err != nil {
						t.Fatal(err)
					}
					// Serve the actual applied plan as the committed policy.
					s = firewallWaitPolicyTestServer(t, pkg, "custom", document.Firewall, document.Firewall)
					gen = s.rng.(*fakeRange).gen
					useAppliedProbePlan(t, s, compileProbePlan(t, gen, ids))
				}
				calls := scriptedFirewallCanary(t, s, []canaryObservation{{rc: tc.rc}})
				err := s.waitForFirewallPolicy(ctx, gen)
				if (err == nil) != tc.pass || *calls != 1 {
					t.Fatalf("canary err=%v calls=%d, want pass=%t", err, *calls, tc.pass)
				}
				step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.30.30.20", Port: port}}}}
				s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
					return "", "", tc.rc, nil
				}
				rows := s.executeProbe(ctx, gen, step)
				probePass := tc.pass
				if !tc.applied {
					probePass = !tc.pass
				}
				if len(rows) != 1 || rows[0].Success != probePass {
					t.Fatalf("scenario probe should retain its separate correctness oracle: %+v", rows)
				}
			})
		}
	}
}

func TestWaitForFirewallPolicyUnavailableCanaryExpectation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		firewall map[string]any
		badTuple bool
	}{
		{name: "unsupported default", firewall: map[string]any{"defaultAction": "UNKNOWN", "rules": []any{}}},
		{name: "unsupported selector", firewall: map[string]any{"defaultAction": "DENY", "rules": []any{map[string]any{"action": "ALLOW", "schedule": "work-hours"}}}},
		{name: "unresolvable tuple", firewall: map[string]any{"defaultAction": "DENY", "rules": []any{}}, badTuple: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := firewallWaitPolicyTestServer(t, usPackageID, "custom", tc.firewall, tc.firewall)
			gen := s.rng.(*fakeRange).gen
			if tc.badTuple {
				gen.Package.Template.Networks = nil
			}
			calls := scriptedFirewallCanary(t, s, []canaryObservation{{rc: 124}})
			err := s.waitForFirewallPolicy(t.Context(), gen)
			if err == nil || !strings.Contains(err.Error(), "expectation unavailable") || *calls != 0 {
				t.Fatalf("err=%v calls=%d, want explicit unavailable expectation without exec", err, *calls)
			}
		})
	}
}

func TestFirewallCanaryExpectationBridged(t *testing.T) {
	canary := canaryProbe{fromNode: "relay-1", toNode: "recloser-1", network: "field_net", port: 502}
	got, err := firewallCanaryExpectation(usGeneration(nil), canary, "10.40.40.21",
		[]byte(`{"firewall":{"defaultAction":"DENY","rules":[]}}`))
	if err != nil || got != "allow" {
		t.Fatalf("bridged canary expectation=%q err=%v, want allow", got, err)
	}
}

func TestWaitForFirewallPolicyCanaryRejectsChangedSnapshot(t *testing.T) {
	committed := []byte(`{"firewall":{"defaultAction":"ALLOW","rules":[]}}`)
	var reads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/config" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if reads.Add(1) == 1 {
			_, _ = w.Write(committed)
		} else {
			fmt.Fprint(w, `{"firewall":{"defaultAction":"DENY","rules":[]}}`)
		}
	}))
	t.Cleanup(upstream.Close)
	s := rangeServer(usGeneration(containd.NewClient(upstream.URL)), "custom")
	var err error
	s.lastAppliedHash, err = containd.FirewallHashFromBytes(committed)
	if err != nil {
		t.Fatal(err)
	}
	// No curriculum is needed for reconciliation of a committed manual policy.
	s.rng.(*fakeRange).gen.Package.Course.Features.SegmentationPlan = nil
	calls := scriptedFirewallCanary(t, s, []canaryObservation{{rc: 0}})
	if err := s.waitForFirewallPolicy(t.Context(), s.rng.(*fakeRange).gen); err == nil || !strings.Contains(err.Error(), "policy changed") {
		t.Fatalf("accepted observation across a running-policy change: %v", err)
	}
	if *calls != 1 || reads.Load() != 2 {
		t.Fatalf("calls=%d config reads=%d, want snapshot and final identity check", *calls, reads.Load())
	}
}

func TestWaitForFirewallPolicyCanaryDeadlineIsUntested(t *testing.T) {
	setFirewallWaitBudget(t, 25*time.Millisecond)
	for _, active := range []string{"weak", "improved"} {
		t.Run(active, func(t *testing.T) {
			s := firewallWaitTestServer(t, usPackageID, active, true)
			s.execInContainer = func(ctx context.Context, _ string, _ []string, _ int) (string, string, int, error) {
				<-ctx.Done()
				rc := 0
				if active == "improved" {
					rc = 124
				}
				return "", "", rc, nil
			}
			err := s.waitForFirewallPolicy(t.Context(), s.rng.(*fakeRange).gen)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline observation counted as a verdict: %v", err)
			}
		})
	}
}

func TestWaitForFirewallPolicyHashTimeoutSkipsCanary(t *testing.T) {
	setFirewallWaitBudget(t, 25*time.Millisecond)
	s := firewallWaitTestServer(t, usPackageID, "weak", false)
	calls := scriptedFirewallCanary(t, s, []canaryObservation{{rc: 0}})
	err := s.waitForFirewallPolicy(context.Background(), s.rng.(*fakeRange).gen)
	if err == nil || !strings.Contains(err.Error(), "firewall config hash did not reconcile") {
		t.Fatalf("waitForFirewallPolicy() error = %v, want config hash timeout", err)
	}
	if *calls != 0 {
		t.Errorf("canary calls = %d, want 0", *calls)
	}
}
