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
	sequencePause func()
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
	case "command":
		r := executors.command(step.Action.Device, step.Action.Command, step.Action.Source, step.Action.Value)
		result.Passed = r.Success
		if !r.Success && strings.Contains(r.Detail, "BLOCKED by containd") {
			result.Passed = true
			r.Detail += " (expected — hardened policy is working)"
		}
		result.Detail = r.Detail

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
		checks := executors.check(step.Action.Expect)
		allPass := true
		for _, check := range checks {
			if !check.Success {
				allPass = false
				break
			}
		}
		result.Passed = allPass
		result.Detail = fmt.Sprintf("%d checks, all pass: %v", len(checks), allPass)

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
