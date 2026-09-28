package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

const probeCommand = `if command -v bash >/dev/null 2>&1; then timeout 3 bash -c 'exec 3<>/dev/tcp/$1/$2' _ "$1" "$2"; else timeout 3 nc -w 3 "$1" "$2" </dev/null; fi`

// executeProbe tests TCP reachability from topology nodes. Rows remain in YAML
// order even though blocked targets run concurrently.
func (s *Server) executeProbe(step labs.ScenarioStep) []StepActionResult {
	results := make([]StepActionResult, len(step.Action.Targets))
	var workers sync.WaitGroup
	limit := make(chan struct{}, 5)
	for i, target := range step.Action.Targets {
		workers.Add(1)
		limit <- struct{}{}
		go func() {
			defer workers.Done()
			defer func() { <-limit }()
			results[i] = s.probeTarget(step, target)
		}()
	}
	workers.Wait()
	return results
}

func (s *Server) probeTarget(step labs.ScenarioStep, target labs.ProbeTarget) StepActionResult {
	source := target.From
	if source == "" {
		source = step.Node
	}
	row := StepActionResult{Action: fmt.Sprintf("probe %s → %s:%d", source, target.Host, target.Port)}
	prefix := ""
	if target.Note != "" {
		prefix = target.Note + ": "
	}
	node, err := s.resolveWorkshopNode(source)
	if err != nil {
		row.Detail = prefix + err.Error()
		return row
	}
	container := node.Container
	if container == "" {
		container = "rangerdanger-" + strings.ReplaceAll(node.ID, "_", "-")
	}
	cmd := []string{"/bin/sh", "-c", probeCommand, "_", target.Host, fmt.Sprint(target.Port)}
	start := time.Now()
	_, _, rc, err := s.execInContainer(context.Background(), container, cmd, 4)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		row.Detail = prefix + "probe failed: " + err.Error()
		return row
	}
	var verdict, suffix string
	switch rc {
	case 0:
		verdict = "reachable"
		suffix = fmt.Sprintf(" in %d ms", ms)
	case 124, 143:
		verdict = "blocked"
		suffix = " (timeout 3 s)"
	case 1:
		if ms < 500 {
			verdict = "reachable"
			suffix = fmt.Sprintf(" in %d ms (connection refused)", ms)
		} else {
			verdict = "blocked"
			suffix = fmt.Sprintf(" (%d ms, connection refused or timeout)", ms)
		}
	default:
		// Anything else (127 missing tool, 137 killed, -1 inspect failure)
		// says nothing about the firewall; never let it certify "blocked".
		row.Detail = fmt.Sprintf("%sprobe error: unexpected exit status %d after %d ms", prefix, rc, ms)
		return row
	}
	row.Detail = prefix + verdict + suffix
	row.Success = verdict == step.Action.Outcome
	return row
}
