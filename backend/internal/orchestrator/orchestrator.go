package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Orchestrator runs commands and shells in the range's containers. The
// range's lifecycle belongs to Compose (internal/lifecycle); this package
// never creates, starts or removes containers.
type Orchestrator struct {
	dockerClient   DockerAPI
	concreteClient *client.Client
}

// New creates an orchestrator with a Docker SDK client from the
// environment. Without one, DockerClient returns nil and every call fails.
func New() *Orchestrator {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("WARNING: Docker client init failed: %v", err)
		return &Orchestrator{}
	}
	return &Orchestrator{dockerClient: cli, concreteClient: cli}
}

// DockerClient returns the underlying Docker client for direct API access.
// Returns nil if Docker is not available.
func (o *Orchestrator) DockerClient() *client.Client {
	return o.concreteClient
}

// ExecShell executes an interactive shell in a container and returns
// the hijacked connection along with the exec ID (needed for resize).
// applianceCLI starts the containd CLI: the caller sets it for the range's
// firewall role.
func (o *Orchestrator) ExecShell(ctx context.Context, containerID string, applianceCLI bool) (types.HijackedResponse, string, error) {
	if o.dockerClient == nil {
		return types.HijackedResponse{}, "", fmt.Errorf("docker client not available")
	}

	// The firewall container lands directly in the containd appliance CLI
	// — that's the experience labs are written against (`show running-config`,
	// `set firewall rule`, `commit`, etc.). Type `shell` from the CLI to drop
	// to bash and `exit` to return. If the CLI errors (e.g., daemon not yet
	// up, stale users.db), the wrapper falls through to bash so students
	// aren't locked out of low-level diagnostics.
	//
	// All other containers default to bash (or sh fallback). The -il flags
	// make bash read /etc/profile and ~/.bashrc so PS1 and aliases are set.
	var cmd []string
	if applianceCLI {
		cmd = []string{"sh", "-c", "containd cli; exec bash -il || exec sh -i"}
	} else {
		cmd = []string{"sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash -il || exec sh -i"}
	}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Env:          []string{"TERM=xterm-256color"},
	}

	execID, err := o.dockerClient.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return types.HijackedResponse{}, "", fmt.Errorf("exec create: %w", err)
	}

	hijack, err := o.dockerClient.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{Tty: true})
	if err != nil {
		return types.HijackedResponse{}, "", fmt.Errorf("exec attach: %w", err)
	}
	return hijack, execID.ID, nil
}

// ResizeExec resizes the PTY of a running exec session.
func (o *Orchestrator) ResizeExec(ctx context.Context, execID string, cols, rows uint) error {
	if o.dockerClient == nil {
		return fmt.Errorf("docker client not available")
	}
	return o.dockerClient.ContainerExecResize(ctx, execID, container.ResizeOptions{
		Width:  cols,
		Height: rows,
	})
}

// ExecCommand runs a command non-interactively in a container and returns stdout/stderr.
func (o *Orchestrator) ExecCommand(ctx context.Context, containerName string, cmd []string, timeoutSec int) (string, string, int, error) {
	if o.dockerClient == nil {
		return "", "", -1, fmt.Errorf("docker client not available")
	}

	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if timeoutSec > 60 {
		timeoutSec = 60
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
	}

	execID, err := o.dockerClient.ContainerExecCreate(execCtx, containerName, execConfig)
	if err != nil {
		return "", "", -1, fmt.Errorf("exec create: %w", err)
	}

	resp, err := o.dockerClient.ContainerExecAttach(execCtx, execID.ID, container.ExecStartOptions{Tty: false})
	if err != nil {
		return "", "", -1, fmt.Errorf("exec attach: %w", err)
	}
	defer resp.Close()

	// Non-TTY output is multiplexed into stdout and stderr frames.
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, resp.Reader); err != nil && execCtx.Err() == nil {
		return "", "", -1, fmt.Errorf("read output: %w", err)
	}

	// Get exit code
	inspect, err := o.dockerClient.ContainerExecInspect(ctx, execID.ID)
	exitCode := -1
	if err == nil {
		exitCode = inspect.ExitCode
	}

	return stdout.String(), stderr.String(), exitCode, nil
}
