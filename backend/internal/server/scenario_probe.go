package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

const probeCommand = `if command -v bash >/dev/null 2>&1; then timeout 3 bash -c 'exec 3<>/dev/tcp/$1/$2' _ "$1" "$2"; else timeout 3 nc -w 3 "$1" "$2" </dev/null; fi`

// executeProbe tests TCP reachability from topology nodes. Rows remain in YAML
// order even though blocked targets run concurrently.
func (s *Server) executeProbe(ctx context.Context, gen *lifecycle.Generation, step labs.ScenarioStep) []StepActionResult {
	results := make([]StepActionResult, len(step.Action.Targets))
	client := gen.Containd()
	if client == nil {
		return probePolicyFailures(step, "running policy unavailable: firewall client is not configured")
	}
	data, err := client.GetConfigDocument(ctx)
	if err != nil {
		return probePolicyFailures(step, "running policy unavailable: "+err.Error())
	}
	policy, err := readEvaluatedFirewall(data)
	if err != nil {
		return probePolicyFailures(step, "running policy cannot be evaluated: "+err.Error())
	}
	var workers sync.WaitGroup
	limit := make(chan struct{}, 5)
	for i, target := range step.Action.Targets {
		workers.Add(1)
		limit <- struct{}{}
		go func() {
			defer workers.Done()
			defer func() { <-limit }()
			results[i] = s.probeTarget(ctx, gen, step, target, policy)
		}()
	}
	workers.Wait()
	return results
}

func probePolicyFailures(step labs.ScenarioStep, detail string) []StepActionResult {
	results := make([]StepActionResult, len(step.Action.Targets))
	for i, target := range step.Action.Targets {
		source := target.From
		if source == "" {
			source = step.Node
		}
		prefix := ""
		if target.Note != "" {
			prefix = target.Note + ": "
		}
		results[i] = StepActionResult{
			Action: fmt.Sprintf("probe %s → %s:%d", source, target.Host, target.Port),
			Detail: prefix + detail,
		}
	}
	return results
}

func (s *Server) probeTarget(ctx context.Context, gen *lifecycle.Generation, step labs.ScenarioStep, target labs.ProbeTarget, policy evaluatedFirewall) StepActionResult {
	source := target.From
	if source == "" {
		source = step.Node
	}
	row := StepActionResult{Action: fmt.Sprintf("probe %s → %s:%d", source, target.Host, target.Port)}
	prefix := ""
	if target.Note != "" {
		prefix = target.Note + ": "
	}
	svc, err := nodeService(gen, source)
	if err != nil {
		row.Detail = prefix + err.Error()
		return row
	}
	route, err := labs.ResolveProbeTuple(gen.Package.Template, source, target.Host, target.Port)
	if err != nil {
		row.Detail = prefix + "probe target cannot resolve policy tuple: " + err.Error()
		return row
	}
	container := svc.Container
	cmd := []string{"/bin/sh", "-c", probeCommand, "_", target.Host, fmt.Sprint(target.Port)}
	stdout, stderr, rc, err := s.execInContainer(ctx, container, cmd, 4)
	verdict, detail := classifyScenarioProbe(stdout, stderr, rc, err)
	if verdict == "" {
		row.Detail = prefix + detail
		return row
	}
	expectedVerdict := labs.VerdictAllow
	if !route.Bridged {
		expectedVerdict = labs.Evaluate(policy.Rules, policy.DefaultAction, route.Tuple)
	}
	expected := "blocked"
	if expectedVerdict == labs.VerdictAllow {
		expected = "reachable"
	}
	row.Detail = prefix + fmt.Sprintf("expected %s by running policy, observed %s%s", expected, verdict, detail)
	row.Success = verdict == expected
	return row
}

func classifyScenarioProbe(stdout, stderr string, rc int, err error) (string, string) {
	if err != nil {
		return "", "probe failed: " + err.Error()
	}
	diagnostic := strings.ToLower(strings.TrimSpace(stdout + "\n" + stderr))
	refused := strings.Contains(diagnostic, "connection refused")
	timedOut := strings.Contains(diagnostic, "timed out")
	if refused && timedOut {
		return "", "probe error: conflicting refusal and timeout diagnostics"
	}
	switch rc {
	case 0:
		if refused || timedOut {
			return "", "probe error: successful exit conflicts with connection diagnostics"
		}
		return "reachable", ""
	case 124, 143:
		if refused {
			return "", "probe error: timeout exit conflicts with refusal diagnostic"
		}
		return "blocked", " (timeout 3 s)"
	case 1:
		switch {
		case refused:
			return "reachable", " (connection refused)"
		case timedOut:
			return "blocked", " (connection timed out)"
		default:
			return "", fmt.Sprintf("probe error: exit status 1 without refusal or timeout diagnostics: %s", diagnostic)
		}
	default:
		if diagnostic == "" {
			return "", fmt.Sprintf("probe error: unexpected exit status %d", rc)
		}
		return "", fmt.Sprintf("probe error: unexpected exit status %d: %s", rc, diagnostic)
	}
}
