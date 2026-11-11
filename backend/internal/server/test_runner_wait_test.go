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
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func firewallWaitTestServer(t *testing.T, pkg, active string, hashMatches bool) *Server {
	t.Helper()

	wantedFirewall := map[string]any{"defaultAction": "DENY", "rules": []any{}}
	observedFirewall := wantedFirewall
	if !hashMatches {
		observedFirewall = map[string]any{"defaultAction": "ALLOW", "rules": []any{}}
	}
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
	if err == nil || !strings.Contains(err.Error(), "still allow") {
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

func TestWaitForFirewallPolicyCustomCanaryUsesProbeJudgingTarget(t *testing.T) {
	setFirewallWaitBudget(t, 25*time.Millisecond)
	for _, pkg := range []string{usPackageID, euPackageID} {
		for _, tc := range []struct {
			name, source string
			applied      bool
			full         bool
			request      bool
			requestFull  bool
			rc           int
			pass         bool
		}{
			{name: "deferred plan allows", applied: true, rc: 0, pass: true},
			{name: "deferred plan rejects timeout", applied: true, rc: 124},
			{name: "full plan denies", applied: true, full: true, rc: 124, pass: true},
			{name: "full plan rejects connection", applied: true, full: true, rc: 0},
			{name: "manual uses full reference", source: "manual-custom", rc: 124, pass: true},
			{name: "manual exposure fails reference", source: "manual-custom", rc: 0},
			{name: "saved empty request allows", source: "manual-custom", request: true, rc: 0, pass: true},
			{name: "saved full request denies", source: "manual-custom", request: true, requestFull: true, rc: 124, pass: true},
			{name: "applied empty plan supersedes full request", applied: true, request: true, requestFull: true, rc: 0, pass: true},
			{name: "applied full plan supersedes empty request", applied: true, full: true, request: true, rc: 124, pass: true},
		} {
			t.Run(pkg+"/"+tc.name, func(t *testing.T) {
				s := firewallWaitTestServer(t, pkg, "custom", true)
				gen := s.rng.(*fakeRange).gen
				s.policySource = tc.source
				var all []string
				for _, action := range gen.Package.Curriculum.Catalog {
					all = append(all, action.ID)
				}
				ctx := t.Context()
				if tc.request {
					var ids []string
					if tc.requestFull {
						ids = all
					}
					ctx = context.WithValue(ctx, changeRequestKey{}, compiledChangeRequest{compileProbePlan(t, gen, ids)})
				}
				if tc.applied {
					var ids []string
					if tc.full {
						ids = all
					}
					// The mock hash deliberately differs from the plan; the
					// expected verdict must come from the judging target,
					// not the hash-matched running document.
					hash := s.lastAppliedHash
					useAppliedProbePlan(t, s, compileProbePlan(t, gen, ids))
					s.lastAppliedHash = hash
				}
				calls := scriptedFirewallCanary(t, s, []canaryObservation{{rc: tc.rc}})
				err := s.waitForFirewallPolicy(ctx, gen)
				if (err == nil) != tc.pass || *calls != 1 {
					t.Fatalf("canary err=%v calls=%d, want pass=%t", err, *calls, tc.pass)
				}
				port := 502
				if pkg == euPackageID {
					port = 2404
				}
				step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.30.30.20", Port: port}}}}
				s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
					return "", "", tc.rc, nil
				}
				rows := s.executeProbe(ctx, gen, step)
				if len(rows) != 1 || rows[0].Success != tc.pass {
					t.Fatalf("scenario probe disagrees with canary: %+v", rows)
				}
			})
		}
	}
}

func TestWaitForFirewallPolicyUnavailableCanaryExpectation(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprint(malformed), func(t *testing.T) {
			s := firewallWaitTestServer(t, usPackageID, "custom", true)
			gen := s.rng.(*fakeRange).gen
			if malformed {
				hash := s.lastAppliedHash
				useAppliedProbePlan(t, s, compileProbePlan(t, gen, nil))
				s.appliedPlan.Config = []byte(`{}`)
				s.lastAppliedHash = hash
			} else {
				gen.Package.Curriculum = nil
			}
			calls := scriptedFirewallCanary(t, s, []canaryObservation{{rc: 124}})
			err := s.waitForFirewallPolicy(t.Context(), gen)
			if err == nil || !strings.Contains(err.Error(), "expectation unavailable") || *calls != 0 {
				t.Fatalf("err=%v calls=%d, want explicit unavailable expectation without exec", err, *calls)
			}
		})
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
