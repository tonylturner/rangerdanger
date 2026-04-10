package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type resetAction struct {
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
}

// handleWorkshopReset restores the lab to its default state:
// weak firewall config, all devices in normal operating condition.
func (s *Server) handleWorkshopReset(c *gin.Context) {
	var actions []resetAction

	// 1. Apply weak firewall config
	err := s.applyFirewallConfigInternal("weak")
	actions = append(actions, resetAction{
		Action:  "Apply weak firewall baseline",
		Success: err == nil,
		Detail:  boolDetail(err == nil, "weak config applied", errStr(err)),
	})

	// 2. Reset all field devices via RTAC commands
	resetCommands := []struct {
		device  string
		command string
		desc    string
	}{
		{"relay", "clear_fault", "Clear relay faults"},
		{"relay", "unlock", "Unlock relay"},
		{"relay", "close", "Close feeder breaker"},
		{"recloser", "clear_fault", "Clear recloser faults"},
		{"recloser", "reset_lockout", "Reset recloser lockout"},
		{"recloser", "enable_reclose", "Enable auto-reclose"},
		{"recloser", "close", "Close recloser"},
		{"regulator", "set_auto", "Set regulator to auto mode"},
	}

	for _, cmd := range resetCommands {
		result := s.executeCommand(cmd.device, cmd.command, "reset-script", nil)
		actions = append(actions, resetAction{
			Action:  cmd.desc,
			Success: result.Success,
			Detail:  result.Detail,
		})
	}

	// Reset regulator tap to 0 (needs value parameter)
	tapZero := float64(0)
	tapResult := s.executeCommand("regulator", "set_tap", "reset-script", &tapZero)
	actions = append(actions, resetAction{
		Action:  "Reset regulator tap to 0",
		Success: tapResult.Success,
		Detail:  tapResult.Detail,
	})

	// Wait for state propagation
	time.Sleep(500 * time.Millisecond)

	// Check overall success
	allSuccess := true
	for _, a := range actions {
		if !a.Success {
			allSuccess = false
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": allSuccess,
		"actions": actions,
	})
}

func boolDetail(ok bool, success, failure string) string {
	if ok {
		return success
	}
	return failure
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
