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
// match the just-committed policy. The backend's activeConfig flips at commit
// time, so polling it would not establish that the running policy is visible.
func (s *Server) waitForFirewallPolicy() error {
	s.activeConfigMu.RLock()
	want := s.lastAppliedHash
	s.activeConfigMu.RUnlock()
	if want == "" {
		return fmt.Errorf("no recorded firewall hash")
	}
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for {
		got, err := s.containdClient.GetFirewallHash()
		if err == nil && got == want {
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("running firewall policy not active: %w", lastErr)
			}
			return fmt.Errorf("running firewall policy not active after 15 s")
		}
		time.Sleep(200 * time.Millisecond)
	}
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

	// Load all scenarios ordered by `order`
	var scenarios []models.Scenario
	if err := s.db.Order("\"order\" ASC, name ASC").Find(&scenarios).Error; err != nil {
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
		time.Sleep(1 * time.Second)

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
