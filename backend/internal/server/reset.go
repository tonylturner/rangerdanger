package server

import (
	"context"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/gin-gonic/gin"
)

type resetAction struct {
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
}

// resetDeviceCommands is the canonical list of (device, command) pairs the
// workshop reset and the test-runner replay against the sims to restore
// default state. Every command here must be a real handler in the
// corresponding sim's main.go switch — TestResetCommandsAreSupported pins
// that contract so a typo (or a sim-handler rename) fails CI instead of
// surfacing as a silent "success: false" at workshop time.
var resetDeviceCommands = []struct {
	device, command, desc string
}{
	{"relay", "clear_fault", "Clear relay faults"},
	{"relay", "unlock", "Unlock relay"},
	{"relay", "close", "Close feeder breaker"},
	{"recloser", "clear_fault", "Clear recloser faults"},
	{"recloser", "reset_lockout", "Reset recloser lockout"},
	{"recloser", "enable_reclose", "Enable auto-reclose"},
	{"recloser", "close", "Close recloser"},
	{"regulator", "set_auto", "Set regulator to auto mode"},
	// capbank: reset_lockout already clears the alarm flag (see
	// services/capbank-sim/main.go reset_lockout handler), so no
	// separate clear_alarm command exists.
	{"capbank", "reset_lockout", "Reset capbank lockout"},
	{"capbank", "switch_in", "Switch capbank in"},
	{"capbank", "set_auto", "Set capbank to auto mode"},
}

// handleWorkshopReset restores the lab to its default state:
// weak firewall config, all devices in normal operating condition.
func (s *Server) handleWorkshopReset(c *gin.Context) {
	var actions []resetAction

	// 1. Apply weak firewall config
	_, err := s.applyFirewallConfigInternal("weak")
	actions = append(actions, resetAction{
		Action:  "Apply weak firewall baseline",
		Success: err == nil,
		Detail:  boolDetail(err == nil, "weak config applied", errStr(err)),
	})

	// 2. Reset all field devices via RTAC commands
	for _, cmd := range resetDeviceCommands {
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

	// Clear PCAP captures so validators reflect fresh state
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
		actions = append(actions, resetAction{
			Action:  "Clear PCAP captures",
			Success: err == nil,
			Detail:  boolDetail(err == nil, "capture files removed", errStr(err)),
		})

		// Defensive credential reset for containd. With containd
		// v0.1.22+ in lab mode, password change is locked at the API
		// — students can't drift the canonical `containd/containd`
		// credential via the UI or via SSH. This step is for the
		// edge case where someone hit a pre-v0.1.22 containd directly
		// on :9080 and changed the password before pulling the new
		// image: wiping users.db lets containd reseed the default on
		// its next restart. The wipe is a no-op on a clean stack
		// (file is already containing the default cred) so this is
		// always safe to run from Reset Lab.
		credCfg := container.ExecOptions{
			Cmd: []string{"sh", "-c", "rm -f /data/users.db /data/sessions.db 2>/dev/null; true"},
		}
		credExecID, credErr := dockerCli.ContainerExecCreate(context.Background(), firewallContainer, credCfg)
		if credErr == nil {
			dockerCli.ContainerExecStart(context.Background(), credExecID.ID, container.ExecStartOptions{})
		}
		actions = append(actions, resetAction{
			Action:  "Reset containd credentials to default",
			Success: credErr == nil,
			Detail: boolDetail(credErr == nil,
				"users.db cleared (firewall restart required for changes to take effect; with containd >= v0.1.22 lab mode this is a no-op)",
				errStr(credErr)),
		})
	}

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
