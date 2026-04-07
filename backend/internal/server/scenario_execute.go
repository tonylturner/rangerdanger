package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

// StepExecutionResult is the response from executing a scenario step action.
type StepExecutionResult struct {
	StepIndex int               `json:"step_index"`
	StepTitle string            `json:"step_title"`
	ActionType string           `json:"action_type"`
	Success   bool              `json:"success"`
	Results   []StepActionResult `json:"results"`
	Timestamp string            `json:"timestamp"`
}

// StepActionResult is the result of a single action within a step.
type StepActionResult struct {
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
	Impact  string `json:"impact,omitempty"`
}

// handleExecuteStep executes the action defined in a scenario step.
// POST /api/scenarios/:id/steps/:stepIdx/execute
func (s *Server) handleExecuteStep(c *gin.Context) {
	scenarioID := c.Param("id")
	stepIdxStr := c.Param("stepIdx")
	stepIdx, err := strconv.Atoi(stepIdxStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid step index"})
		return
	}

	// Load scenario
	var scenario models.Scenario
	if err := s.db.First(&scenario, "id = ?", scenarioID).Error; err != nil {
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
		result := s.executeCommand(step.Action.Device, step.Action.Command, step.Action.Source, step.Action.Value)
		results = append(results, result)

	case "sequence":
		for _, cmd := range step.Action.Commands {
			result := s.executeCommand(cmd.Device, cmd.Command, cmd.Source, cmd.Value)
			results = append(results, result)
			if !result.Success {
				break
			}
			time.Sleep(500 * time.Millisecond) // brief pause between commands
		}

	case "firewall":
		result := s.executeFirewallAction(step.Action.Config)
		results = append(results, result)

	case "check":
		checkResults := s.executeCheck(step.Action.Expect)
		results = append(results, checkResults...)

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

	c.JSON(http.StatusOK, StepExecutionResult{
		StepIndex:  stepIdx,
		StepTitle:  step.Title,
		ActionType: step.Action.Type,
		Success:    allSuccess,
		Results:    results,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})
}

// executeCommand sends a command to a field device via RTAC.
// When the improved firewall config is active, it enforces source authorization:
// only the RTAC and operator sources can reach field devices.
func (s *Server) executeCommand(device, command, source string, value *float64) StepActionResult {
	// Check if the source is authorized under the current firewall policy.
	// In "improved" mode, only RTAC (10.30.30.20 / 10.40.40.10) and "operator"
	// can send commands to field devices — all other sources are blocked by containd.
	s.activeConfigMu.RLock()
	activeConfig := s.activeConfig
	s.activeConfigMu.RUnlock()

	if activeConfig == "improved" && !isAuthorizedSource(source) {
		zone := sourceZoneLabel(source)
		return StepActionResult{
			Action:  fmt.Sprintf("%s/%s from %s", device, command, source),
			Success: false,
			Detail:  fmt.Sprintf("BLOCKED by containd — %s not authorized to reach field devices", source),
			Impact:  fmt.Sprintf("containd NGFW blocked %s→%s traffic [%s zone denied]", source, device, zone),
		}
	}

	payload := map[string]any{
		"command": command,
		"source":  source,
	}
	if value != nil {
		payload["value"] = *value
	}

	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(
		s.rtacURL()+"/api/command/"+device,
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return StepActionResult{
			Action:  fmt.Sprintf("%s/%s", device, command),
			Success: false,
			Detail:  "Cannot reach RTAC: " + err.Error(),
		}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)

	resultStr, _ := result["result"].(string)
	detail, _ := result["detail"].(string)
	impact, _ := result["process_impact"].(string)

	succeeded := resultStr == "executed" || resultStr == "success"

	return StepActionResult{
		Action:  fmt.Sprintf("%s/%s from %s", device, command, source),
		Success: succeeded,
		Detail:  detail,
		Impact:  impact,
	}
}

// isAuthorizedSource returns true if the source is allowed to command field
// devices under the hardened ("improved") firewall policy.
func isAuthorizedSource(source string) bool {
	switch source {
	case "operator", "rtac", "rtac-sim",
		"10.30.30.20", // RTAC on ot_ops_net
		"10.40.40.10": // RTAC on field_net
		return true
	}
	return false
}

// sourceZoneLabel returns a human-readable zone label for a source IP.
func sourceZoneLabel(source string) string {
	switch {
	case strings.HasPrefix(source, "10.10.10."):
		return "enterprise"
	case strings.HasPrefix(source, "10.20.20."):
		return "vendor"
	case strings.HasPrefix(source, "10.30.30."):
		return "ot_ops"
	case strings.HasPrefix(source, "10.40.40."):
		return "field"
	default:
		return "unknown"
	}
}

// executeFirewallAction applies a firewall configuration.
func (s *Server) executeFirewallAction(configName string) StepActionResult {
	err := s.applyFirewallConfigInternal(configName)
	if err != nil {
		return StepActionResult{
			Action:  "Apply firewall: " + configName,
			Success: false,
			Detail:  err.Error(),
		}
	}

	label := "weak baseline"
	if configName == "improved" {
		label = "hardened (RTAC-only field access)"
	}
	return StepActionResult{
		Action:  "Apply firewall: " + configName,
		Success: true,
		Detail:  "containd policy set to " + label,
	}
}

// executeCheck validates current substation state against expectations.
func (s *Server) executeCheck(expect map[string]any) []StepActionResult {
	var results []StepActionResult

	state, err := s.fetchRTACState()
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
			s.activeConfigMu.RLock()
			actual := s.activeConfig
			s.activeConfigMu.RUnlock()
			expected, _ := expectedVal.(string)
			if actual == expected {
				results = append(results, StepActionResult{Action: "Firewall policy", Success: true, Detail: "Active config: " + actual})
			} else {
				results = append(results, StepActionResult{Action: "Firewall policy", Success: false, Detail: fmt.Sprintf("Expected %s but got %s", expected, actual)})
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
