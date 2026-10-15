package server

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// The latest CC link_up is the attempt boundary, not a policy name or range
// boot. Periodic GIs do not restart it. A restart/reconnect requires new proof.
const conduitAttemptLimit = 10 * time.Minute

type conduitPoint struct {
	ccPoint
	IOA int `json:"ioa"`
}

type conduitState struct {
	Schema     int            `json:"schema"`
	CacheEpoch string         `json:"cache_epoch"`
	Points     []conduitPoint `json:"points"`
	Link       struct {
		State         string `json:"state"`
		ConnectionSeq uint64 `json:"connection_seq"`
	} `json:"link"`
}

type conduitAudit struct {
	Generation uint32              `json:"generation"`
	Entries    []conduitAuditEntry `json:"entries"`
}

type conduitAuditEntry struct {
	auditEntry
	Before struct {
		Tap int `json:"tap"`
	} `json:"before"`
	After struct {
		Revision  uint64 `json:"revision"`
		Tap       int    `json:"tap"`
		TargetTap int    `json:"target_tap"`
		Moving    bool   `json:"moving"`
	} `json:"after"`
	Context struct {
		Protocol string `json:"protocol"`
		Peer     string `json:"peer"`
	} `json:"context"`
}

type conduitHistory struct {
	Point    string `json:"point"`
	Segments []struct {
		CacheEpoch string `json:"cache_epoch"`
		Samples    []struct {
			SnapshotSeq  uint64          `json:"snapshot_seq"`
			Value        json.RawMessage `json:"value"`
			CCReceivedAt *time.Time      `json:"cc_received_at"`
			IngestedAt   time.Time       `json:"ingested_at"`
		} `json:"samples"`
	} `json:"segments"`
}

type conduitDeny struct {
	source, dest string
	port         int
}

type conduitEvidence struct {
	rangeStart, collectedAt time.Time
	state                   *conduitState
	stateErr                error
	journal                 *ccJournal
	journalErr              error
	audit                   *conduitAudit
	auditErr                error
	device                  *deviceTruth
	deviceErr               error
	history                 *conduitHistory
	historyErr              error
	events                  []containd.Event
	eventsErr               error
	plcIP                   string
	denies                  []conduitDeny
	identityErr             error
	clock                   ValidationCheck
	reads                   []ValidationCheck
	readStart, readEnd      time.Time
}

func conduitWindow(ev conduitEvidence) (time.Time, ValidationCheck) {
	name := "Completed GI in this connection attempt"
	fail := func(detail string) (time.Time, ValidationCheck) {
		return time.Time{}, checkFail(name, "missing evidence: "+detail)
	}
	if ev.stateErr != nil || ev.journalErr != nil || ev.state == nil || ev.journal == nil {
		return fail(fmt.Sprintf("CC state/journal unavailable: %v; %v", ev.stateErr, ev.journalErr))
	}
	if ev.state.Schema != 2 || ev.state.CacheEpoch == "" || ev.state.CacheEpoch != ev.journal.CacheEpoch || ev.state.Link.State != "started" {
		return fail("schema-2 state and journal must share a live CC cache epoch")
	}
	var up journalEntry
	for _, entry := range ev.journal.Entries {
		if entry.Kind == "link_up" && entry.Detail.ConnectionSeq != nil && *entry.Detail.ConnectionSeq == ev.state.Link.ConnectionSeq && entry.Seq > up.Seq {
			up = entry
		}
	}
	if up.Seq == 0 || up.Seq < ev.journal.OldestSeq || up.At.Before(ev.rangeStart) || !insideEvidenceWindow(up.At, ev.collectedAt.Add(-conduitAttemptLimit), ev.collectedAt) {
		return fail("no retained link_up for the current connection within ten minutes; run the proof-window session cut and repeat operations")
	}
	for _, entry := range ev.journal.Entries {
		if entry.Kind == "gi_complete" && entry.Seq > up.Seq && entry.Detail.ConnectionSeq != nil && *entry.Detail.ConnectionSeq == *up.Detail.ConnectionSeq && insideEvidenceWindow(entry.At, up.At, ev.collectedAt) {
			return up.At, checkPass(name, fmt.Sprintf("CC epoch %s connection %d: link_up %s then GI seq %d; attempt ends %s", ev.state.CacheEpoch, *up.Detail.ConnectionSeq, up.At.Format(time.RFC3339Nano), entry.Seq, ev.collectedAt.Format(time.RFC3339Nano)))
		}
	}
	return fail("no completed GI after the current link_up")
}

// Each frozen IOA must be represented exactly once. This is independent of
// lab 1's deliberately smaller breaker-only set.
var conduitIOAs = []int{1001, 1002, 2001, 2002, 1101, 1102, 1103, 1104, 2101, 2102, 2103, 1201, 1202, 1203, 1204, 2201, 2202, 2203, 1301, 1302, 1303, 1304, 1305, 1306, 2301, 2302, 1401, 1402, 2401, 2402, 2403}

func conduitPolls(ev conduitEvidence, start time.Time) ValidationCheck {
	name := "Fresh full-station protocol observations"
	if start.IsZero() || ev.state == nil {
		return checkFail(name, "missing evidence: no current CC attempt")
	}
	seen := map[int]int{}
	for _, point := range ev.state.Points {
		if badQuality(point.ccPoint) == "" && len(point.Value) > 0 && string(point.Value) != "null" && insideEvidenceWindow(point.ReceivedAt, start, ev.collectedAt) && ev.collectedAt.Sub(point.ReceivedAt) <= 3*time.Second {
			seen[point.IOA]++
		}
	}
	var missing []int
	for _, ioa := range conduitIOAs {
		if seen[ioa] != 1 {
			missing = append(missing, ioa)
		}
	}
	if len(missing) != 0 {
		return checkFail(name, fmt.Sprintf("missing evidence: IOAs %v need unique, non-null, GOOD observations received in this attempt and no older than 3 s", missing))
	}
	return checkPass(name, "all 31 breaker/RMU/OLTC/PLC IOAs freshly received over IEC 104; no field/process truth substituted")
}

func conduitPLC(ev conduitEvidence, start time.Time) ValidationCheck {
	name := "PLC Modbus request completed and solved"
	if start.IsZero() || ev.auditErr != nil || ev.deviceErr != nil || ev.identityErr != nil || ev.audit == nil || ev.device == nil || ev.state == nil {
		return checkFail(name, fmt.Sprintf("missing evidence: attempt, OLTC audit/state or PLC identity unavailable: %v; %v; %v", ev.auditErr, ev.deviceErr, ev.identityErr))
	}
	if ev.audit.Generation != ev.device.Generation {
		return checkFail(name, "missing evidence: OLTC generation changed; repeat the attempt")
	}
	if ev.device.Schema != 2 {
		return checkFail(name, "missing evidence: OLTC state must use schema 2")
	}
	for _, ioa := range []int{1302, 1303, 1401, 1402} {
		enabled := false
		for _, point := range ev.state.Points {
			if point.IOA == ioa && string(point.Value) == "true" && badQuality(point.ccPoint) == "" &&
				insideEvidenceWindow(point.ReceivedAt, start, ev.collectedAt) && ev.collectedAt.Sub(point.ReceivedAt) <= 3*time.Second {
				enabled = true
			}
		}
		if !enabled {
			return checkFail(name, fmt.Sprintf("missing evidence: fresh remote/auto/running/input-valid indication %d must be true", ioa))
		}
	}
	for _, request := range ev.audit.Entries {
		peer, _, err := net.SplitHostPort(request.Context.Peer)
		if err != nil || peer != ev.plcIP || request.Operation != "plc_request" || request.Result != "accepted" || request.Context.Protocol != "modbus" || !request.After.Moving || absConduit(request.After.TargetTap-request.Before.Tap) != 1 || !insideEvidenceWindow(request.At, start, ev.collectedAt) {
			continue
		}
		for _, complete := range ev.audit.Entries {
			if complete.Seq <= request.Seq || complete.Operation != "motion_complete" || complete.Result != "accepted" || complete.After.Moving || complete.After.Tap != request.After.TargetTap || !complete.At.After(request.At) || !insideEvidenceWindow(complete.At, start, ev.collectedAt) {
				continue
			}
			for _, solve := range ev.audit.Entries {
				var detail struct {
					Revision *uint64 `json:"revision"`
				}
				if solve.Operation == "first_solve" && solve.Result == "accepted" && solve.Seq > complete.Seq && json.Unmarshal(solve.Detail, &detail) == nil && detail.Revision != nil && *detail.Revision == complete.After.Revision && solve.At.After(complete.At) && insideEvidenceWindow(solve.At, start, ev.collectedAt) {
					return checkPass(name, fmt.Sprintf("PLC %s Modbus request seq %d → tap %d completion seq %d → solved revision %d, all inside attempt", peer, request.Seq, complete.After.Tap, complete.Seq, complete.After.Revision))
				}
			}
		}
	}
	return checkFail(name, "missing evidence: no in-window PLC-sourced accepted adjacent tap request followed by motion_complete and first_solve of that revision")
}

func absConduit(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func conduitFeed(ev conduitEvidence, start time.Time) ValidationCheck {
	name := "Advancing CC-to-historian ingest"
	if start.IsZero() || ev.historyErr != nil || ev.history == nil || ev.state == nil || ev.history.Point != "oltc-1-tap" {
		return checkFail(name, fmt.Sprintf("missing evidence: current-attempt OLTC history unavailable: %v", ev.historyErr))
	}
	for _, segment := range ev.history.Segments {
		if segment.CacheEpoch != ev.state.CacheEpoch {
			continue
		}
		var seq uint64
		var ingested time.Time
		for _, sample := range segment.Samples {
			if sample.CCReceivedAt == nil || !insideEvidenceWindow(*sample.CCReceivedAt, start, sample.IngestedAt) || !insideEvidenceWindow(sample.IngestedAt, start, ev.collectedAt) || len(sample.Value) == 0 || string(sample.Value) == "null" {
				continue
			}
			if seq > 0 && sample.SnapshotSeq > seq && sample.IngestedAt.After(ingested) && ev.collectedAt.Sub(sample.IngestedAt) <= 3*time.Second {
				return checkPass(name, fmt.Sprintf("CC epoch %s ingest advanced snapshot %d → %d at %s; receipt times belong to this attempt", segment.CacheEpoch, seq, sample.SnapshotSeq, sample.IngestedAt.Format(time.RFC3339Nano)))
			}
			seq, ingested = sample.SnapshotSeq, sample.IngestedAt
		}
	}
	return checkFail(name, "missing evidence: need two advancing in-window snapshots of this CC epoch with recent ingest, preserving CC receipt times")
}

func conduitTime(ev conduitEvidence, start time.Time) ValidationCheck {
	name := "RTU station-clock HTTP operation"
	if start.IsZero() || !insideEvidenceWindow(ev.readStart, start, ev.collectedAt) || !insideEvidenceWindow(ev.readEnd, ev.readStart, ev.collectedAt) || ev.clock.Name == "" {
		return checkFail(name, "missing evidence: no RTU clock read inside the retained connection attempt")
	}
	return ev.clock
}

func conduitDenies(ev conduitEvidence, start time.Time) ValidationCheck {
	name := "Foreign control, field and ingest attempts denied"
	if start.IsZero() || ev.eventsErr != nil || ev.identityErr != nil || len(ev.denies) == 0 {
		return checkFail(name, fmt.Sprintf("missing evidence: attempt/firewall events/source identities unavailable: %v; %v", ev.eventsErr, ev.identityErr))
	}
	var missing []string
	for _, want := range ev.denies {
		found := false
		for _, event := range ev.events {
			action, _ := event.Attributes["action"].(string)
			if event.Kind == "firewall.rule.hit" && action == "DENY" && event.Transport == "tcp" && event.Source == want.source && event.Dest == want.dest && event.DstPort == want.port && insideEvidenceWindow(event.Timestamp, start, ev.collectedAt) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fmt.Sprintf("%s→%s:%d", want.source, want.dest, want.port))
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return checkFail(name, fmt.Sprintf("missing evidence: no in-window logged TCP DENY for %v", missing))
	}
	return checkPass(name, fmt.Sprintf("all %d named foreign-source protocol attempts denied inside the current connection attempt", len(ev.denies)))
}

func evaluateConduits(ev conduitEvidence) []ValidationCheck {
	start, window := conduitWindow(ev)
	checks := []ValidationCheck{window, conduitPolls(ev, start), conduitPLC(ev, start), conduitFeed(ev, start), conduitTime(ev, start), conduitDenies(ev, start)}
	for i, read := range ev.reads {
		if start.IsZero() || !insideEvidenceWindow(ev.readStart, start, ev.collectedAt) || !insideEvidenceWindow(ev.readEnd, ev.readStart, ev.collectedAt) {
			ev.reads[i] = checkFail(read.Name, "missing evidence: teaching-source GET is outside the retained connection attempt")
		}
	}
	return append(checks, ev.reads...)
}
