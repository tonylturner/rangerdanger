package server

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// Collect only the positive evidence the new 2.2 validator consumes. The old
// wrapper's clock/isolation/logged-deny collection belongs to its old exercise.
func (s *Server) collectEUWorkshopConduits(ctx context.Context, gen *lifecycle.Generation) conduitEvidence {
	ev := conduitEvidence{rangeStart: gen.StartedAt, readStart: time.Now().UTC(), stationPoints: gen.Package.StationPoints}
	for _, read := range []struct{ source, dest, network, path, name string }{
		{"kali-1", "historian-1", "ops_dmz_net", "/api/points", "Enterprise historian read"},
		{"eng-ws-1", "plc-1", "station_net", "/login", "Named engineering PLC maintenance GET"},
	} {
		_, network, _, err := euSource(gen, read.source)
		if err != nil {
			ev.reads = append(ev.reads, checkFail(read.name, err.Error()))
			continue
		}
		ev.reads = append(ev.reads, s.conduitRead(ctx, gen, read.source, network, read.dest, read.network, read.path, read.name))
	}
	ev.readEnd = time.Now().UTC()
	ev.plcIP, ev.identityErr = nodeIP(gen, "plc-1", "station_net")
	if endpoint, ok := nodeSimURL(gen, "oltc-1"); ok {
		ev.audit, ev.auditErr = fetchJSON[conduitAudit](ctx, simService, endpoint+"/api/audit?after=0")
		ev.device, ev.deviceErr = fetchJSON[deviceTruth](ctx, simService, endpoint+"/api/state")
	} else {
		ev.auditErr = fmt.Errorf("no OLTC sim endpoint")
		ev.deviceErr = ev.auditErr
	}
	if endpoint, ok := gen.Manifest.Endpoint(manifest.RoleHistorian, "api"); ok {
		until := time.Now().UTC()
		since := until.Add(-conduitAttemptLimit)
		if since.Before(gen.StartedAt) {
			since = gen.StartedAt
		}
		query := url.Values{"point": {"oltc-1-tap"}, "since": {since.Format(time.RFC3339Nano)}, "until": {until.Format(time.RFC3339Nano)}}
		ev.history, ev.historyErr = fetchJSON[conduitHistory](ctx, "historian", endpoint+"/api/history?"+query.Encode())
	} else {
		ev.historyErr = fmt.Errorf("no historian API endpoint")
	}
	// Read telemetry last so probes and source-bound GETs cannot age it.
	ev.state, ev.journal, ev.stateErr = euCC(ctx, gen)
	ev.journalErr = ev.stateErr
	ev.collectedAt = time.Now().UTC()
	return ev
}
