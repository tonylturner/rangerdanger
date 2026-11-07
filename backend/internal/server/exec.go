package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

type execRequest struct {
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeout_sec"`
}

type execResponse struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

// handleWorkshopExec runs a command non-interactively on a workshop node container.
func (s *Server) handleWorkshopExec(c *gin.Context) {
	nodeID := c.Param("nodeId")

	var req execRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Command == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "command is required"})
		return
	}

	// Validate command against allowlist
	if !isAllowedCommand(req.Command) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "command not allowed",
			"hint":  "only network analysis and OT protocol tools are permitted",
		})
		return
	}

	svc, err := nodeService(rangeOf(c), nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	start := time.Now()
	stdout, stderr, exitCode, err := s.runShellCommand(c.Request.Context(), svc.Container, req.Command, req.TimeoutSec)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "exec failed",
			"detail": err.Error(),
		})
		return
	}

	duration := time.Since(start).Milliseconds()

	c.JSON(http.StatusOK, execResponse{
		Stdout:     stdout,
		Stderr:     stderr,
		ExitCode:   exitCode,
		DurationMs: duration,
	})
}

// isAllowedCommand defers to the labs allowlist so the exec endpoint and a
// check step's authored run commands accept exactly the same tools.
func isAllowedCommand(cmd string) bool {
	return labs.IsAllowedCommand(cmd)
}

// runShellCommand runs one command in a container through the shared exec
// path: a /bin/sh -c invocation with the Run-button default timeout. The
// workshop exec endpoint and the test suite's replayed wire commands both use
// it, so there is no second exec implementation.
func (s *Server) runShellCommand(ctx context.Context, containerName, command string, timeoutSec int) (string, string, int, error) {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return s.execInContainer(ctx, containerName, []string{"/bin/sh", "-c", command}, timeoutSec)
}

// execOnNode resolves a package node to its container and runs the command
// through runShellCommand. The test suite uses it to replay a check step's
// authored wire commands on the node the student runs them from.
func (s *Server) execOnNode(ctx context.Context, gen *lifecycle.Generation, nodeID, command string, timeoutSec int) (string, string, int, error) {
	svc, err := nodeService(gen, nodeID)
	if err != nil {
		return "", "", 0, err
	}
	return s.runShellCommand(ctx, svc.Container, command, timeoutSec)
}
