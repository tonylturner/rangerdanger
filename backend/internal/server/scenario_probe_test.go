package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestProbeVerdicts(t *testing.T) {
	tests := []struct {
		name        string
		rc          int
		delay       time.Duration
		err         error
		outcome     string
		wantSuccess bool
		wantDetail  string
	}{
		{"open", 0, 0, nil, "reachable", true, "reachable in"},
		{"GNU timeout", 124, 0, nil, "blocked", true, "blocked (timeout 3 s)"},
		{"BusyBox timeout", 143, 0, nil, "blocked", true, "blocked (timeout 3 s)"},
		{"fast RST", 1, 0, nil, "reachable", true, "connection refused"},
		{"slow refusal", 1, 510 * time.Millisecond, nil, "blocked", true, "blocked"},
		{"missing tool", 127, 0, nil, "blocked", false, "probe error: unexpected exit status 127"},
		{"killed", 137, 0, nil, "reachable", false, "probe error: unexpected exit status 137"},
		{"inspect failure", -1, 0, nil, "blocked", false, "probe error: unexpected exit status -1"},
		{"expect mismatch", 0, 0, nil, "blocked", false, "reachable"},
		{"docker failure", -1, 0, errors.New("container stopped"), "blocked", false, "probe failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := activePackageServer(nil)
			s.execInContainer = func(_ context.Context, container string, cmd []string, timeout int) (string, string, int, error) {
				if container != "rangerdanger-kali" || timeout != 4 || len(cmd) != 6 || cmd[4] != "10.40.40.20" || cmd[5] != "502" || !strings.Contains(cmd[2], "timeout 3 nc -w 3") {
					t.Errorf("exec arguments: container=%q timeout=%d cmd=%q", container, timeout, cmd)
				}
				time.Sleep(tt.delay)
				return "", "", tt.rc, tt.err
			}
			step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Outcome: tt.outcome, Targets: []labs.ProbeTarget{{Host: "10.40.40.20", Port: 502, Note: "Relay"}}}}
			rows := s.executeProbe(context.Background(), s.rng.(*fakeRange).gen, step)
			if len(rows) != 1 || rows[0].Success != tt.wantSuccess || rows[0].Action != "probe kali-1 → 10.40.40.20:502" || !strings.Contains(rows[0].Detail, tt.wantDetail) || !strings.HasPrefix(rows[0].Detail, "Relay: ") {
				t.Fatalf("rows = %+v", rows)
			}
		})
	}
}

func TestProbeMultiSourceAndOrder(t *testing.T) {
	s := activePackageServer(nil)
	var mu sync.Mutex
	containers := map[string]bool{}
	s.execInContainer = func(_ context.Context, container string, _ []string, _ int) (string, string, int, error) {
		mu.Lock()
		containers[container] = true
		mu.Unlock()
		return "", "", 0, nil
	}
	step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Outcome: "reachable", Targets: []labs.ProbeTarget{
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
	s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
		t.Error("probe ran for a node outside the manifest")
		return "", "", 0, nil
	}
	step := labs.ScenarioStep{Node: "plc-9", Action: &labs.StepAction{Type: "probe", Outcome: "reachable", Targets: []labs.ProbeTarget{{Host: "10.40.40.20", Port: 502}}}}
	rows := s.executeProbe(context.Background(), s.rng.(*fakeRange).gen, step)
	if len(rows) != 1 || rows[0].Success || !strings.Contains(rows[0].Detail, "node plc-9 is not part of package us-dnp3-substation") {
		t.Fatalf("rows = %+v", rows)
	}
}
