package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// StepExecutionResult is the response from executing a scenario step action.
type StepExecutionResult struct {
	Containd   *containd.ApplyReceipt `json:"containd,omitempty"`
	StepIndex  int                    `json:"step_index"`
	StepTitle  string                 `json:"step_title"`
	ActionType string                 `json:"action_type"`
	Success    bool                   `json:"success"`
	Results    []StepActionResult     `json:"results"`
	Timestamp  string                 `json:"timestamp"`
}

// StepActionResult is the result of a single action within a step.
type StepActionResult struct {
	Containd   *containd.ApplyReceipt `json:"containd,omitempty"`
	applyError error

	Action   string   `json:"action"`
	Success  bool     `json:"success"`
	Blocked  bool     `json:"-"`
	Detail   string   `json:"detail"`
	Impact   string   `json:"impact,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// handleExecuteStep executes the action defined in a scenario step.
// POST /api/scenarios/:id/steps/:stepIdx/execute
func (s *Server) handleExecuteStep(c *gin.Context) {
	ctx := c.Request.Context()
	gen := rangeOf(c)
	scenarioID := c.Param("id")
	stepIdxStr := c.Param("stepIdx")
	stepIdx, err := strconv.Atoi(stepIdxStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid step index"})
		return
	}

	scenario, err := s.findActiveScenario(scenarioID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}

	// Parse steps
	var steps []labs.ScenarioStep
	if err := json.Unmarshal([]byte(scenario.Steps), &steps); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse steps"})
		return
	}

	if stepIdx < 0 || stepIdx >= len(steps) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "step index out of range"})
		return
	}

	step := steps[stepIdx]
	if step.Action == nil {
		c.JSON(http.StatusOK, StepExecutionResult{
			StepIndex:  stepIdx,
			StepTitle:  step.Title,
			ActionType: "manual",
			Success:    true,
			Results:    []StepActionResult{{Action: "manual", Success: true, Detail: "Manual step — no automated action"}},
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	var results []StepActionResult

	switch step.Action.Type {
	case "command":
		result := s.executeCommand(ctx, gen, step.Action.Device, step.Action.Command, step.Action.Source, step.Action.Value)
		results = append(results, result)

	case "sequence":
		for _, cmd := range step.Action.Commands {
			result := s.executeCommand(ctx, gen, cmd.Device, cmd.Command, cmd.Source, cmd.Value)
			results = append(results, result)
			if !result.Success {
				break
			}
			time.Sleep(500 * time.Millisecond) // brief pause between commands
		}

	case "firewall":
		result := s.executeFirewallAction(ctx, gen, step.Action.Config)
		results = append(results, result)

	case "check":
		checkResults := s.executeCheck(ctx, gen, step.Action.Expect)
		results = append(results, checkResults...)

	case "session-cut":
		results = append(results, s.executeSessionCut(ctx, gen, step.Action.Flow))

	case "probe":
		results = s.executeProbe(ctx, gen, step)

	default:
		results = append(results, StepActionResult{
			Action:  step.Action.Type,
			Success: false,
			Detail:  fmt.Sprintf("Unknown action type: %s", step.Action.Type),
		})
	}

	allSuccess := true
	for _, r := range results {
		if !r.Success {
			allSuccess = false
			break
		}
	}

	var receipt *containd.ApplyReceipt
	for _, result := range results {
		if result.Containd != nil {
			receipt = result.Containd
		}
		if result.applyError != nil {
			writeApplyError(c, result.applyError, &containd.ApplyResult{Receipt: receipt}, gin.H{"success": false, "results": results, "step_index": stepIdx, "step_title": step.Title, "action_type": step.Action.Type, "timestamp": time.Now().UTC().Format(time.RFC3339)})
			return
		}
	}
	c.JSON(http.StatusOK, StepExecutionResult{
		Containd:   receipt,
		StepIndex:  stepIdx,
		StepTitle:  step.Title,
		ActionType: step.Action.Type,
		Success:    allSuccess,
		Results:    results,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})
}

const rtacCommandResponseLimit = 1 << 20

// executeRTACCommand evaluates the actual running L4 policy before simulating
// a source's command through the RTAC transport. A missing identity is untested.
func (s *Server) executeRTACCommand(ctx context.Context, gen *lifecycle.Generation, _ *packageRecipe, device, command, source string, value *float64) StepActionResult {
	verdict, err := s.commandVerdict(ctx, gen, device, command, source)
	if err != nil {
		return StepActionResult{Action: fmt.Sprintf("%s/%s from %s", device, command, source), Detail: "Cannot evaluate running policy: " + err.Error()}
	}
	if verdict == labs.VerdictDeny {
		return StepActionResult{Action: fmt.Sprintf("%s/%s from %s", device, command, source), Blocked: true, Detail: "BLOCKED by running L4 policy for this source → device tuple"}
	}

	payload := map[string]any{
		"command": command,
		"source":  source,
	}
	if value != nil {
		payload["value"] = *value
	}

	body, _ := json.Marshal(payload)
	rtac, err := rtacURL(gen)
	if err != nil {
		return StepActionResult{Action: fmt.Sprintf("%s/%s", device, command), Detail: err.Error()}
	}
	resp, err := rtacRequest(ctx, http.MethodPost, rtac+"/api/command/"+device, bytes.NewReader(body))
	if err != nil {
		return StepActionResult{
			Action:  fmt.Sprintf("%s/%s", device, command),
			Success: false,
			Detail:  "Cannot reach RTAC: " + err.Error(),
		}
	}
	defer resp.Body.Close()

	action := StepActionResult{Action: fmt.Sprintf("%s/%s from %s", device, command, source)}
	// Read one extra byte so a valid JSON prefix cannot hide an oversized body.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, rtacCommandResponseLimit+1))
	if err != nil {
		action.Detail = fmt.Sprintf("RTAC returned HTTP %d: cannot read response: %v", resp.StatusCode, err)
		return action
	}
	if len(respBody) > rtacCommandResponseLimit {
		action.Detail = fmt.Sprintf("RTAC returned HTTP %d: response exceeds %d bytes", resp.StatusCode, rtacCommandResponseLimit)
		return action
	}
	var result map[string]any
	decodeErr := json.Unmarshal(respBody, &result)

	resultStr, _ := result["result"].(string)
	detail, _ := result["detail"].(string)
	impact, _ := result["process_impact"].(string)

	action.Success = resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices &&
		decodeErr == nil && (resultStr == "executed" || resultStr == "success")
	action.Detail = detail
	action.Impact = impact
	if !action.Success {
		action.Detail = fmt.Sprintf("RTAC returned HTTP %d", resp.StatusCode)
		if decodeErr != nil {
			action.Detail += ": invalid JSON response: " + decodeErr.Error()
		}
		if detail != "" {
			action.Detail += ": " + detail
		}
	}
	return action
}

// executeFirewallAction applies a firewall configuration.
func (s *Server) executeFirewallAction(ctx context.Context, gen *lifecycle.Generation, configName string) StepActionResult {
	applied, err := s.applyFirewallConfigInternal(ctx, gen, configName)
	if err != nil {
		var receipt *containd.ApplyReceipt
		if applied != nil {
			receipt = applied.Receipt
		}
		return StepActionResult{
			Containd: receipt, applyError: err,
			Action:  "Apply firewall: " + configName,
			Success: false,
			Detail:  err.Error(),
		}
	}

	warnings := applied.Warnings
	label := configName
	if recipe, err := recipeFor(gen); err == nil {
		label = recipe.policyLabel(configName)
	}
	detail := "containd policy set to " + label
	if len(warnings) > 0 {
		// Surface containd's commit warnings so a partial-apply (e.g. nft
		// operation not permitted) is visible in the step result instead
		// of silently flagged as success.
		detail += " (warnings: " + strings.Join(warnings, "; ") + ")"
	}
	return StepActionResult{
		Containd: applied.Receipt,
		Action:   "Apply firewall: " + configName,
		Success:  true,
		Detail:   detail,
		Warnings: warnings,
	}
}

// executeCheck validates current substation state against expectations.
func (s *Server) executeCheck(ctx context.Context, gen *lifecycle.Generation, expect map[string]any) []StepActionResult {
	recipe, err := recipeFor(gen)
	if err != nil {
		return []StepActionResult{{Action: "Check", Detail: err.Error()}}
	}
	if recipe.check == nil {
		return []StepActionResult{{Action: "Check", Detail: "checks not supported by this package"}}
	}
	return recipe.check(s, ctx, gen, expect)
}

func (s *Server) executeUSCheck(ctx context.Context, gen *lifecycle.Generation, expect map[string]any) []StepActionResult {
	var results []StepActionResult

	state, err := s.fetchRTACState(ctx, gen)
	if err != nil {
		return []StepActionResult{{
			Action:  "Check substation state",
			Success: false,
			Detail:  "Cannot reach RTAC: " + err.Error(),
		}}
	}

	elec := mapGet(state, "electrical")
	devices := mapGet(state, "devices")
	recloser := mapGet(devices, "recloser")

	for key, expectedVal := range expect {
		switch key {
		case "breaker_closed":
			actual := boolGet(elec, "breaker_closed")
			expected := expectedVal == true
			if actual == expected {
				if expected {
					results = append(results, StepActionResult{Action: "Breaker status", Success: true, Detail: "Feeder breaker is CLOSED — loads energized"})
				} else {
					results = append(results, StepActionResult{Action: "Breaker status", Success: true, Detail: "Feeder breaker is OPEN — as expected after attack"})
				}
			} else {
				results = append(results, StepActionResult{Action: "Breaker status", Success: false, Detail: fmt.Sprintf("Expected breaker_closed=%v but got %v", expected, actual)})
			}

		case "loads_energized":
			critOk := boolGet(elec, "critical_load_energized")
			genOk := boolGet(elec, "general_load_energized")
			expected := expectedVal == true
			actual := critOk && genOk
			if actual == expected {
				results = append(results, StepActionResult{Action: "Load status", Success: true, Detail: "All loads energized"})
			} else {
				results = append(results, StepActionResult{Action: "Load status", Success: false, Detail: "Loads not in expected state"})
			}

		case "recloser_closed":
			actual := boolGet(elec, "recloser_closed")
			expected := expectedVal == true
			if actual == expected {
				results = append(results, StepActionResult{Action: "Recloser status", Success: true, Detail: fmt.Sprintf("Recloser %s as expected", boolLabel(actual, "CLOSED", "OPEN"))})
			} else {
				results = append(results, StepActionResult{Action: "Recloser status", Success: false, Detail: fmt.Sprintf("Expected recloser %s", boolLabel(expected, "CLOSED", "OPEN"))})
			}

		case "reclose_enabled":
			actual := boolGet(recloser, "reclose_enabled")
			expected := expectedVal == true
			if actual == expected {
				results = append(results, StepActionResult{Action: "Auto-reclose", Success: true, Detail: fmt.Sprintf("Auto-reclose %s", boolLabel(actual, "ENABLED", "DISABLED"))})
			} else {
				results = append(results, StepActionResult{Action: "Auto-reclose", Success: false, Detail: fmt.Sprintf("Expected auto-reclose %s", boolLabel(expected, "ENABLED", "DISABLED"))})
			}

		case "voltage_normal":
			critV := floatGet(elec, "critical_load_voltage_v")
			normal := critV >= 114 && critV <= 126
			expected := expectedVal == true
			if normal == expected {
				results = append(results, StepActionResult{Action: "Voltage quality", Success: true, Detail: fmt.Sprintf("%.1fV — %s", critV, boolLabel(normal, "within normal range", "outside normal range"))})
			} else {
				results = append(results, StepActionResult{Action: "Voltage quality", Success: false, Detail: fmt.Sprintf("%.1fV — expected %s", critV, boolLabel(expected, "normal", "abnormal"))})
			}

		case "firewall_config":
			expected, _ := expectedVal.(string)
			if expected == "improved" {
				judgment := s.usPolicyJudgment(ctx, gen)
				results = append(results, StepActionResult{Action: judgment.Name, Success: judgment.Status == "pass", Detail: judgment.Detail})
			} else {
				s.activeConfigMu.RLock()
				actual := s.activeConfig
				s.activeConfigMu.RUnlock()
				results = append(results, StepActionResult{Action: "Firewall policy", Success: actual == expected, Detail: "Active config: " + actual})
			}
		}
	}

	if len(results) == 0 {
		results = append(results, StepActionResult{Action: "Check", Success: true, Detail: "No conditions to verify"})
	}

	return results
}

func boolLabel(v bool, trueLabel, falseLabel string) string {
	if v {
		return trueLabel
	}
	return falseLabel
}
