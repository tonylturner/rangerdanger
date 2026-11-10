package server

import (
	"fmt"
	"strings"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

type stepExecutors struct {
	command       func(device, command, source string, value *float64) StepActionResult
	firewall      func(configName string) StepActionResult
	check         func(expect map[string]any) []StepActionResult
	probe         func(step labs.ScenarioStep) []StepActionResult
	sessionCut    func(flow string) StepActionResult
	sequencePause func()
	// runWire replays a check step's authored wire commands and returns a
	// detail string recording each exit. The check stays the oracle; a
	// non-zero exit is reported, not treated as a failure.
	runWire func(run []labs.StepRun) string
}

func evaluateTestStep(index int, step labs.ScenarioStep, executors stepExecutors) stepTestResult {
	result := stepTestResult{StepIndex: index, StepTitle: step.Title}
	if step.Action == nil {
		result.Passed = true
		result.AutoPass = true
		result.Detail = "manual step (no action)"
		return result
	}

	switch step.Action.Type {
	case "sequence":
		allOK := true
		for _, cmd := range step.Action.Commands {
			r := executors.command(cmd.Device, cmd.Command, cmd.Source, cmd.Value)
			if !r.Success {
				allOK = false
			}
			if executors.sequencePause != nil {
				executors.sequencePause()
			}
		}
		result.Passed = allOK
		result.Detail = fmt.Sprintf("%d commands", len(step.Action.Commands))

	case "firewall":
		r := executors.firewall(step.Action.Config)
		result.Passed = r.Success && len(r.Warnings) == 0
		result.Detail = r.Detail

	case "check":
		var runDetail string
		if len(step.Action.Run) > 0 && executors.runWire != nil {
			runDetail = executors.runWire(step.Action.Run)
		}
		checks := executors.check(step.Action.Expect)
		allPass := true
		var failures []string
		for _, check := range checks {
			if !check.Success {
				allPass = false
				failures = append(failures, check.Action+": "+check.Detail)
			}
		}
		result.Passed = allPass
		result.Detail = fmt.Sprintf("%d checks, all pass: %v", len(checks), allPass)
		if len(failures) > 0 {
			result.Detail += "; " + strings.Join(failures, "; ")
		}
		if runDetail != "" {
			result.Detail = runDetail + "; " + result.Detail
		}

	case "probe":
		probes := executors.probe(step)
		result.Passed = len(probes) > 0
		for _, probe := range probes {
			if !probe.Success {
				result.Passed = false
			}
		}
		result.Detail = fmt.Sprintf("%d probes, all pass: %v", len(probes), result.Passed)

	case "session-cut":
		r := executors.sessionCut(step.Action.Flow)
		result.Passed = r.Success
		result.Detail = r.Detail

	case "decision":
		result.Passed = true
		result.AutoPass = true
		result.Detail = "decision step (student planning — auto-pass)"

	default:
		result.Detail = fmt.Sprintf("unknown action type: %s", step.Action.Type)
	}

	return result
}

func aggregateResetProblems(preResetProblems, resetProblems []string) (bool, string) {
	var details []string
	for _, problem := range preResetProblems {
		details = append(details, "pre-reset: "+problem)
	}
	for _, problem := range resetProblems {
		details = append(details, "reset: "+problem)
	}
	return len(details) == 0, strings.Join(details, "; ")
}

func countResetFailures(scenarios []scenarioTestResult) int {
	count := 0
	for _, scenario := range scenarios {
		if !scenario.ResetOK {
			count++
		}
	}
	return count
}
