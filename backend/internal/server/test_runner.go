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

	"github.com/docker/docker/api/types/container"
	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
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
)

// ensureHardenedPrecondition applies the reference policy only when a step
// declares that it must begin under hardened policy.
func ensureHardenedPrecondition(step labs.ScenarioStep, active string, apply func(string) ([]string, error)) (bool, error) {
	if step.ExpectedConfig != "hardened" || active == "improved" || active == "custom" {
		return false, nil
	}
	warnings, err := apply("improved")
	if err != nil {
		return false, err
	}
	if len(warnings) > 0 {
		return true, fmt.Errorf("firewall apply warnings: %s", strings.Join(warnings, "; "))
	}
	return true, nil
}

// waitForFirewallPolicy waits for containd's running firewall document to
// match the just-committed policy, then checks the dataplane canary for canned
// policies. The backend's activeConfig flips at commit time, so neither it nor
// the config hash alone establishes that the running dataplane is reconciled.
func (s *Server) waitForFirewallPolicy() error {
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
	for {
		if !time.Now().Before(deadline) {
			return firewallHashTimeout(budget, lastErr)
		}
		got, err := s.containdClient.GetFirewallHash()
		if err == nil && got == want {
			if time.Now().Before(deadline) {
				break
			}
			return firewallHashTimeout(budget, nil)
		}
		lastErr = err
		if !sleepWithinFirewallBudget(deadline, firewallHashPollInterval) {
			return firewallHashTimeout(budget, lastErr)
		}
	}

	if active != "weak" && active != "improved" {
		return nil
	}
	node, err := s.resolveWorkshopNode("kali-1")
	if err != nil {
		return fmt.Errorf("dataplane never reconciled to %s: resolve canary kali→10.30.30.20:502: %w", active, err)
	}
	if node.Container == "" {
		return fmt.Errorf("dataplane never reconciled to %s: canary kali→10.30.30.20:502 has no container", active)
	}
	if s.execInContainer == nil {
		return fmt.Errorf("dataplane never reconciled to %s: canary kali→10.30.30.20:502 cannot run without container exec", active)
	}

	wantAllow := active == "weak"
	var lastProbeErr error
	lastVerdict := "unknown"
	for {
		if !time.Now().Before(deadline) {
			return firewallCanaryTimeout(active, lastVerdict, lastProbeErr, budget)
		}
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		_, _, rc, probeErr := s.execInContainer(ctx, node.Container, []string{
			"timeout", "1", "bash", "-c", "exec 3<>/dev/tcp/10.30.30.20/502",
		}, 2)
		cancel()
		lastProbeErr = probeErr
		if probeErr == nil {
			lastVerdict = "deny"
			if rc == 0 {
				lastVerdict = "allow"
			}
			if (wantAllow && rc == 0) || (!wantAllow && rc != 0) {
				return nil
			}
		}
		if !sleepWithinFirewallBudget(deadline, firewallCanaryPollInterval) {
			return firewallCanaryTimeout(active, lastVerdict, lastProbeErr, budget)
		}
	}
}

func firewallHashTimeout(budget time.Duration, lastErr error) error {
	if lastErr != nil {
		return fmt.Errorf("firewall config hash did not reconcile after %s: %w", budget, lastErr)
	}
	return fmt.Errorf("firewall config hash did not reconcile after %s", budget)
}

func sleepWithinFirewallBudget(deadline time.Time, interval time.Duration) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	if interval > remaining {
		interval = remaining
	}
	time.Sleep(interval)
	return time.Now().Before(deadline)
}

func firewallCanaryTimeout(active, verdict string, probeErr error, budget time.Duration) error {
	if probeErr != nil {
		return fmt.Errorf("dataplane never reconciled to %s: canary kali→10.30.30.20:502 unavailable after %s: %w", active, budget, probeErr)
	}
	return fmt.Errorf("dataplane never reconciled to %s: canary kali→10.30.30.20:502 still %s after %s", active, verdict, budget)
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
	Scenarios     []scenarioTestResult `json:"scenarios"`
	TotalTests    int                  `json:"total_tests"`
	Passed        int                  `json:"passed"`
	Failed        int                  `json:"failed"`
	AutoPassed    int                  `json:"auto_passed"`
	ResetFailures int                  `json:"reset_failures"`
	DurationMs    int64                `json:"duration_ms"`
}

// handleWorkshopTestSuite runs all exercises in order and reports results.
func (s *Server) handleWorkshopTestSuite(c *gin.Context) {
	suiteStart := time.Now()

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
		log.Printf("TEST SUITE: Running scenario %s: %s", sc.Order, sc.Name)
		scenarioStart := time.Now()

		// Reset lab before each scenario
		preResetProblems := s.resetLabState()
		if err := s.waitForFirewallPolicy(); err != nil {
			preResetProblems = append(preResetProblems, "firewall dataplane: "+err.Error())
		}

		// Parse steps
		var steps []labs.ScenarioStep
		json.Unmarshal([]byte(sc.Steps), &steps)

		var stepResults []stepTestResult

		for i, step := range steps {
			stepStart := time.Now()
			totalTests++
			s.activeConfigMu.RLock()
			active := s.activeConfig
			s.activeConfigMu.RUnlock()
			applied, prepErr := ensureHardenedPrecondition(step, active, s.applyFirewallConfigInternal)
			if prepErr == nil && applied {
				prepErr = s.waitForFirewallPolicy()
			}

			result := stepTestResult{StepIndex: i, StepTitle: step.Title}
			if prepErr != nil {
				result.Detail = "hardened precondition: " + prepErr.Error()
			} else {
				result = evaluateTestStep(i, step, stepExecutors{
					command:  s.executeCommand,
					firewall: s.executeFirewallAction,
					check:    s.executeCheck,
					probe:    s.executeProbe,
					sequencePause: func() {
						time.Sleep(300 * time.Millisecond)
					},
				})
				if step.Action != nil && step.Action.Type == "firewall" && result.Passed {
					if err := s.waitForFirewallPolicy(); err != nil {
						result.Passed = false
						result.Detail += "; dataplane: " + err.Error()
					}
				}
			}
			if step.Action != nil {
				// After state-changing commands, wait for effects to propagate.
				if step.Action.Type == "command" {
					if step.Action.Command == "inject_fault" || step.Action.Command == "disable_reclose" {
						time.Sleep(2 * time.Second)
					}
					if step.Action.Command == "set_tap" {
						time.Sleep(1 * time.Second)
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

			// Pause between steps — longer after commands to let state propagate
			if step.Action != nil && (step.Action.Type == "command" || step.Action.Type == "sequence") {
				time.Sleep(1500 * time.Millisecond)
			} else {
				time.Sleep(200 * time.Millisecond)
			}
		}

		// Reset after scenario
		postResetProblems := s.resetLabState()
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

	c.JSON(http.StatusOK, testSuiteResult{
		Scenarios:     results,
		TotalTests:    totalTests,
		Passed:        totalPassed,
		Failed:        totalFailed,
		AutoPassed:    autoPassed,
		ResetFailures: countResetFailures(results),
		DurationMs:    time.Since(suiteStart).Milliseconds(),
	})
}

// resetLabState restores all devices to defaults.
func (s *Server) resetLabState() []string {
	var problems []string
	warnings, err := s.applyFirewallConfigInternal("weak")
	if err != nil {
		problems = append(problems, "firewall apply: "+err.Error())
	} else {
		for _, warning := range warnings {
			problems = append(problems, "firewall apply warning: "+warning)
		}
	}

	for _, cmd := range resetDeviceCommands {
		result := s.executeCommand(cmd.device, cmd.command, "reset-script", nil)
		if !result.Success {
			problems = append(problems, fmt.Sprintf("%s/%s: %s", cmd.device, cmd.command, result.Detail))
		}
	}

	tapZero := float64(0)
	tapResult := s.executeCommand("regulator", "set_tap", "reset-script", &tapZero)
	if !tapResult.Success {
		problems = append(problems, "regulator/set_tap: "+tapResult.Detail)
	}

	// Clear PCAP captures so validators don't see stale files
	s.pcapMu.Lock()
	s.pcap.FileReady = false
	s.pcapMu.Unlock()
	if dockerCli := s.orchestrator.DockerClient(); dockerCli != nil {
		execCfg := container.ExecOptions{
			Cmd: []string{"sh", "-c", "rm -f /data/captures/*.pcap /tmp/capture*.pcap 2>/dev/null; true"},
		}
		execID, err := dockerCli.ContainerExecCreate(context.Background(), firewallContainer, execCfg)
		if err != nil {
			problems = append(problems, "clear PCAP captures: "+err.Error())
		} else if err := dockerCli.ContainerExecStart(context.Background(), execID.ID, container.ExecStartOptions{}); err != nil {
			problems = append(problems, "clear PCAP captures (start): "+err.Error())
		}
	} else {
		problems = append(problems, "clear PCAP captures: Docker client not configured")
	}

	time.Sleep(500 * time.Millisecond)
	return problems
}
