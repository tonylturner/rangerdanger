package server

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func commandExecutionError(exitCode int, output string) error {
	if exitCode == 0 {
		return nil
	}
	detail := strings.TrimSpace(output)
	if detail == "" {
		return fmt.Errorf("command exited with status %d", exitCode)
	}
	return fmt.Errorf("command exited with status %d: %s", exitCode, detail)
}

// execCapture runs cmd in containerName, returns combined stdout, the exit
// code, and any error. Blocks until the command exits.
func (s *Server) execCapture(ctx context.Context, cli *client.Client, containerName string, cmd []string) (string, int, error) {
	execID, err := cli.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd: cmd, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return "", -1, err
	}
	att, err := cli.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", -1, err
	}
	defer att.Close()
	var outBuf, errBuf bytes.Buffer
	_, copyErr := stdcopy.StdCopy(&outBuf, &errBuf, att.Reader) // blocks until exec exits
	output := outBuf.String() + errBuf.String()
	if copyErr != nil {
		return output, -1, fmt.Errorf("read exec output: %w", copyErr)
	}
	insp, err := cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return output, -1, err
	}
	return output, insp.ExitCode, nil
}
