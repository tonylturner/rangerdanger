package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Allowed command prefixes for the exec endpoint.
var allowedCommands = []string{
	"nmap", "mbpoll", "dnp3poll", "dnp3cmd", "curl", "tshark",
	"tcpdump", "nc", "ping", "traceroute", "wget", "cat", "ls",
	"ip", "ss", "netstat",
}

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

	nodeConfig, err := s.resolveWorkshopNode(nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Determine container name
	containerName := nodeConfig.Container
	if containerName == "" {
		containerName = "rangerdanger-" + strings.ReplaceAll(nodeConfig.ID, "_", "-")
	}

	timeout := req.TimeoutSec
	if timeout <= 0 {
		timeout = 30
	}

	start := time.Now()

	// Execute via shell so pipes and args work
	cmd := []string{"/bin/sh", "-c", req.Command}
	stdout, stderr, exitCode, err := s.orchestrator.ExecCommand(c.Request.Context(), containerName, cmd, timeout)
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

func isAllowedCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	for _, prefix := range allowedCommands {
		if strings.HasPrefix(trimmed, prefix+" ") || trimmed == prefix {
			return true
		}
	}
	return false
}
