package server

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// All recipe source images (engineering workstation, Kali, vendor jump)
// ship Python 3. Docker context cancellation alone does NOT kill an exec.
// This supervisor owns a private process group, kills it on stdin EOF, and
// reaps it before closing stdout. No container-wide kill or shared PID file.
const trafficSupervisor = `
import os, select, signal, subprocess, sys
p = subprocess.Popen(["sh", "-c", sys.argv[1]], stdin=subprocess.DEVNULL, start_new_session=True)
try:
    while p.poll() is None:
        ready, _, _ = select.select([sys.stdin], [], [], 0.05)
        if ready and not os.read(sys.stdin.fileno(), 1):
            break
finally:
    if p.returncode is None:
        try:
            os.killpg(p.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    p.wait()
sys.exit(p.returncode if p.returncode >= 0 else 128 - p.returncode)
`

func executeTraffic(ctx context.Context, cli *client.Client, target resolvedTraffic) error {
	id, err := cli.ContainerExecCreate(ctx, target.container, container.ExecOptions{
		Cmd:         []string{"python3", "-c", trafficSupervisor, target.cmd},
		AttachStdin: true, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return err
	}
	att, err := cli.ContainerExecAttach(ctx, id.ID, container.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer att.Close()
	cancelled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(cancelled)
		// Half-close delivers EOF to the supervisor; keep reading until
		// it acknowledges child exit. Bound a broken daemon/transport.
		_ = att.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_ = att.CloseWrite()
	})
	defer func() {
		if !stop() {
			<-cancelled
		}
	}()
	if _, err := io.Copy(io.Discard, att.Reader); err != nil {
		return fmt.Errorf("traffic exec drain: %w", err)
	}
	inspectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	inspection, err := cli.ContainerExecInspect(inspectCtx, id.ID)
	if err != nil {
		return err
	}
	if inspection.Running {
		return fmt.Errorf("traffic exec is still running")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Preserve the existing count of attempted flows. A tool's nonzero exit
	// (for example a request blocked by policy) is still generated traffic,
	// not a Docker exec failure or evidence of authorization.
	return nil
}
