package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

type stepTestResult struct {
	StepIndex  int    `json:"step_index"`
	StepTitle  string `json:"step_title"`
	Passed     bool   `json:"passed"`
	AutoPass   bool   `json:"auto_pass"`
	Detail     string `json:"detail"`
	DurationMs int64  `json:"duration_ms"`
}

var firewallWaitBudget = 15 * time.Second

const (
	firewallHashPollInterval   = 200 * time.Millisecond
	firewallCanaryPollInterval = 500 * time.Millisecond
	// wireReplaySettle is the wait after a check step's replayed wire
	// commands. The US RTAC polls field devices every 2 s
	// (services/rtac-sim/main.go), so a shorter wait can read pre-attack
	// state. It matches the ~3 s the lab text tells students to wait.
	wireReplaySettle = 3 * time.Second
)

// ensureHardenedPrecondition applies the reference policy only when a step
// declares that it must begin under hardened policy.
func ensureHardenedPrecondition(step labs.ScenarioStep, active string, apply func(string) ([]string, error)) (bool, error) {
	if step.ExpectedConfig != labs.ExpectedConfigHardened || active == labs.HardenedPolicy || active == "custom" {
		return false, nil
	}
	warnings, err := apply(labs.HardenedPolicy)
	if err != nil {
		return false, err
	}
	if len(warnings) > 0 {
		return true, fmt.Errorf("firewall apply warnings: %s", strings.Join(warnings, "; "))
	}
	return true, nil
}

// waitForFirewallPolicy waits for containd's running firewall document to
// match the just-committed policy, then checks the dataplane canary.
// The backend's activeConfig flips at commit time, so neither it nor
// the config hash alone establishes that the running dataplane is reconciled.
func (s *Server) waitForFirewallPolicy(ctx context.Context, gen *lifecycle.Generation) error {
	s.activeConfigMu.RLock()
	want := s.lastAppliedHash
	active := s.activeConfig
	s.activeConfigMu.RUnlock()
	if want == "" {
		return fmt.Errorf("no recorded firewall hash")
	}
	budget := firewallWaitBudget
	deadline := time.Now().Add(budget)
	var lastErr error
	var committed []byte
	for {
		if !time.Now().Before(deadline) {
			return firewallHashTimeout(budget, lastErr)
		}
		document, err := gen.Containd().GetConfigDocument(ctx)
		var got string
		if err == nil {
			got, err = containd.FirewallHashFromBytes(document)
		}
		if err == nil && got == want {
			if time.Now().Before(deadline) {
				committed = document
				break
			}
			return firewallHashTimeout(budget, nil)
		}
		lastErr = err
		if !sleepWithinFirewallBudget(ctx, deadline, firewallHashPollInterval) {
			return firewallHashTimeout(budget, lastErr)
		}
	}

	recipe, err := recipeFor(gen)
	if err != nil {
		return fmt.Errorf("dataplane never reconciled to %s: %w", active, err)
	}
	canary := recipe.canary
	from, err := nodeService(gen, canary.fromNode)
	if err != nil {
		return fmt.Errorf("dataplane never reconciled to %s: resolve canary source: %w", active, err)
	}
	target, err := nodeIP(gen, canary.toNode, canary.network)
	if err != nil {
		return fmt.Errorf("dataplane never reconciled to %s: resolve canary target: %w", active, err)
	}
	label := fmt.Sprintf("%s→%s:%d", canary.label, target, canary.port)
	expected, err := firewallCanaryExpectation(gen, active, canary, target, committed)
	if err != nil {
		return fmt.Errorf("dataplane never reconciled to %s: canary %s expectation unavailable: %w", active, label, err)
	}
	if s.execInContainer == nil {
		return fmt.Errorf("dataplane never reconciled to %s: canary %s cannot run without container exec", active, label)
	}

	var lastProbeErr error
	lastVerdict := "skipped"
	for {
		if !time.Now().Before(deadline) {
			return firewallCanaryTimeout(active, label, lastVerdict, lastProbeErr, budget)
		}
		probeCtx, cancel := context.WithTimeout(ctx, min(time.Until(deadline), euProbeExecLimit))
		stdout, stderr, rc, probeErr := s.execInContainer(probeCtx, from.Container, []string{
			"sh", "-c", euTCPCommand, "probe", target, fmt.Sprint(canary.port),
		}, int(euProbeExecLimit/time.Second))
		verdict, detail := classifyStrictTCP(stdout, stderr, rc, probeErr)
		lastProbeErr = nil
		if err := probeCtx.Err(); err != nil {
			verdict, lastProbeErr = "skipped", err
		} else if detail != "" {
			lastProbeErr = fmt.Errorf("%s", detail)
		}
		cancel()
		lastVerdict = verdict
		if verdict == expected {
			return nil
		}
		if !sleepWithinFirewallBudget(ctx, deadline, firewallCanaryPollInterval) {
			return firewallCanaryTimeout(active, label, lastVerdict, lastProbeErr, budget)
		}
	}
}

func firewallCanaryExpectation(gen *lifecycle.Generation, active string, canary canaryProbe, target string, committed []byte) (string, error) {
	switch active {
	case canary.allowUnder:
		return string(labs.VerdictAllow), nil
	case canary.denyUnder:
		return string(labs.VerdictDeny), nil
	}
	// The canary checks reconciliation, not student correctness. Its oracle
	// is the committed config snapshot whose hash was verified above; the
	// measured fact is the dataplane. Probes and validators separately judge
	// the policy against the student's change request or the full reference.
	policy, err := readEvaluatedFirewall(committed)
	if err != nil {
		return "", err
	}
	route, err := labs.ResolveProbeTuple(gen.Package.Template, canary.fromNode, target, canary.port)
	if err != nil {
		return "", err
	}
	if route.Bridged {
		return string(labs.VerdictAllow), nil
	}
	return string(labs.Evaluate(policy.Rules, policy.DefaultAction, route.Tuple)), nil
}

func firewallHashTimeout(budget time.Duration, lastErr error) error {
	if lastErr != nil {
		return fmt.Errorf("firewall config hash did not reconcile after %s: %w", budget, lastErr)
	}
	return fmt.Errorf("firewall config hash did not reconcile after %s", budget)
}

func sleepWithinFirewallBudget(ctx context.Context, deadline time.Time, interval time.Duration) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	if interval > remaining {
		interval = remaining
	}
	return sleepCtx(ctx, interval) && time.Now().Before(deadline)
}

// sleepCtx sleeps for d unless ctx ends first, and reports whether ctx is
// still live. Long handlers use it so a range switch drains them promptly.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func firewallCanaryTimeout(active, canary, verdict string, probeErr error, budget time.Duration) error {
	if probeErr != nil {
		return fmt.Errorf("dataplane never reconciled to %s: canary %s unavailable after %s: %w", active, canary, budget, probeErr)
	}
	return fmt.Errorf("dataplane never reconciled to %s: canary %s still %s after %s", active, canary, verdict, budget)
}

type scenarioTestResult struct {
	ScenarioID   string           `json:"scenario_id"`
	ScenarioName string           `json:"scenario_name"`
	Order        string           `json:"order"`
	Steps        []stepTestResult `json:"steps"`
	Passed       bool             `json:"passed"`
	ResetOK      bool             `json:"reset_ok"`
	ResetDetail  string           `json:"reset_detail,omitempty"`
	DurationMs   int64            `json:"duration_ms"`
}

type testSuiteResult struct {
	Containd      *containd.ApplyReceipt `json:"containd,omitempty"`
	Scenarios     []scenarioTestResult   `json:"scenarios"`
	TotalTests    int                    `json:"total_tests"`
	Passed        int                    `json:"passed"`
	Failed        int                    `json:"failed"`
	AutoPassed    int                    `json:"auto_passed"`
	ResetFailures int                    `json:"reset_failures"`
	DurationMs    int64                  `json:"duration_ms"`
}

// handleWorkshopTestSuite runs all exercises in order and reports results.
func (s *Server) handleWorkshopTestSuite(c *gin.Context) {
	ctx := c.Request.Context()
	gen := rangeOf(c)
	recipe, ok := supportedRecipe(c, func(r *packageRecipe) bool { return r.testSuite })
	if !ok {
		return
	}
	suiteStart := time.Now()
	var lastApply *containd.ApplyResult
	remember := func(applied *containd.ApplyResult) {
		if applied != nil && applied.Receipt != nil {
			lastApply = applied
		}
	}
	failApply := func(err error) {
		writeApplyError(c, err, lastApply, gin.H{"context": "workshop test suite"})
	}

	// Load the active package's scenarios ordered by `order`
	var scenarios []models.Scenario
	if err := s.activeScenarios().Order("\"order\" ASC, name ASC").Find(&scenarios).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Sort by order field
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Order < scenarios[j].Order
	})

	var results []scenarioTestResult
	totalTests := 0
	totalPassed := 0
	totalFailed := 0
	autoPassed := 0

	for _, sc := range scenarios {
		if ctx.Err() != nil {
			break
		}
		log.Printf("TEST SUITE: Running scenario %s: %s", sc.Order, sc.Name)
		scenarioStart := time.Now()

		// Reset lab before each scenario
		preResetProblems, resetApply, resetErr := s.resetLabState(ctx, gen, recipe)
		remember(resetApply)
		if resetErr != nil {
			failApply(resetErr)
			return
		}
		if err := s.waitForFirewallPolicy(ctx, gen); err != nil {
			preResetProblems = append(preResetProblems, "firewall dataplane: "+err.Error())
		}

		// Parse steps
		var steps []labs.ScenarioStep
		json.Unmarshal([]byte(sc.Steps), &steps)

		var stepResults []stepTestResult

		for i, step := range steps {
			if ctx.Err() != nil {
				break
			}
			stepStart := time.Now()
			totalTests++
			s.activeConfigMu.RLock()
			active := s.activeConfig
			s.activeConfigMu.RUnlock()
			var applyErr error
			applied, prepErr := ensureHardenedPrecondition(step, active, func(name string) ([]string, error) {
				applied, err := s.applyFirewallConfigInternal(ctx, gen, name)
				remember(applied)
				applyErr = err
				if applied == nil {
					return nil, err
				}
				return applied.Warnings, err
			})
			if applyErr != nil {
				failApply(applyErr)
				return
			}
			if prepErr == nil && applied {
				prepErr = s.waitForFirewallPolicy(ctx, gen)
			}

			result := stepTestResult{StepIndex: i, StepTitle: step.Title}
			if prepErr != nil {
				result.Detail = "hardened precondition: " + prepErr.Error()
			} else {
				result = evaluateTestStep(i, step, stepExecutors{
					command: func(device, command, source string, value *float64) StepActionResult {
						return s.executeCommand(ctx, gen, device, command, source, value)
					},
					firewall: func(name string) StepActionResult {
						action := s.executeFirewallAction(ctx, gen, name)
						if action.Containd != nil {
							remember(&containd.ApplyResult{Receipt: action.Containd, Warnings: action.Warnings})
						}
						applyErr = action.applyError
						return action
					},
					check:      func(expect map[string]any) []StepActionResult { return s.executeSuiteCheck(ctx, gen, expect) },
					probe:      func(step labs.ScenarioStep) []StepActionResult { return s.executeProbe(ctx, gen, step) },
					sessionCut: func(flow string) StepActionResult { return s.executeSessionCut(ctx, gen, flow) },
					sequencePause: func() {
						sleepCtx(ctx, 300*time.Millisecond)
					},
					runWire: func(run []labs.StepRun) string {
						return s.replayWireCommands(ctx, gen, run)
					},
				})
				if applyErr != nil {
					failApply(applyErr)
					return
				}
				if step.Action != nil && step.Action.Type == "firewall" && result.Passed {
					if err := s.waitForFirewallPolicy(ctx, gen); err != nil {
						result.Passed = false
						result.Detail += "; dataplane: " + err.Error()
					}
				}
			}
			result.DurationMs = time.Since(stepStart).Milliseconds()
			stepResults = append(stepResults, result)

			if result.Passed {
				totalPassed++
			} else {
				totalFailed++
			}
			if result.AutoPass {
				autoPassed++
			}

			// Pause between steps — longer after device commands to let state propagate
			if step.Action != nil && step.Action.Type == "sequence" {
				sleepCtx(ctx, 1500*time.Millisecond)
			} else {
				sleepCtx(ctx, 200*time.Millisecond)
			}
		}

		// Reset after scenario
		postResetProblems, resetApply, resetErr := s.resetLabState(ctx, gen, recipe)
		remember(resetApply)
		if resetErr != nil {
			failApply(resetErr)
			return
		}
		resetOK, resetDetail := aggregateResetProblems(preResetProblems, postResetProblems)

		scenarioResult := scenarioTestResult{
			ScenarioID:   sc.ID,
			ScenarioName: sc.Name,
			Order:        sc.Order,
			Steps:        stepResults,
			ResetOK:      resetOK,
			ResetDetail:  resetDetail,
			DurationMs:   time.Since(scenarioStart).Milliseconds(),
		}

		// Scenario passes if all steps pass and both resets were clean
		scenarioResult.Passed = resetOK
		for _, sr := range stepResults {
			if !sr.Passed {
				scenarioResult.Passed = false
				break
			}
		}

		results = append(results, scenarioResult)
		log.Printf("TEST SUITE: Scenario %s %s: %v", sc.Order, sc.Name, scenarioResult.Passed)
	}
	if err := ctx.Err(); err != nil {
		fields := gin.H{"error": "test suite interrupted: " + err.Error()}
		if lastApply != nil {
			fields["containd"] = lastApply.Receipt
		}
		c.JSON(http.StatusServiceUnavailable, fields)
		return
	}

	var receipt *containd.ApplyReceipt
	if lastApply != nil {
		receipt = lastApply.Receipt
	}
	c.JSON(http.StatusOK, testSuiteResult{
		Containd:      receipt,
		Scenarios:     results,
		TotalTests:    totalTests,
		Passed:        totalPassed,
		Failed:        totalFailed,
		AutoPassed:    autoPassed,
		ResetFailures: countResetFailures(results),
		DurationMs:    time.Since(suiteStart).Milliseconds(),
	})
}

// replayWireCommands sends a check step's authored wire commands through the
// same exec path as the Run button, in order, with the Run-button timeout. It
// then waits wireReplaySettle so device and RTAC state settle before the
// check reads them. The check remains the oracle: a non-zero
// exit (a hardened retest times out by design) or an exec error is recorded in
// the step detail, never a failure on its own.
func (s *Server) replayWireCommands(ctx context.Context, gen *lifecycle.Generation, run []labs.StepRun) string {
	parts := make([]string, 0, len(run))
	for i, entry := range run {
		if i > 0 {
			sleepCtx(ctx, 300*time.Millisecond)
		}
		_, _, exitCode, err := s.execOnNode(ctx, gen, entry.Node, entry.Command, 30)
		if err != nil {
			parts = append(parts, fmt.Sprintf("%s: exec error: %v", entry.Node, err))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s [EXIT %d]", entry.Node, exitCode))
	}
	sleepCtx(ctx, wireReplaySettle)
	return "ran " + strings.Join(parts, ", ")
}

// resetLabState restores all devices to defaults.
func (s *Server) resetLabState(ctx context.Context, gen *lifecycle.Generation, recipe *packageRecipe) ([]string, *containd.ApplyResult, error) {
	var problems []string
	policy, err := defaultPolicy(gen)
	if err != nil {
		return []string{"firewall apply: " + err.Error()}, nil, err
	}
	applied, applyErr := s.applyFirewallConfigInternal(ctx, gen, policy)
	if applyErr != nil {
		return []string{"firewall apply: " + applyErr.Error()}, applied, applyErr
	}
	for _, warning := range applied.Warnings {
		problems = append(problems, "firewall apply warning: "+warning)
	}

	if recipe.resetTruth != nil {
		for _, action := range recipe.resetTruth(ctx, gen, recipe) {
			if !action.Success {
				problems = append(problems, action.Action+": "+action.Detail)
			}
		}
	}

	for _, cmd := range recipe.resetCommands {
		result := s.executeCommand(ctx, gen, cmd.device, cmd.command, "reset-script", cmd.value)
		if !result.Success {
			problems = append(problems, fmt.Sprintf("%s/%s: %s", cmd.device, cmd.command, result.Detail))
		}
	}

	// Clear PCAP captures so validators don't see stale files
	s.pcapMu.Lock()
	s.pcap.FileReady = false
	s.pcapMu.Unlock()
	firewall, err := firewallContainer(gen)
	if err != nil {
		problems = append(problems, "clear PCAP captures: "+err.Error())
	} else if dockerCli := s.orchestrator.DockerClient(); dockerCli != nil {
		out, exitCode, execErr := s.execCapture(ctx, dockerCli, firewall, []string{"sh", "-c", "rm -f /data/captures/*.pcap /tmp/capture*.pcap 2>/dev/null"})
		if execErr != nil {
			problems = append(problems, "clear PCAP captures: "+execErr.Error())
		} else if exitErr := commandExecutionError(exitCode, out); exitErr != nil {
			problems = append(problems, "clear PCAP captures: "+exitErr.Error())
		}
	} else {
		problems = append(problems, "clear PCAP captures: Docker client not configured")
	}

	sleepCtx(ctx, 500*time.Millisecond)
	return problems, applied, applyErr
}
