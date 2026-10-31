package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

type euWorkshopBreakerEntry = euAuditEntry[euWorkshopDevice, euWorkshopDevice, peerAuditContext]
type euWorkshopBreakerAudit struct {
	euAudit[euWorkshopBreakerEntry]
	OldestSeq *uint64 `json:"oldest_seq"`
	LatestSeq *uint64 `json:"latest_seq"`
}

type euRTUContainerIdentity struct {
	id           string
	startedAt    time.Time
	restartCount int
}

func euRTUIdentityFromInspect(info dockertypes.ContainerJSON, hardeningSince time.Time) (euRTUContainerIdentity, error) {
	if info.ContainerJSONBase == nil || info.ID == "" || info.State == nil || !info.State.Running || info.State.Restarting {
		return euRTUContainerIdentity{}, fmt.Errorf("RTU container is not running with a stable identity")
	}
	startedAt, err := time.Parse(time.RFC3339Nano, info.State.StartedAt)
	if err != nil || startedAt.IsZero() || startedAt.After(hardeningSince) {
		return euRTUContainerIdentity{}, fmt.Errorf("RTU container start does not precede hardening")
	}
	if info.RestartCount != 0 {
		return euRTUContainerIdentity{}, fmt.Errorf("RTU container restarted; log incarnation is ambiguous")
	}
	return euRTUContainerIdentity{id: info.ID, startedAt: startedAt, restartCount: info.RestartCount}, nil
}

// euRTULog streams one container incarnation from its start, retaining only
// parser state. The range's hardening boundary determines which operations
// count; older lines are needed only to establish the listener and any
// command that straddles that boundary.
func (s *Server) euRTULog(ctx context.Context, gen *lifecycle.Generation, hardeningSince, operationEnd time.Time, ccIP string) error {
	if s.orchestrator == nil || s.orchestrator.DockerClient() == nil {
		return fmt.Errorf("RTU log requires Docker read access")
	}
	rtu, err := nodeService(gen, "rtu-1")
	if err != nil {
		return err
	}
	if hardeningSince.IsZero() || operationEnd.IsZero() || operationEnd.Before(hardeningSince) || ccIP == "" {
		return fmt.Errorf("invalid RTU evidence window or control-centre identity")
	}
	cli := s.orchestrator.DockerClient()
	before, err := cli.ContainerInspect(ctx, rtu.Container)
	if err != nil {
		return err
	}
	identity, err := euRTUIdentityFromInspect(before, hardeningSince)
	if err != nil {
		return err
	}
	stream, err := cli.ContainerLogs(ctx, identity.id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Since:      identity.startedAt.UTC().Format(time.RFC3339Nano),
		Until:      operationEnd.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	defer stream.Close()
	stop := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stop()

	reader, writer := io.Pipe()
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(writer, writer, stream)
		_ = writer.CloseWithError(copyErr)
		copyDone <- copyErr
	}()
	parseErr := euRTUPeersReader(reader, ccIP, identity.startedAt, gen.StartedAt, hardeningSince, operationEnd)
	_ = reader.Close()
	copyErr := <-copyDone
	if parseErr != nil {
		return parseErr
	}
	if copyErr != nil {
		return copyErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	after, err := cli.ContainerInspect(ctx, rtu.Container)
	if err != nil {
		return err
	}
	afterIdentity, err := euRTUIdentityFromInspect(after, hardeningSince)
	if err != nil {
		return err
	}
	if afterIdentity != identity {
		return fmt.Errorf("RTU container incarnation changed during log collection")
	}
	return nil
}

func euNoForeignBreaker(ev euWorkshopEvidence) ValidationCheck {
	if ev.isPlan && ev.foreignBreakerAllowed {
		return checkPass("Foreign breaker command exposure", "the applied plan permits Kali → RTU TCP/2404; a foreign breaker command can reach the RTU as the visible consequence. Hardened would deny this connection; restoration, GI and supply checks still apply")
	}
	const name = "No foreign breaker operation since hardening (L4 only)"
	fail := func(reason string) ValidationCheck { return checkFail(name, reason) }
	if ev.logErr != nil || ev.auditErr != nil || ev.identityErr != nil || ev.ccIP == "" || ev.rtuIP == "" || !insideEvidenceWindow(ev.hardeningSince, ev.start, ev.policyAt) || !insideEvidenceWindow(ev.operationEnd, ev.policyAt, ev.now) {
		return fail(fmt.Sprintf("missing identity, policy boundary, RTU log or breaker audit: %v; %v; %v", ev.identityErr, ev.logErr, ev.auditErr))
	}
	audit, device := ev.breakerAudit, ev.devices["breaker-1"]
	if audit == nil || device == nil || device.EventSeq == 0 || audit.Generation == 0 || audit.Generation != device.Generation || audit.OldestSeq == nil || audit.LatestSeq == nil || len(audit.Entries) == 0 {
		return fail("breaker audit and truth must share a retained device generation")
	}
	first := audit.Entries[0]
	if first.Seq != *audit.OldestSeq || first.At.After(ev.hardeningSince) || audit.Entries[len(audit.Entries)-1].Seq != *audit.LatestSeq || *audit.LatestSeq < device.EventSeq {
		return fail("breaker audit does not cover the hardening boundary and current truth")
	}
	for i, entry := range audit.Entries {
		if i > 0 && (entry.Seq != audit.Entries[i-1].Seq+1 || entry.At.Before(audit.Entries[i-1].At)) {
			return fail("breaker audit is discontinuous")
		}
		if !insideEvidenceWindow(entry.At, ev.hardeningSince, ev.operationEnd) {
			continue
		}
		if entry.Operation == "reset" && entry.Result == "accepted" {
			return fail("breaker reset after hardening; repeat the retest with a new policy boundary")
		}
		if entry.Result != "accepted" || entry.Operation != "open" && entry.Operation != "close" {
			continue
		}
		if entry.Context.Protocol != "modbus" {
			continue
		}
		peer, _, err := net.SplitHostPort(entry.Context.Peer)
		if err != nil || peer != ev.rtuIP {
			return fail("accepted breaker Modbus operation from a non-RTU peer after hardening")
		}
	}
	return checkPass(name, fmt.Sprintf("continuous current-container RTU log evidence contains no accepted non-CC breaker execute since hardening through %s; continuous current-generation breaker audit contains no accepted foreign Modbus open/close through that cutoff. No DPI or denied-command decoding certified", ev.operationEnd.Format(time.RFC3339Nano)))
}

func euRTUPeers(log, ccIP string, start, end time.Time) error {
	return euRTUPeersReader(strings.NewReader(log), ccIP, time.Time{}, time.Time{}, start, end)
}

func euRTUPeersReader(reader io.Reader, ccIP string, incarnationStart, rangeStart, start, end time.Time) error {
	if start.IsZero() || end.IsZero() || end.Before(start) || ccIP == "" {
		return fmt.Errorf("invalid RTU evidence window or control-centre identity")
	}
	listening := false
	pendingForeign := ""
	var previousAt time.Time
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "set-gateway:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return fmt.Errorf("malformed RTU log record")
		}
		at, err := time.Parse(time.RFC3339Nano, fields[0])
		if err != nil {
			return fmt.Errorf("RTU log record has no timestamp")
		}
		if !previousAt.IsZero() && at.Before(previousAt) {
			return fmt.Errorf("RTU log timestamps are discontinuous")
		}
		previousAt = at
		if !incarnationStart.IsZero() && at.Before(incarnationStart) {
			continue
		}
		if fields[1] != "rtu" || at.After(end) {
			continue
		}
		if fields[2] == "listening" {
			if at.After(start) {
				return fmt.Errorf("RTU restarted after hardening")
			}
			listening, pendingForeign = true, ""
			continue
		}
		if fields[2] == "stopping" {
			if insideEvidenceWindow(at, start, end) {
				return fmt.Errorf("RTU stopped after hardening")
			}
			if at.Before(start) {
				listening, pendingForeign = false, ""
			}
			continue
		}
		if !rangeStart.IsZero() && at.Before(rangeStart) {
			continue
		}
		inWindow := insideEvidenceWindow(at, start, end)
		if fields[2] != "command" {
			continue
		}
		values := map[string]string{}
		for _, field := range fields[3:] {
			k, v, ok := strings.Cut(field, "=")
			if ok {
				values[k] = v
			}
		}
		for _, key := range []string{"ioa", "peer", "phase", "result"} {
			if values[key] == "" {
				return fmt.Errorf("incomplete RTU command metadata")
			}
		}
		if values["ioa"] != "3001" || values["phase"] != "execute" {
			continue
		}
		peer, _, peerErr := net.SplitHostPort(values["peer"])
		foreign := peerErr != nil || peer != ccIP
		result := values["result"]
		// An accepted command can straddle the policy change. Unknown is
		// ambiguous actuation, not proof that no operation happened.
		if inWindow && foreign && (result == "accepted" || result == "completed" || result == "unknown" || result == "failed") {
			return fmt.Errorf("non-CC IEC 104 breaker execute accepted, completed, failed feedback or ambiguous after hardening")
		}
		switch result {
		case "accepted":
			if foreign {
				pendingForeign = values["peer"]
			}
		case "completed", "unknown", "rejected", "failed":
			// A different master's rejected attempt or terminal record
			// cannot close an outstanding foreign command's evidence gap.
			if values["peer"] == pendingForeign {
				pendingForeign = ""
			}
		default:
			return fmt.Errorf("unknown RTU breaker execute result %q", result)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read RTU log window: %w", err)
	}
	if !listening {
		return fmt.Errorf("RTU listening marker not retained before hardening; cannot prove absence")
	}
	if pendingForeign != "" {
		return fmt.Errorf("foreign breaker execute straddles the hardening window without a terminal outcome")
	}
	return nil
}
