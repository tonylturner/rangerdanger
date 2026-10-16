package server

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const (
	euProbeConcurrency = 4
	euProbeMatrixLimit = 30 * time.Second
	euProbeExecLimit   = 5 * time.Second
)

// Bash provides a connect-only primitive on the workstations and PLC. The
// Alpine RTU/CC images instead ship BusyBox nc: -e true exits immediately
// AFTER connect, rather than waiting on an established protocol session and
// mistaking that idle session for a connect timeout. Neither sends a payload.
const euTCPCommand = `export LC_ALL=C
if command -v bash >/dev/null 2>&1; then
  exec timeout 3 bash -c 'exec 3<>/dev/tcp/$1/$2' probe "$1" "$2"
else
  exec timeout 4 busybox nc -w 3 "$1" "$2" -e /bin/true
fi`

// strictTCPProbe is shared by the EU report and validators. Only a successful
// connect, explicit refusal, or the probe's own connect timeout is measurable.
// Exec errors, signals, bad arguments and missing tools remain untested.
func (s *Server) strictTCPProbe(ctx context.Context, cli *client.Client, p validationProbe) validationRow {
	start := time.Now()
	row := validationRow{validationProbe: p, Actual: "skipped"}
	port, err := strconv.Atoi(p.Port)
	if net.ParseIP(p.Dst) == nil || err != nil || port < 1 || port > 65535 || p.Src == "" {
		row.ProbeError = "invalid TCP probe identity or endpoint"
		return row
	}
	probeCtx, cancel := context.WithTimeout(ctx, euProbeExecLimit)
	defer cancel()
	stdout, stderr, rc, execErr := strictProbeExec(probeCtx, cli, p.Src, []string{"sh", "-c", euTCPCommand, "probe", p.Dst, p.Port})
	row.Actual, row.ProbeError = classifyStrictTCP(stdout, stderr, rc, execErr)
	if probeCtx.Err() != nil {
		row.Actual, row.ProbeError = "skipped", probeCtx.Err().Error()
	}
	row.DurationMs = int(time.Since(start).Milliseconds())
	row.Pass = row.Actual == p.Expected
	return row
}

func classifyStrictTCP(stdout, stderr string, rc int, err error) (string, string) {
	if err != nil {
		return "skipped", fmt.Sprintf("container exec: %v", err)
	}
	if rc == 0 {
		return "allow", ""
	}
	// Containd drops rather than rejects: refusal proves the destination was
	// reached, even without a listener. Only a connect timeout counts as deny.
	if rc == 1 && strings.Contains(stderr, "Connection refused") {
		return "allow", ""
	}
	// 124 is timeout(1)'s own budget, not a Docker/context timeout.
	if rc == 124 || rc == 1 && (strings.Contains(stderr, "Connection timed out") || strings.Contains(stderr, "timed out")) {
		return "deny", ""
	}
	return "skipped", fmt.Sprintf("probe tool exit %d: %s", rc, strings.TrimSpace(stderr+"\n"+stdout))
}

// Cancellation closes the hijacked stream: a wedged container cannot hold a
// worker indefinitely in StdCopy. The API calls also share the same deadline.
// Keep this strict executor separate from the legacy US report executor.
func strictProbeExec(ctx context.Context, cli *client.Client, source string, cmd []string) (string, string, int, error) {
	if cli == nil {
		return "", "", -1, fmt.Errorf("docker client not available")
	}
	id, err := cli.ContainerExecCreate(ctx, source, container.ExecOptions{Cmd: cmd, AttachStdout: true, AttachStderr: true})
	if err != nil {
		return "", "", -1, err
	}
	att, err := cli.ContainerExecAttach(ctx, id.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", "", -1, err
	}
	defer att.Close()
	stop := context.AfterFunc(ctx, att.Close)
	defer stop()
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, att.Reader); err != nil {
		return "", "", -1, err
	}
	if ctx.Err() != nil {
		return "", "", -1, ctx.Err()
	}
	inspection, err := cli.ContainerExecInspect(ctx, id.ID)
	if err != nil {
		return "", "", -1, err
	}
	if inspection.Running {
		return "", "", -1, fmt.Errorf("probe exec is still running")
	}
	return stdout.String(), stderr.String(), inspection.ExitCode, nil
}

type euProbeFunc func(context.Context, validationProbe) validationRow

// Fixed workers bound both active execs and goroutines. A deadline leaves
// unstarted rows explicitly untested, never silently dropped from the matrix.
func runEUProbeMatrix(ctx context.Context, matrix []validationProbe, probe euProbeFunc) []validationRow {
	ctx, cancel := context.WithTimeout(ctx, euProbeMatrixLimit)
	defer cancel()
	rows := make([]validationRow, len(matrix))
	var wg sync.WaitGroup
	for worker := 0; worker < min(euProbeConcurrency, len(matrix)); worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := worker; i < len(matrix); i += euProbeConcurrency {
				p := matrix[i]
				if ctx.Err() != nil {
					rows[i] = validationRow{validationProbe: p, Actual: "skipped", ProbeError: ctx.Err().Error()}
					continue
				}
				row := probe(ctx, p)
				if p.Category == "authorized" && row.Actual == "deny" && ctx.Err() == nil {
					row = probe(ctx, p)
				}
				if ctx.Err() != nil {
					row.Actual, row.Pass, row.ProbeError = "skipped", false, ctx.Err().Error()
				}
				rows[i] = row
			}
		}(worker)
	}
	wg.Wait()
	return rows
}
