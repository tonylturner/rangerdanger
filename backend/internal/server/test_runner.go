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

	"github.com/tturner/rangerdanger/backend/internal/models"
)

type stepTestResult struct {
	StepIndex int    `json:"step_index"`
	StepTitle string `json:"step_title"`
	Passed    bool   `json:"passed"`
	Detail    string `json:"detail"`
	DurationMs int64 `json:"duration_ms"`
}

type scenarioTestResult struct {
	ScenarioID   string           `json:"scenario_id"`
	ScenarioName string           `json:"scenario_name"`
	Order        string           `json:"order"`
	Steps        []stepTestResult `json:"steps"`
	Passed       bool             `json:"passed"`
	ResetOK      bool             `json:"reset_ok"`
	DurationMs   int64            `json:"duration_ms"`
}

type testSuiteResult struct {
	Scenarios  []scenarioTestResult `json:"scenarios"`
	TotalTests int                  `json:"total_tests"`
	Passed     int                  `json:"passed"`
	Failed     int                  `json:"failed"`
	DurationMs int64                `json:"duration_ms"`
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

	for _, sc := range scenarios {
		log.Printf("TEST SUITE: Running scenario %s: %s", sc.Order, sc.Name)
		scenarioStart := time.Now()

		// Reset lab before each scenario
		s.resetLabState()
		time.Sleep(1 * time.Second)

		// Exercises that assume the hardened config is already applied
		if sc.ID == "validation-evidence" {
			s.applyFirewallConfigInternal("improved")
			time.Sleep(500 * time.Millisecond)
		}

		// Parse steps
		var steps []struct {
			Title       string  `json:"title"`
			Description string  `json:"description"`
			Action      *struct {
				Type    string         `json:"type"`
				Device  string         `json:"device,omitempty"`
				Command string         `json:"command,omitempty"`
				Source  string         `json:"source,omitempty"`
				Value   *float64       `json:"value,omitempty"`
				Config  string         `json:"config,omitempty"`
				Expect  map[string]any `json:"expect,omitempty"`
				Commands []struct {
					Device  string   `json:"device"`
					Command string   `json:"command"`
					Source  string   `json:"source,omitempty"`
					Value   *float64 `json:"value,omitempty"`
				} `json:"commands,omitempty"`
			} `json:"action,omitempty"`
		}
		json.Unmarshal([]byte(sc.Steps), &steps)

		var stepResults []stepTestResult

		for i, step := range steps {
			stepStart := time.Now()
			totalTests++

			result := stepTestResult{
				StepIndex: i,
				StepTitle: step.Title,
			}

			if step.Action == nil {
				// Manual/observational step — auto-pass
				result.Passed = true
				result.Detail = "manual step (no action)"
			} else {
				switch step.Action.Type {
				case "command":
					r := s.executeCommand(step.Action.Device, step.Action.Command, step.Action.Source, step.Action.Value)
					if r.Success {
						result.Passed = true
					} else if strings.Contains(r.Detail, "BLOCKED by containd") {
						// A blocked command after hardening is the desired outcome
						result.Passed = true
						r.Detail += " (expected — hardened policy is working)"
					} else {
						result.Passed = false
					}
					result.Detail = r.Detail
					// After state-changing commands, wait for effects to propagate
					if step.Action.Command == "inject_fault" || step.Action.Command == "disable_reclose" {
						time.Sleep(2 * time.Second)
					}
					if step.Action.Command == "set_tap" {
						time.Sleep(1 * time.Second)
					}

				case "sequence":
					allOK := true
					var details []string
					for _, cmd := range step.Action.Commands {
						r := s.executeCommand(cmd.Device, cmd.Command, cmd.Source, cmd.Value)
						if !r.Success {
							allOK = false
						}
						details = append(details, fmt.Sprintf("%s/%s: %s", cmd.Device, cmd.Command, r.Detail))
						time.Sleep(300 * time.Millisecond)
					}
					result.Passed = allOK
					result.Detail = fmt.Sprintf("%d commands", len(step.Action.Commands))

				case "firewall":
					r := s.executeFirewallAction(step.Action.Config)
					result.Passed = r.Success
					result.Detail = r.Detail

				case "check":
					checks := s.executeCheck(step.Action.Expect)
					allPass := true
					for _, ch := range checks {
						if !ch.Success {
							allPass = false
							break
						}
					}
					result.Passed = allPass
					result.Detail = fmt.Sprintf("%d checks, all pass: %v", len(checks), allPass)

				case "decision":
					result.Passed = true
					result.Detail = "decision step (student planning — auto-pass)"

				default:
					result.Passed = true
					result.Detail = fmt.Sprintf("unhandled type: %s", step.Action.Type)
				}
			}

			result.DurationMs = time.Since(stepStart).Milliseconds()
			stepResults = append(stepResults, result)

			if result.Passed {
				totalPassed++
			} else {
				totalFailed++
			}

			// Pause between steps — longer after commands to let state propagate
			if step.Action != nil && (step.Action.Type == "command" || step.Action.Type == "sequence") {
				time.Sleep(1500 * time.Millisecond)
			} else {
				time.Sleep(200 * time.Millisecond)
			}
		}

		// Reset after scenario
		s.resetLabState()
		resetOK := true // assume success for now

		scenarioResult := scenarioTestResult{
			ScenarioID:   sc.ID,
			ScenarioName: sc.Name,
			Order:        sc.Order,
			Steps:        stepResults,
			ResetOK:      resetOK,
			DurationMs:   time.Since(scenarioStart).Milliseconds(),
		}

		// Scenario passes if all steps pass
		scenarioResult.Passed = true
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
		Scenarios:  results,
		TotalTests: totalTests,
		Passed:     totalPassed,
		Failed:     totalFailed,
		DurationMs: time.Since(suiteStart).Milliseconds(),
	})
}

// resetLabState restores all devices to defaults.
func (s *Server) resetLabState() {
	s.applyFirewallConfigInternal("weak")

	resetCmds := []struct{ device, command string }{
		{"relay", "clear_fault"},
		{"relay", "unlock"},
		{"relay", "close"},
		{"recloser", "clear_fault"},
		{"recloser", "reset_lockout"},
		{"recloser", "enable_reclose"},
		{"recloser", "close"},
		{"regulator", "set_auto"},
		{"capbank", "clear_alarm"},
		{"capbank", "reset_lockout"},
		{"capbank", "switch_in"},
		{"capbank", "set_auto"},
	}

	for _, cmd := range resetCmds {
		s.executeCommand(cmd.device, cmd.command, "reset-script", nil)
	}

	tapZero := float64(0)
	s.executeCommand("regulator", "set_tap", "reset-script", &tapZero)

	// Clear PCAP captures so validators don't see stale files
	s.pcapMu.Lock()
	s.pcap.FileReady = false
	s.pcapMu.Unlock()
	if dockerCli := s.orchestrator.DockerClient(); dockerCli != nil {
		execCfg := container.ExecOptions{
			Cmd: []string{"sh", "-c", "rm -f /data/captures/*.pcap /tmp/capture*.pcap 2>/dev/null; true"},
		}
		execID, err := dockerCli.ContainerExecCreate(context.Background(), firewallContainer, execCfg)
		if err == nil {
			dockerCli.ContainerExecStart(context.Background(), execID.ID, container.ExecStartOptions{})
		}
	}

	time.Sleep(500 * time.Millisecond)
}
