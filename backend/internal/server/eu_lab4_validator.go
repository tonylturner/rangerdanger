package server

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/url"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

type euLab4Sample struct {
	SnapshotSeq uint64          `json:"snapshot_seq"`
	Value       json.RawMessage `json:"value"`
	Quality     struct {
		IV bool `json:"iv"`
		NT bool `json:"nt"`
	} `json:"quality"`
	StaleReason       *string    `json:"stale_reason"`
	SourceTime        *time.Time `json:"source_time"`
	SourceTimeInvalid *bool      `json:"source_time_invalid"`
	CCReceivedAt      *time.Time `json:"cc_received_at"`
	IngestedAt        time.Time  `json:"ingested_at"`
}

type euLab4History struct {
	Point    string `json:"point"`
	Segments []struct {
		CacheEpoch string         `json:"cache_epoch"`
		Samples    []euLab4Sample `json:"samples"`
	} `json:"segments"`
	Gaps []struct {
		From time.Time `json:"from"`
		To   time.Time `json:"to"`
	} `json:"gaps"`
}

func init() {
	registerScenarioValidator("eu-oltc-plc-historian", scenarioValidator{
		capabilities: []string{labs.CapabilityPolicyContaind, labs.CapabilityProtocolIEC104, labs.CapabilityProcessTruth},
		run: func(ctx context.Context, s *Server, in validatorInput) []ValidationCheck {
			ev := collectEULab3(ctx, in.gen, "oltc-1")
			if ev.err != nil {
				return []ValidationCheck{checkFail("OLTC evidence available", ev.err.Error())}
			}
			historian, ok := in.gen.Manifest.Endpoint(manifest.RoleHistorian, "api")
			if !ok {
				return []ValidationCheck{checkFail("Historian evidence available", "no historian read API")}
			}
			query := url.Values{"point": {"oltc-1-bus-kv"}, "since": {ev.start.Format(time.RFC3339Nano)}, "until": {ev.end.Format(time.RFC3339Nano)}}
			history, err := fetchJSON[euLab4History](ctx, "historian", historian+"/api/history?"+query.Encode())
			if err != nil {
				return []ValidationCheck{checkFail("Historian evidence available", err.Error())}
			}
			query.Set("point", "oltc-1-auto")
			clockHistory, err := fetchJSON[euLab4History](ctx, "historian", historian+"/api/history?"+query.Encode())
			if err != nil {
				return []ValidationCheck{checkFail("Clock evidence available", err.Error())}
			}
			policyOK := in.activeConfig == "improved" && s.runningPolicyIs(ctx, in.gen, "improved") == nil
			return evaluateEULab4(ev, history, clockHistory, policyOK)
		},
	})
}

func evaluateEULab4(ev euLab3Evidence, history, clockHistory *euLab4History, policyOK bool) []ValidationCheck {
	if ev.err != nil {
		return []ValidationCheck{checkFail("OLTC evidence available", ev.err.Error())}
	}
	return []ValidationCheck{
		euLab3Check("PLC request completed one adjacent step", euLab4Motion(ev, false),
			"accepted PLC-source Modbus request followed by its local motion completion within 3 seconds"),
		euLab3Check("Upper limit reached by the PLC", euLab4Motion(ev, true),
			"PLC-source request completed at +8; the 1.10 p.u. case is not claimed to reach -8"),
		euLab3Check("IEC 104 manual and auto handover", euLab4Handover(ev),
			"ordered manual then auto single commands, each with Modbus mode audit and matching 1303 feedback in its own attempt"),
		euLab3Check("Vendor tap operation and later source deny", policyOK && ev.vendorPair("oltc-1", "10.40.40.23", "manual_step"),
			"accepted vendor Modbus tap write then same-source TCP/502 deny with improved running; denied payload is not observable"),
		euLab3Check("Historian preserves outage uncertainty", euLab4Outage(ev, history),
			"CC link-down/stale/recovery-GI interval contains stale nonzero historian samples or a feed gap"),
		euLab3Check("Station time shifted and restored", euLab4Clock(ev, clockHistory),
			"fresh IEC 104 source versus receipt timestamps show >60 s positive offset, then <5 s offset on a later received observation"),
	}
}

func euLab4Motion(ev euLab3Evidence, upper bool) bool {
	audit := ev.audits["oltc-1"]
	if audit == nil {
		return false
	}
	for _, request := range audit.Entries {
		peer, _, _ := net.SplitHostPort(request.Context.Peer)
		if request.Operation != "plc_request" || request.Result != "accepted" ||
			request.Context.Protocol != "modbus" || peer != "10.30.30.30" ||
			!euLab3Within(request.At, ev.start, ev.end) ||
			!request.After.Moving || !request.Before.Auto ||
			absEULab4(request.After.TargetTap-request.Before.Tap) != 1 {
			continue
		}
		for _, done := range audit.Entries {
			if done.Operation == "motion_complete" && done.Result == "accepted" && done.Context.Protocol == "local" &&
				done.Seq > request.Seq && done.After.Revision > request.After.Revision &&
				done.At.After(request.At) && !done.At.After(request.At.Add(3*time.Second)) &&
				!done.At.After(ev.end) && done.Before.Moving && !done.After.Moving &&
				done.Before.Tap == request.Before.Tap && done.After.Tap == request.After.TargetTap &&
				(!upper || done.After.Tap == 8) {
				return true
			}
		}
	}
	return false
}

func absEULab4(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func euLab4Handover(ev euLab3Evidence) bool {
	audit := ev.audits["oltc-1"]
	if audit == nil {
		return false
	}
	var manualEnd time.Time
	for _, operation := range []string{"manual", "auto"} {
		found := false
		for _, cmd := range ev.commands() {
			if cmd.Point != "oltc-1-mode" || cmd.Operation != operation || cmd.Result != "completed" ||
				!euLab3Within(cmd.ConAt, cmd.ExecutedAt, ev.end) ||
				!euLab3Within(cmd.TermAt, cmd.ConAt, ev.end) || cmd.Feedback == nil ||
				cmd.Feedback.IOA != 1303 || !euLab3Within(cmd.Feedback.At, cmd.ConAt, cmd.TermAt) ||
				(operation == "auto" && !cmd.SelectedAt.After(manualEnd)) {
				continue
			}
			var value bool
			if json.Unmarshal(cmd.Feedback.Value, &value) != nil || value != (operation == "auto") {
				continue
			}
			for _, entry := range audit.Entries {
				peer, _, _ := net.SplitHostPort(entry.Context.Peer)
				if entry.Operation == "mode" && entry.Result == "accepted" && entry.Context.Protocol == "modbus" &&
					peer == "10.30.30.20" &&
					euLab3Within(entry.At, cmd.ExecutedAt, cmd.ConAt) &&
					entry.Before.Auto != entry.After.Auto && entry.After.Auto == value {
					found = true
					if operation == "manual" {
						manualEnd = cmd.TermAt
					}
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func euLab4Outage(ev euLab3Evidence, history *euLab4History) bool {
	if history == nil || history.Point != "oltc-1-bus-kv" || ev.journal == nil {
		return false
	}
	// Match link identity, stale point and later GI, not just a disconnected
	// final status. The voltage remains an observation, never field truth.
	for _, down := range ev.journal.Entries {
		if down.Kind != "link_down" || !euLab3Within(down.At, ev.start, ev.end) {
			continue
		}
		var downDetail struct {
			ConnectionSeq *uint64 `json:"connection_seq"`
		}
		if json.Unmarshal(down.Detail, &downDetail) != nil || downDetail.ConnectionSeq == nil {
			continue
		}
		var staleAt, upAt time.Time
		var upSeq uint64
		for _, entry := range ev.journal.Entries {
			if entry.Seq <= down.Seq || !euLab3Within(entry.At, down.At, ev.end) {
				continue
			}
			var detail struct {
				ConnectionSeq *uint64 `json:"connection_seq"`
				Point         string  `json:"point"`
			}
			if json.Unmarshal(entry.Detail, &detail) != nil {
				continue
			}
			switch entry.Kind {
			case "point_stale":
				if detail.Point == "oltc-1-bus-kv" {
					staleAt = entry.At
				}
			case "link_up":
				if detail.ConnectionSeq != nil && *detail.ConnectionSeq > *downDetail.ConnectionSeq {
					upAt, upSeq = entry.At, *detail.ConnectionSeq
				}
			case "gi_complete":
				if staleAt.IsZero() || upAt.IsZero() || !upAt.After(staleAt) ||
					detail.ConnectionSeq == nil || *detail.ConnectionSeq != upSeq {
					continue
				}
				retained := false
				for _, segment := range history.Segments {
					if segment.CacheEpoch != ev.epoch {
						continue
					}
					for _, sample := range segment.Samples {
						var kv float64
						if !euLab3Within(sample.IngestedAt, staleAt, upAt) || !sample.IngestedAt.Before(upAt) {
							continue
						}
						// This exercise starts with a live bus. Every retained
						// outage sample must preserve uncertainty and its
						// nonzero value, not just one convenient sample.
						if !sample.Quality.IV || !sample.Quality.NT || sample.StaleReason == nil ||
							*sample.StaleReason != "link_lost" || string(sample.Value) == "null" ||
							json.Unmarshal(sample.Value, &kv) != nil || kv <= 0 {
							return false
						}
						retained = true
					}
				}
				if retained {
					return true
				}
				for _, gap := range history.Gaps {
					if euLab3Within(gap.From, down.At, entry.At) && euLab3Within(gap.To, gap.From, entry.At) &&
						gap.To.Sub(gap.From) > 3*time.Second {
						return true
					}
				}
			}
		}
	}
	return false
}

func euLab4Clock(ev euLab3Evidence, history *euLab4History) bool {
	if history == nil || history.Point != "oltc-1-auto" {
		return false
	}
	for _, segment := range history.Segments {
		if segment.CacheEpoch != ev.epoch {
			continue
		}
		var shifted *euLab4Sample
		for i := range segment.Samples {
			sample := &segment.Samples[i]
			if sample.Quality.IV || sample.Quality.NT || sample.StaleReason != nil ||
				sample.SourceTime == nil || sample.CCReceivedAt == nil ||
				sample.SourceTimeInvalid == nil || *sample.SourceTimeInvalid ||
				!euLab3Within(sample.IngestedAt, ev.start, ev.end) ||
				!euLab3Within(*sample.CCReceivedAt, ev.start, sample.IngestedAt) ||
				sample.IngestedAt.Sub(*sample.CCReceivedAt) > 3*time.Second {
				continue
			}
			offset := sample.SourceTime.Sub(*sample.CCReceivedAt).Seconds()
			if offset > 60 {
				shifted = sample
				continue
			}
			if shifted != nil && sample.SnapshotSeq > shifted.SnapshotSeq &&
				sample.CCReceivedAt.After(*shifted.CCReceivedAt) && math.Abs(offset) < 5 {
				return true
			}
		}
	}
	return false
}
