package server

import (
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

// handleWorkshopReset restores the lab to its default state:
// weak firewall config, all devices in normal operating condition.
func (s *Server) handleWorkshopReset(c *gin.Context) {
	ctx := c.Request.Context()
	gen := rangeOf(c)
	recipe, err := recipeFor(gen)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	firewall, err := firewallContainer(gen)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var actions []resetAction

	// 1. Apply weak firewall config
	_, err = s.applyFirewallConfigInternal(ctx, gen, "weak")
	actions = append(actions, resetAction{
		Action:  "Apply weak firewall baseline",
		Success: err == nil,
		Detail:  boolDetail(err == nil, "weak config applied", errStr(err)),
	})

	// 2. Reset all field devices via RTAC commands
	for _, cmd := range recipe.resetCommands {
		result := s.executeCommand(ctx, gen, cmd.device, cmd.command, "reset-script", cmd.value)
		actions = append(actions, resetAction{
			Action:  cmd.desc,
			Success: result.Success,
			Detail:  result.Detail,
		})
	}

	// Clear PCAP captures so validators reflect fresh state
	s.pcapMu.Lock()
	s.pcap.FileReady = false
	s.pcapMu.Unlock()
	if dockerCli := s.orchestrator.DockerClient(); dockerCli != nil {
		execCfg := container.ExecOptions{
			Cmd: []string{"sh", "-c", "rm -f /data/captures/*.pcap /tmp/capture*.pcap 2>/dev/null; true"},
		}
		execID, err := dockerCli.ContainerExecCreate(ctx, firewall, execCfg)
		if err == nil {
			dockerCli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{})
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
		credExecID, credErr := dockerCli.ContainerExecCreate(ctx, firewall, credCfg)
		if credErr == nil {
			dockerCli.ContainerExecStart(ctx, credExecID.ID, container.ExecStartOptions{})
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
