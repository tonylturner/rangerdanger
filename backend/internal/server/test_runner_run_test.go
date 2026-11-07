package server

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

// A check step that carries run replays its wire commands before the check,
// and the check stays the oracle: a non-zero exit is recorded, not a failure.
func TestEvaluateCheckReplaysRunThenChecks(t *testing.T) {
	step := labs.ScenarioStep{Title: "attack", Action: &labs.StepAction{
		Type: "check",
		Run: []labs.StepRun{
			{Node: "kali-1", Command: "dnp3cmd 10.40.40.21:20000 -a 2 crob 0 trip"},
		},
		Expect: map[string]any{"recloser_closed": false},
	}}

	var replayed [][]labs.StepRun
	result := evaluateTestStep(2, step, stepExecutors{
		check:   func(map[string]any) []StepActionResult { return []StepActionResult{{Success: true}} },
		runWire: func(run []labs.StepRun) string { replayed = append(replayed, run); return "ran kali-1 [EXIT 2]" },
	})

	if len(replayed) != 1 || !reflect.DeepEqual(replayed[0], step.Action.Run) {
		t.Fatalf("runWire replay = %+v, want the step's run list once", replayed)
	}
	if !result.Passed {
		t.Fatalf("check with a passing oracle must pass despite a non-zero run exit: %+v", result)
	}
	if !strings.HasPrefix(result.Detail, "ran kali-1 [EXIT 2]; ") {
		t.Fatalf("run detail not recorded in step detail: %q", result.Detail)
	}
}

// A check without run never touches the exec path.
func TestEvaluateCheckWithoutRunSkipsReplay(t *testing.T) {
	called := false
	result := evaluateTestStep(0, labs.ScenarioStep{Action: &labs.StepAction{
		Type: "check", Expect: map[string]any{"breaker_closed": true},
	}}, stepExecutors{
		check:   func(map[string]any) []StepActionResult { return []StepActionResult{{Success: true}} },
		runWire: func([]labs.StepRun) string { called = true; return "unexpected" },
	})
	if called {
		t.Fatal("runWire called for a check with no run")
	}
	if !result.Passed || result.Detail != "1 checks, all pass: true" {
		t.Fatalf("plain check result changed: %+v", result)
	}
}

// execOnNode resolves the node and runs through the same /bin/sh -c path and
// Run-button default timeout as the workshop exec endpoint.
func TestExecOnNodeUsesSharedShellPath(t *testing.T) {
	gen := usGeneration(nil)
	s := rangeServer(gen, "weak")
	var gotContainer string
	var gotCmd []string
	var gotTimeout int
	s.execInContainer = func(_ context.Context, container string, cmd []string, timeout int) (string, string, int, error) {
		gotContainer, gotCmd, gotTimeout = container, cmd, timeout
		return "stdout", "", 0, nil
	}

	stdout, _, code, err := s.execOnNode(t.Context(), gen, "kali-1", "dnp3cmd 10.40.40.21:20000 -a 2 crob 0 trip", 0)
	if err != nil || code != 0 || stdout != "stdout" {
		t.Fatalf("execOnNode = (%q, %d, %v)", stdout, code, err)
	}
	if gotContainer == "" {
		t.Fatal("execOnNode did not resolve the node container")
	}
	if !reflect.DeepEqual(gotCmd, []string{"/bin/sh", "-c", "dnp3cmd 10.40.40.21:20000 -a 2 crob 0 trip"}) {
		t.Fatalf("shell invocation = %q", gotCmd)
	}
	if gotTimeout != 30 {
		t.Fatalf("default timeout = %d, want the Run-button 30s", gotTimeout)
	}
}

// Interactive Execute Step ignores a check's run block and never sends a
// command, even on an attack step whose check expects post-attack state.
func TestExecuteStepCheckWithRunStaysReadOnly(t *testing.T) {
	s := rangeServer(euGeneration(t, nil, nil), "weak")
	steps := seedEUScenario(t, s)

	var found *labs.ScenarioStep
	for i := range steps {
		if steps[i].ID == "attack-iec104-breaker-open" {
			found = &steps[i]
			break
		}
	}
	if found == nil || found.Action == nil || len(found.Action.Run) == 0 {
		t.Fatal("fixture attack check lost its run block")
	}

	execCalls := 0
	s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
		execCalls++
		return "", "", 0, nil
	}

	result := executeAuthoredStep(t, s, steps, "attack-iec104-breaker-open")
	if result.ActionType != "check" {
		t.Fatalf("action type = %q, want check", result.ActionType)
	}
	if execCalls != 0 {
		t.Fatalf("Execute Step replayed %d wire commands; it must stay read-only", execCalls)
	}
}
