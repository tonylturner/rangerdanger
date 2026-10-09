package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

func firewallWaitTestServer(t *testing.T, active string, hashMatches bool) *Server {
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

	s := activePackageServer(nil)
	s.rng = servingRange(containd.NewClient(containdServer.URL))
	s.activeConfig = active
	s.lastAppliedHash = wantHash
	return s
}

func setFirewallWaitBudget(t *testing.T, budget time.Duration) {
	t.Helper()
	previous := firewallWaitBudget
	firewallWaitBudget = budget
	t.Cleanup(func() { firewallWaitBudget = previous })
}

func scriptedFirewallCanary(t *testing.T, s *Server, exitCodes []int) *int {
	t.Helper()
	calls := 0
	wantCommand := []string{"timeout", "1", "bash", "-c", "exec 3<>/dev/tcp/10.30.30.20/502"}
	s.execInContainer = func(_ context.Context, container string, cmd []string, timeout int) (string, string, int, error) {
		if container != "rangerdanger-kali" || timeout != 2 || !reflect.DeepEqual(cmd, wantCommand) {
			t.Errorf("exec arguments: container=%q timeout=%d cmd=%q", container, timeout, cmd)
		}
		index := calls
		calls++
		if index >= len(exitCodes) {
			index = len(exitCodes) - 1
		}
		return "", "", exitCodes[index], nil
	}
	return &calls
}

func TestWaitForFirewallPolicyCanary(t *testing.T) {
	tests := []struct {
		name      string
		active    string
		exitCodes []int
	}{
		{name: "weak waits for allow", active: "weak", exitCodes: []int{1, 1, 0}},
		{name: "improved waits for deny", active: "improved", exitCodes: []int{0, 0, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := firewallWaitTestServer(t, tt.active, true)
			calls := scriptedFirewallCanary(t, s, tt.exitCodes)
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
	s := firewallWaitTestServer(t, "improved", true)
	calls := scriptedFirewallCanary(t, s, []int{0})
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

func TestWaitForFirewallPolicyCustomSkipsCanary(t *testing.T) {
	s := firewallWaitTestServer(t, "custom", true)
	calls := scriptedFirewallCanary(t, s, []int{0})
	if err := s.waitForFirewallPolicy(context.Background(), s.rng.(*fakeRange).gen); err != nil {
		t.Fatalf("waitForFirewallPolicy() error = %v", err)
	}
	if *calls != 0 {
		t.Errorf("canary calls = %d, want 0", *calls)
	}
}

func TestWaitForFirewallPolicyHashTimeoutSkipsCanary(t *testing.T) {
	setFirewallWaitBudget(t, 25*time.Millisecond)
	s := firewallWaitTestServer(t, "weak", false)
	calls := scriptedFirewallCanary(t, s, []int{0})
	err := s.waitForFirewallPolicy(context.Background(), s.rng.(*fakeRange).gen)
	if err == nil || !strings.Contains(err.Error(), "firewall config hash did not reconcile") {
		t.Fatalf("waitForFirewallPolicy() error = %v, want config hash timeout", err)
	}
	if *calls != 0 {
		t.Errorf("canary calls = %d, want 0", *calls)
	}
}
