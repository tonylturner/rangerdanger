package server

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

func euLab4TestHistory(t *testing.T, point string, samples []map[string]any) *euLab4History {
	t.Helper()
	var history euLab4History
	data := euLab3TestJSON(t, map[string]any{"point": point, "segments": []any{map[string]any{"cache_epoch": "cc1", "samples": samples}}, "gaps": []any{}})
	if err := json.Unmarshal(data, &history); err != nil {
		t.Fatal(err)
	}
	return &history
}

func euLab4TestEvidence(t *testing.T) (euLab3Evidence, *euLab4History, *euLab4History) {
	t.Helper()
	ev := euLab3TestEvidence(t)
	ev.journal.Entries = nil
	ev.audits = map[string]*euLab3Audit{"oltc-1": {Generation: 1}}
	request := euLab3TestEntry(10, "plc_request", "accepted", "10.30.30.30:45000")
	request.Before.Auto, request.Before.Tap = true, 7
	request.After.Moving, request.After.TargetTap = true, 8
	request.After.Revision = 5
	done := euLab3TestEntry(12, "motion_complete", "accepted", "actuator")
	done.Context.Protocol = "local"
	done.Before.Tap, done.After.Tap = 7, 8
	done.Before.Moving = true
	done.After.Revision = 6
	ev.audits["oltc-1"].Entries = []euLab3AuditEntry{request, done}
	for _, mode := range []struct {
		second    int
		operation string
		value     bool
	}{{20, "manual", false}, {30, "auto", true}} {
		detail := map[string]any{"point": "oltc-1-mode", "operation": mode.operation, "result": "completed",
			"selected_at": euLab3TestAt(mode.second), "executed_at": euLab3TestAt(mode.second + 1),
			"con_at": euLab3TestAt(mode.second + 3), "term_at": euLab3TestAt(mode.second + 5),
			"feedback": map[string]any{"ioa": 1303, "value": mode.value, "at": euLab3TestAt(mode.second + 4)}}
		ev.journal.Entries = append(ev.journal.Entries, euLab3JournalEntry{Seq: uint64(mode.second), At: euLab3TestAt(mode.second + 6), Kind: "command", Detail: euLab3TestJSON(t, detail)})
		entry := euLab3TestEntry(mode.second+2, "mode", "accepted", "10.30.30.20:43001")
		entry.Before.Auto, entry.After.Auto = !mode.value, mode.value
		ev.audits["oltc-1"].Entries = append(ev.audits["oltc-1"].Entries, entry)
	}
	vendor := euLab3TestEntry(40, "manual_step", "accepted", "10.20.20.10:44000")
	ev.audits["oltc-1"].Entries = append(ev.audits["oltc-1"].Entries, vendor)
	ev.events[0].Dest, ev.events[0].Timestamp = "10.40.40.23", euLab3TestAt(50)
	for _, event := range []struct {
		second int
		kind   string
		detail map[string]any
	}{
		{60, "link_down", map[string]any{"connection_seq": 1}},
		{61, "point_stale", map[string]any{"point": "oltc-1-bus-kv"}},
		{70, "link_up", map[string]any{"connection_seq": 2}},
		{71, "gi_complete", map[string]any{"connection_seq": 2}},
	} {
		ev.journal.Entries = append(ev.journal.Entries, euLab3JournalEntry{Seq: uint64(event.second), At: euLab3TestAt(event.second), Kind: event.kind, Detail: euLab3TestJSON(t, event.detail)})
	}
	history := euLab4TestHistory(t, "oltc-1-bus-kv", []map[string]any{
		{"snapshot_seq": 65, "value": 20.0, "quality": map[string]bool{"iv": true, "nt": true}, "stale_reason": "link_lost", "ingested_at": euLab3TestAt(65)},
	})
	clock := euLab4TestHistory(t, "oltc-1-auto", []map[string]any{
		{"snapshot_seq": 80, "value": true, "quality": map[string]bool{"iv": false, "nt": false}, "ingested_at": euLab3TestAt(80), "cc_received_at": euLab3TestAt(80), "source_time": euLab3TestAt(200), "source_time_invalid": false},
		{"snapshot_seq": 90, "value": true, "quality": map[string]bool{"iv": false, "nt": false}, "ingested_at": euLab3TestAt(90), "cc_received_at": euLab3TestAt(90), "source_time": euLab3TestAt(90), "source_time_invalid": false},
	})
	return ev, history, clock
}

func TestEULab4ValidatorPass(t *testing.T) {
	ev, history, clock := euLab4TestEvidence(t)
	euLab3AssertPass(t, evaluateEULab4(ev, history, clock, true))
}

func TestEULab4EvidenceCollectorReadsManifest(t *testing.T) {
	ev, history, clock := euLab4TestEvidence(t)
	s, gen := euLab3TestRange(t, ev, history, clock)
	collected := collectEULab3(t.Context(), gen, "oltc-1")
	endpoint, ok := gen.Manifest.Endpoint(manifest.RoleHistorian, "api")
	if !ok {
		t.Fatal("no historian")
	}
	query := url.Values{"point": {"oltc-1-bus-kv"}, "since": {collected.start.Format(time.RFC3339Nano)}, "until": {collected.end.Format(time.RFC3339Nano)}}
	gotHistory, err := fetchJSON[euLab4History](t.Context(), "historian", endpoint+"/api/history?"+query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	query.Set("point", "oltc-1-auto")
	gotClock, err := fetchJSON[euLab4History](t.Context(), "historian", endpoint+"/api/history?"+query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	euLab3AssertPass(t, evaluateEULab4(collected, gotHistory, gotClock, s.runningPolicyIs(t.Context(), gen, "improved") == nil))
}

func TestEULab4MotionRequiresActualPLCCompletion(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*euLab3Evidence)
	}{
		{"no PLC request", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries = ev.audits["oltc-1"].Entries[1:] }},
		{"wrong source", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries[0].Context.Peer = "10.20.20.10:44000" }},
		{"nonadjacent", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries[0].Before.Tap = 6 }},
		{"no completion", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries[1].Operation = "measurements_valid" }},
		{"before request", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries[1].At = euLab3TestAt(9) }},
		{"unrelated later completion", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries[1].At = euLab3TestAt(16) }},
		{"old revision", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries[1].After.Revision = 4 }},
		{"still moving", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries[1].After.Moving = true }},
		{"outside attempt", func(ev *euLab3Evidence) { ev.start = euLab3TestAt(11) }},
		{"future completion", func(ev *euLab3Evidence) { ev.end = euLab3TestAt(11) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ev, _, _ := euLab4TestEvidence(t)
			test.change(&ev)
			if euLab4Motion(ev, false) {
				t.Fatal("unproven motion passed")
			}
		})
	}
	t.Run("not at upper", func(t *testing.T) {
		ev, _, _ := euLab4TestEvidence(t)
		ev.audits["oltc-1"].Entries[0].Before.Tap = 6
		ev.audits["oltc-1"].Entries[0].After.TargetTap = 7
		ev.audits["oltc-1"].Entries[1].Before.Tap = 6
		ev.audits["oltc-1"].Entries[1].After.Tap = 7
		if !euLab4Motion(ev, false) || euLab4Motion(ev, true) {
			t.Fatal("upper limit confused with arbitrary adjacent step")
		}
	})
}

func TestEULab4HandoverRequiresOrderedProtocolEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*euLab3Evidence)
	}{
		{"no auto", func(ev *euLab3Evidence) { ev.journal.Entries = ev.journal.Entries[:1] }},
		{"no audit", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries = nil }},
		{"wrong peer", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries[2].Context.Peer = "10.20.20.10:44000" }},
		{"audit outside attempt", func(ev *euLab3Evidence) { ev.audits["oltc-1"].Entries[2].At = euLab3TestAt(28) }},
		{"unknown result", func(ev *euLab3Evidence) {
			var cmd map[string]any
			_ = json.Unmarshal(ev.journal.Entries[0].Detail, &cmd)
			cmd["result"] = "unknown"
			ev.journal.Entries[0].Detail = euLab3TestJSON(t, cmd)
		}},
		{"stale feedback", func(ev *euLab3Evidence) {
			var cmd map[string]any
			_ = json.Unmarshal(ev.journal.Entries[0].Detail, &cmd)
			cmd["feedback"].(map[string]any)["at"] = euLab3TestAt(19)
			ev.journal.Entries[0].Detail = euLab3TestJSON(t, cmd)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ev, _, _ := euLab4TestEvidence(t)
			test.change(&ev)
			if euLab4Handover(ev) {
				t.Fatal("unproven handover passed")
			}
		})
	}
}

func TestEULab4HistoryNeverTreatsMissingAsZero(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*euLab3Evidence, *euLab4History)
	}{
		{"wrong epoch", func(_ *euLab3Evidence, h *euLab4History) { h.Segments[0].CacheEpoch = "old" }},
		{"zero", func(_ *euLab3Evidence, h *euLab4History) { h.Segments[0].Samples[0].Value = json.RawMessage("0") }},
		{"null", func(_ *euLab3Evidence, h *euLab4History) { h.Segments[0].Samples[0].Value = json.RawMessage("null") }},
		{"later fabricated zero", func(_ *euLab3Evidence, h *euLab4History) {
			later := h.Segments[0].Samples[0]
			later.IngestedAt = euLab3TestAt(66)
			later.Value = json.RawMessage("0")
			h.Segments[0].Samples = append(h.Segments[0].Samples, later)
		}},
		{"no stale quality", func(_ *euLab3Evidence, h *euLab4History) { h.Segments[0].Samples[0].Quality.IV = false }},
		{"outside outage", func(_ *euLab3Evidence, h *euLab4History) { h.Segments[0].Samples[0].IngestedAt = euLab3TestAt(59) }},
		{"no recovery", func(ev *euLab3Evidence, _ *euLab4History) { ev.journal.Entries = ev.journal.Entries[:4] }},
		{"old connection", func(ev *euLab3Evidence, _ *euLab4History) {
			ev.journal.Entries[5].Detail = euLab3TestJSON(t, map[string]int{"connection_seq": 1})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ev, h, _ := euLab4TestEvidence(t)
			test.change(&ev, h)
			if euLab4Outage(ev, h) {
				t.Fatal("unproven uncertainty passed")
			}
		})
	}
	t.Run("feed gap", func(t *testing.T) {
		ev, h, _ := euLab4TestEvidence(t)
		h.Segments = nil
		data := euLab3TestJSON(t, map[string]any{"point": "oltc-1-bus-kv", "gaps": []any{map[string]any{"from": euLab3TestAt(62), "to": euLab3TestAt(68)}}})
		if err := json.Unmarshal(data, h); err != nil {
			t.Fatal(err)
		}
		if !euLab4Outage(ev, h) {
			t.Fatal("real feed gap rejected")
		}
	})
}

func TestEULab4ClockRequiresTimedFreshOrderedSamples(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*euLab4History)
	}{
		{"untimed voltage", func(h *euLab4History) { h.Point = "oltc-1-bus-kv" }},
		{"old epoch", func(h *euLab4History) { h.Segments[0].CacheEpoch = "old" }},
		{"no shifted sample", func(h *euLab4History) { h.Segments[0].Samples = h.Segments[0].Samples[1:] }},
		{"no restoration", func(h *euLab4History) { h.Segments[0].Samples = h.Segments[0].Samples[:1] }},
		{"untimed sample", func(h *euLab4History) { h.Segments[0].Samples[0].SourceTime = nil }},
		{"invalid source time", func(h *euLab4History) { v := true; h.Segments[0].Samples[0].SourceTimeInvalid = &v }},
		{"missing validity", func(h *euLab4History) { h.Segments[0].Samples[0].SourceTimeInvalid = nil }},
		{"stale sample", func(h *euLab4History) { h.Segments[0].Samples[0].Quality.NT = true }},
		{"cached receipt", func(h *euLab4History) { v := euLab3TestAt(70); h.Segments[0].Samples[0].CCReceivedAt = &v }},
		{"repeated sequence", func(h *euLab4History) { h.Segments[0].Samples[1].SnapshotSeq = 80 }},
		{"no new receipt", func(h *euLab4History) { v := euLab3TestAt(80); h.Segments[0].Samples[1].CCReceivedAt = &v }},
		{"future ingest", func(h *euLab4History) { h.Segments[0].Samples[1].IngestedAt = euLab3TestAt(250) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ev, _, h := euLab4TestEvidence(t)
			test.change(h)
			if euLab4Clock(ev, h) {
				t.Fatal("unproven clock exercise passed")
			}
		})
	}
}

func TestEULab4NilHistoryAndWrongPolicyFail(t *testing.T) {
	ev, _, _ := euLab4TestEvidence(t)
	checks := evaluateEULab4(ev, nil, nil, false)
	for _, index := range []int{3, 4, 5} {
		if checks[index].Status != "fail" {
			t.Fatal(checks[index])
		}
	}
}
