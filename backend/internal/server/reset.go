package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type resetAction struct {
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
}

// handleWorkshopReset restores the lab to its default state: the package's
// default firewall policy, all devices in normal operating condition.
func (s *Server) handleWorkshopReset(c *gin.Context) {
	ctx := c.Request.Context()
	gen := rangeOf(c)
	recipe, ok := supportedRecipe(c, func(*packageRecipe) bool { return true })
	if !ok {
		return
	}
	firewall, err := firewallContainer(gen)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	policy, err := defaultPolicy(gen)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var actions []resetAction

	// 1. Apply the default firewall policy
	_, err = s.applyFirewallConfigInternal(ctx, gen, policy)
	actions = append(actions, resetAction{
		Action:  "Apply " + policy + " firewall baseline",
		Success: err == nil,
		Detail:  boolDetail(err == nil, policy+" config applied", errStr(err)),
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
	if recipe.resetTruth != nil {
		actions = append(actions, recipe.resetTruth(ctx, gen, recipe)...)
	}

	// Clear PCAP captures so validators reflect fresh state
	s.pcapMu.Lock()
	s.pcap.FileReady = false
	s.pcapMu.Unlock()
	if dockerCli := s.orchestrator.DockerClient(); dockerCli != nil {
		err := runResetCleanup(ctx, s.orchestrator, firewall, "rm -f /data/captures/*.pcap /tmp/capture*.pcap")
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
		credErr := runResetCleanup(ctx, s.orchestrator, firewall, "rm -f /data/users.db /data/sessions.db")
		actions = append(actions, resetAction{
			Action:  "Reset containd credentials to default",
			Success: credErr == nil,
			Detail: boolDetail(credErr == nil,
				"users.db cleared (firewall restart required for changes to take effect; with containd >= v0.1.22 lab mode this is a no-op)",
				errStr(credErr)),
		})
	}

	// The truth reset already waits for acknowledged measurement delivery.
	if recipe.resetTruth == nil {
		time.Sleep(500 * time.Millisecond)
	}

	// Check overall success
	allSuccess := true
	for _, a := range actions {
		if !a.Success {
			allSuccess = false
			break
		}
	}

	status := http.StatusOK
	if recipe.resetTruth != nil && !allSuccess {
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{
		"success": allSuccess,
		"actions": actions,
	})
}

type resetCommandRunner interface {
	ExecCommand(context.Context, string, []string, int) (string, string, int, error)
}

// runResetCleanup checks both Docker transport errors and the command's
// exit status. A created exec is not evidence that cleanup succeeded.
func runResetCleanup(ctx context.Context, runner resetCommandRunner, container, command string) error {
	_, stderr, code, err := runner.ExecCommand(ctx, container, []string{"sh", "-c", command}, 5)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("reset cleanup exited %d: %s", code, strings.TrimSpace(stderr))
	}
	return nil
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
