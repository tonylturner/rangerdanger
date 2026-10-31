package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestProbeVerdictsUseDiagnostics(t *testing.T) {
	tests := []struct {
		name        string
		stdout      string
		stderr      string
		rc          int
		err         error
		policy      string
		wantSuccess bool
		wantDetail  string
	}{
		{"open", "", "", 0, nil, "ALLOW", true, "expected reachable by running policy, observed reachable"},
		{"fast refusal", "", "bash: connect: Connection refused", 1, nil, "ALLOW", true, "expected reachable by running policy, observed reachable (connection refused)"},
		{"slow refusal", "", "nc: connect to host 10.40.40.20 port 502 (tcp) failed: Connection refused", 1, nil, "ALLOW", true, "expected reachable by running policy, observed reachable (connection refused)"},
		{"real timeout", "", "nc: timed out", 1, nil, "DENY", true, "expected blocked by running policy, observed blocked (connection timed out)"},
		{"GNU timeout", "", "", 124, nil, "DENY", true, "expected blocked by running policy, observed blocked (timeout 3 s)"},
		{"BusyBox timeout", "", "", 143, nil, "DENY", true, "expected blocked by running policy, observed blocked (timeout 3 s)"},
		{"failed tooling", "", "nc: invalid option -- z", 1, nil, "ALLOW", false, "probe error: exit status 1 without refusal or timeout diagnostics"},
		{"missing tool", "", "timeout: not found", 127, nil, "ALLOW", false, "probe error: unexpected exit status 127"},
		{"killed", "", "", 137, nil, "ALLOW", false, "probe error: unexpected exit status 137"},
		{"inspect failure", "", "", -1, nil, "DENY", false, "probe error: unexpected exit status -1"},
		{"expect mismatch", "", "", 0, nil, "DENY", false, "expected blocked by running policy, observed reachable"},
		{"docker failure", "", "", -1, errors.New("container stopped"), "DENY", false, "probe failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := activePackageServer(nil)
			useProbePolicy(t, s, probeTestPolicy(tt.policy))
			s.execInContainer = func(_ context.Context, container string, cmd []string, timeout int) (string, string, int, error) {
				if container != "rangerdanger-kali" || timeout != 4 || len(cmd) != 6 || cmd[4] != "10.40.40.20" || cmd[5] != "502" || !strings.Contains(cmd[2], "timeout 3 nc -w 3") {
					t.Errorf("exec arguments: container=%q timeout=%d cmd=%q", container, timeout, cmd)
				}
				return tt.stdout, tt.stderr, tt.rc, tt.err
			}
			step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.40.40.20", Port: 502, Note: "Relay"}}}}
			rows := s.executeProbe(context.Background(), s.rng.(*fakeRange).gen, step)
			if len(rows) != 1 || rows[0].Success != tt.wantSuccess || rows[0].Action != "probe kali-1 → 10.40.40.20:502" || !strings.Contains(rows[0].Detail, tt.wantDetail) || !strings.HasPrefix(rows[0].Detail, "Relay: ") {
				t.Fatalf("rows = %+v", rows)
			}
		})
	}
}

func TestProbeMultiSourceAndOrder(t *testing.T) {
	s := activePackageServer(nil)
	useProbePolicy(t, s, probeTestPolicy("ALLOW"))
	var mu sync.Mutex
	containers := map[string]bool{}
	s.execInContainer = func(_ context.Context, container string, _ []string, _ int) (string, string, int, error) {
		mu.Lock()
		containers[container] = true
		mu.Unlock()
		return "", "", 0, nil
	}
	step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{
		{Host: "10.40.40.20", Port: 502},
		{From: "eng-ws-1", Host: "10.40.40.23", Port: 502},
		{From: "rtac-1", Host: "10.40.40.20", Port: 20000},
	}}}
	rows := s.executeProbe(context.Background(), s.rng.(*fakeRange).gen, step)
	if len(rows) != 3 || !strings.Contains(rows[0].Action, "kali-1") || !strings.Contains(rows[1].Action, "eng-ws-1") || !strings.Contains(rows[2].Action, "rtac-1") {
		t.Fatalf("rows = %+v", rows)
	}
	// Sources resolve through the manifest by node; there is no
	// name-synthesising fallback.
	for _, container := range []string{"rangerdanger-kali", "rangerdanger-eng-ws", "rangerdanger-rtac-sim"} {
		if !containers[container] {
			t.Errorf("missing exec on %s", container)
		}
	}
}

func TestProbeUnknownSourceNode(t *testing.T) {
	s := activePackageServer(nil)
	useProbePolicy(t, s, probeTestPolicy("ALLOW"))
	s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
		t.Error("probe ran for a node outside the manifest")
		return "", "", 0, nil
	}
	step := labs.ScenarioStep{Node: "plc-9", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.40.40.20", Port: 502}}}}
	rows := s.executeProbe(context.Background(), s.rng.(*fakeRange).gen, step)
	if len(rows) != 1 || rows[0].Success || !strings.Contains(rows[0].Detail, "node plc-9 is not part of package us-dnp3-substation") {
		t.Fatalf("rows = %+v", rows)
	}
}
