package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

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

// Field audit peers identify the RTU, not its IEC 104 master. Only the RTU
// log carries that original peer. Read the complete retained stream, including
// its listening marker; absence from a truncated stream is not proof.
func (s *Server) euRTULog(ctx context.Context, gen *lifecycle.Generation) (string, error) {
	if s.orchestrator == nil || s.orchestrator.DockerClient() == nil {
		return "", fmt.Errorf("RTU log requires Docker read access")
	}
	rtu, err := nodeService(gen, "rtu-1")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stream, err := s.orchestrator.DockerClient().ContainerLogs(ctx, rtu.Container, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", err
	}
	defer stream.Close()
	stop := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stop()
	const limit = 8 << 20
	raw, err := io.ReadAll(io.LimitReader(stream, limit+1))
	if err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if len(raw) > limit {
		return "", fmt.Errorf("retained RTU log exceeds 8 MiB evidence limit")
	}
	var out bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &out, bytes.NewReader(raw)); err != nil {
		return "", err
	}
	return out.String(), nil
}

func euNoForeignBreaker(ev euWorkshopEvidence) ValidationCheck {
	const name = "No foreign breaker operation since hardening (L4 only)"
	fail := func(reason string) ValidationCheck { return checkFail(name, reason) }
	if ev.logErr != nil || ev.auditErr != nil || ev.identityErr != nil || ev.ccIP == "" || ev.rtuIP == "" || !insideEvidenceWindow(ev.hardeningSince, ev.start, ev.policyAt) || !insideEvidenceWindow(ev.operationEnd, ev.policyAt, ev.now) {
		return fail(fmt.Sprintf("missing identity, policy boundary, RTU log or breaker audit: %v; %v; %v", ev.identityErr, ev.logErr, ev.auditErr))
	}
	if err := euRTUPeers(ev.rtuLog, ev.ccIP, ev.hardeningSince, ev.operationEnd); err != nil {
		return fail(err.Error())
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
	return checkPass(name, fmt.Sprintf("complete retained RTU stream contains no accepted non-CC breaker execute since hardening through %s; continuous current-generation breaker audit contains no accepted foreign Modbus open/close through that cutoff. No DPI or denied-command decoding certified", ev.operationEnd.Format(time.RFC3339Nano)))
}

func euRTUPeers(log, ccIP string, start, end time.Time) error {
	listening := false
	pendingForeign := ""
	for _, line := range strings.Split(log, "\n") {
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
		if fields[1] != "rtu" || at.After(end) {
			continue
		}
		if fields[2] == "listening" {
			if at.After(start) {
				return fmt.Errorf("RTU restarted after hardening")
			}
			listening, pendingForeign = true, ""
		}
		inWindow := insideEvidenceWindow(at, start, end)
		if inWindow && fields[2] == "stopping" {
			return fmt.Errorf("RTU stopped after hardening")
		}
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
	if !listening {
		return fmt.Errorf("RTU listening marker not retained before hardening; cannot prove absence")
	}
	if pendingForeign != "" {
		return fmt.Errorf("foreign breaker execute straddles the hardening window without a terminal outcome")
	}
	return nil
}
