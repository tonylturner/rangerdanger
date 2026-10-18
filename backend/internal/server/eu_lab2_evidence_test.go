package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

func passingConduits(t *testing.T) conduitEvidence {
	t.Helper()
	gen := euGeneration(t, nil, nil)
	ev := conduitEvidence{
		stationPoints: gen.Package.StationPoints,
		rangeStart:    at("12:00:00"), collectedAt: at("12:05:00"),
		state: decodeFixture[conduitState](t, `{"schema":3,"cache_epoch":"cc-2","link":{"state":"started","connection_seq":2}}`),
		journal: decodeFixture[ccJournal](t, `{"cache_epoch":"cc-2","oldest_seq":1,"latest_seq":6,"entries":[
   {"seq":1,"at":"2026-10-09T12:01:00Z","kind":"gi_started","detail":{"connection_seq":2,"trigger":"connect"}},
   {"seq":2,"at":"2026-10-09T12:01:01Z","kind":"gi_complete","detail":{"connection_seq":2,"trigger":"connect"}}]}`),
		audit: decodeFixture[conduitAudit](t, `{"generation":42,"entries":[
   {"seq":1,"at":"2026-10-09T12:02:00Z","operation":"plc_request","result":"accepted","before":{"tap":0},"after":{"revision":2,"tap":0,"target_tap":1,"moving":true},"context":{"peer":"10.30.30.30:12345","protocol":"modbus"}},
   {"seq":2,"at":"2026-10-09T12:02:02Z","operation":"motion_complete","result":"accepted","after":{"revision":3,"tap":1,"target_tap":1,"moving":false}},
   {"seq":3,"at":"2026-10-09T12:02:03Z","operation":"first_solve","result":"accepted","detail":{"revision":3}}]}`),
		device: &deviceTruth{Schema: 3, Generation: 42}, plcIP: "10.30.30.30",
		history: decodeFixture[conduitHistory](t, `{"point":"oltc-1-tap","segments":[{"cache_epoch":"cc-2","samples":[
   {"snapshot_seq":100,"value":{"value":1,"transient":false},"cc_received_at":"2026-10-09T12:04:57Z","ingested_at":"2026-10-09T12:04:58Z"},
   {"snapshot_seq":101,"value":{"value":1,"transient":false},"cc_received_at":"2026-10-09T12:04:58Z","ingested_at":"2026-10-09T12:04:59Z"}]}]}`),
		clock:     checkPass("RTU station-clock HTTP operation", "GET"),
		reads:     []ValidationCheck{checkPass("Enterprise historian read", "GET"), checkPass("Named engineering PLC maintenance GET", "GET")},
		readStart: at("12:04:58"), readEnd: at("12:04:59"),
	}
	for _, identity := range ev.stationPoints {
		ioa := identity.IOA
		value := json.RawMessage(`true`)
		switch {
		case ioa == 1001 || ioa == 1101:
			value = json.RawMessage(`"closed"`)
		case ioa == 1201:
			value = json.RawMessage(`"open"`)
		case ioa == 1301:
			value = json.RawMessage(`{"value":1,"transient":false}`)
		case ioa >= 2000:
			value = json.RawMessage(`20.0`)
		}
		point := conduitPoint{ccPoint: ccPoint{ID: identity.ID, Value: value, ReceivedAt: at("12:01:00")}, IOA: ioa}
		ev.state.Points = append(ev.state.Points, point)
	}

	var err error
	ev.denies, err = conduitDenyMatrix(gen, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range ev.denies {
		ev.events = append(ev.events, containd.Event{Kind: "firewall.rule.hit", Timestamp: at("12:04:00"), Source: row.source, Dest: row.dest, DstPort: row.port, Transport: "tcp", Attributes: map[string]any{"action": "DENY"}})
	}
	return ev
}

func TestConduitsEvidence(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*conduitEvidence)
		failing []int
	}{
		{"complete proof", func(*conduitEvidence) {}, nil},
		{"schema one", func(ev *conduitEvidence) { ev.state.Schema = 1 }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"epoch mismatch", func(ev *conduitEvidence) { ev.state.CacheEpoch = "other" }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"CC disconnected", func(ev *conduitEvidence) { ev.state.Link.State = "disconnected" }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"CC unavailable", func(ev *conduitEvidence) { ev.state = nil; ev.stateErr = errors.New("unavailable") }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"journal unavailable", func(ev *conduitEvidence) { ev.journal = nil; ev.journalErr = errors.New("unavailable") }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"start evicted", func(ev *conduitEvidence) { ev.journal.OldestSeq = 2 }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"no GI", func(ev *conduitEvidence) { ev.journal.Entries = ev.journal.Entries[:1] }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"old connection GI", func(ev *conduitEvidence) { n := uint64(1); ev.journal.Entries[1].Detail.ConnectionSeq = &n }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"pre-generation connection", func(ev *conduitEvidence) { ev.rangeStart = at("12:01:30") }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"expired attempt", func(ev *conduitEvidence) { ev.collectedAt = at("12:12:00") }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"future GI", func(ev *conduitEvidence) { ev.journal.Entries[1].At = at("12:06:00") }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"zero connection seq", func(ev *conduitEvidence) { ev.state.Link.ConnectionSeq = 0 }, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"missing point", func(ev *conduitEvidence) { ev.state.Points = ev.state.Points[1:] }, []int{1}},
		{"missing inventory", func(ev *conduitEvidence) { ev.stationPoints = nil }, []int{1}},
		{"missing C4 point", func(ev *conduitEvidence) {
			for i, point := range ev.state.Points {
				if point.IOA == 2305 {
					ev.state.Points = append(ev.state.Points[:i], ev.state.Points[i+1:]...)
					return
				}
			}
		}, []int{1}},
		{"duplicate point", func(ev *conduitEvidence) { ev.state.Points = append(ev.state.Points, ev.state.Points[0]) }, []int{1}},
		{"stale quality", func(ev *conduitEvidence) { ev.state.Points[0].Quality.NT = true }, []int{1}},
		{"null is not zero", func(ev *conduitEvidence) { ev.state.Points[0].Value = json.RawMessage(`null`) }, []int{1}},
		{"quiet GOOD receipt", func(ev *conduitEvidence) { ev.state.Points[0].ReceivedAt = at("12:01:00") }, nil},
		{"GI own receipt before completion", func(ev *conduitEvidence) { ev.state.Points[0].ReceivedAt = at("12:01:00").Add(500 * time.Millisecond) }, nil},
		{"pre-attempt receipt", func(ev *conduitEvidence) { ev.state.Points[0].ReceivedAt = at("12:00:59") }, []int{1}},
		{"future receipt", func(ev *conduitEvidence) { ev.state.Points[0].ReceivedAt = at("12:05:01") }, []int{1}},
		{"audit unavailable", func(ev *conduitEvidence) { ev.audit = nil; ev.auditErr = errors.New("unavailable") }, []int{2}},
		{"device unavailable", func(ev *conduitEvidence) { ev.device = nil; ev.deviceErr = errors.New("unavailable") }, []int{2}},
		{"OLTC restarted", func(ev *conduitEvidence) { ev.device.Generation++ }, []int{2}},
		{"old OLTC schema", func(ev *conduitEvidence) { ev.device.Schema = 1 }, []int{2}},
		{"PLC no longer running", func(ev *conduitEvidence) {
			for i := range ev.state.Points {
				if ev.state.Points[i].IOA == 1401 {
					ev.state.Points[i].Value = json.RawMessage(`false`)
				}
			}
		}, []int{2}},
		{"OLTC no longer auto", func(ev *conduitEvidence) {
			for i := range ev.state.Points {
				if ev.state.Points[i].IOA == 1303 {
					ev.state.Points[i].Value = json.RawMessage(`false`)
				}
			}
		}, []int{2}},
		{"wrong PLC peer", func(ev *conduitEvidence) { ev.audit.Entries[0].Context.Peer = "10.30.30.20:12345" }, []int{2}},
		{"wrong protocol", func(ev *conduitEvidence) { ev.audit.Entries[0].Context.Protocol = "lab" }, []int{2}},
		{"ignored PLC write", func(ev *conduitEvidence) { ev.audit.Entries[0].Result = "ignored" }, []int{2}},
		{"pre-attempt PLC write", func(ev *conduitEvidence) { ev.audit.Entries[0].At = at("12:00:59") }, []int{2}},
		{"non-adjacent target", func(ev *conduitEvidence) { ev.audit.Entries[0].After.TargetTap = 2 }, []int{2}},
		{"no completed motion", func(ev *conduitEvidence) { ev.audit.Entries[1].Operation = "other" }, []int{2}},
		{"wrong completed tap", func(ev *conduitEvidence) { ev.audit.Entries[1].After.Tap = 0 }, []int{2}},
		{"wrong solved revision", func(ev *conduitEvidence) { ev.audit.Entries[2].Detail = json.RawMessage(`{"revision":2}`) }, []int{2}},
		{"future solved revision", func(ev *conduitEvidence) { ev.audit.Entries[2].At = at("12:05:01") }, []int{2}},
		{"solve before completion", func(ev *conduitEvidence) { ev.audit.Entries[2].At = at("12:02:01") }, []int{2}},
		{"history unavailable", func(ev *conduitEvidence) { ev.history = nil; ev.historyErr = errors.New("unavailable") }, []int{3}},
		{"history wrong epoch", func(ev *conduitEvidence) { ev.history.Segments[0].CacheEpoch = "old" }, []int{3}},
		{"history old observation", func(ev *conduitEvidence) {
			ts := at("12:00:59")
			for i := range ev.history.Segments[0].Samples {
				ev.history.Segments[0].Samples[i].CCReceivedAt = &ts
			}
		}, []int{3}},
		{"history null value", func(ev *conduitEvidence) { ev.history.Segments[0].Samples[0].Value = json.RawMessage(`null`) }, []int{3}},
		{"history stalled snapshot", func(ev *conduitEvidence) { ev.history.Segments[0].Samples[1].SnapshotSeq = 100 }, []int{3}},
		{"history old ingest", func(ev *conduitEvidence) { ev.history.Segments[0].Samples[1].IngestedAt = at("12:04:56") }, []int{3}},
		{"history future ingest", func(ev *conduitEvidence) { ev.history.Segments[0].Samples[1].IngestedAt = at("12:05:01") }, []int{3}},
		{"clock unavailable", func(ev *conduitEvidence) {
			ev.clock = checkFail("RTU station-clock HTTP operation", "missing evidence: unavailable")
		}, []int{4}},
		{"events unavailable", func(ev *conduitEvidence) { ev.eventsErr = errors.New("unavailable") }, []int{5}},
		{"missing one deny", func(ev *conduitEvidence) { ev.events = ev.events[1:] }, []int{5}},
		{"old deny", func(ev *conduitEvidence) { ev.events[0].Timestamp = at("12:00:59") }, []int{5}},
		{"future deny", func(ev *conduitEvidence) { ev.events[0].Timestamp = at("12:05:01") }, []int{5}},
		{"allow is not deny", func(ev *conduitEvidence) { ev.events[0].Attributes["action"] = "ALLOW" }, []int{5}},
		{"UDP not TCP", func(ev *conduitEvidence) { ev.events[0].Transport = "udp" }, []int{5}},
		{"source identity missing", func(ev *conduitEvidence) { ev.identityErr = errors.New("missing PLC") }, []int{2, 5}},
		{"GET before reconnect", func(ev *conduitEvidence) { ev.readStart = at("12:00:59") }, []int{4, 6, 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := passingConduits(t)
			tt.mutate(&ev)
			checks := evaluateConduits(ev)
			if len(checks) != 8 {
				t.Fatalf("checks=%d", len(checks))
			}
			want := map[int]bool{}
			for _, i := range tt.failing {
				want[i] = true
			}
			for i, check := range checks {
				if (check.Status != "pass") != want[i] {
					t.Errorf("check %d %s = %s: %s", i, check.Name, check.Status, check.Detail)
				}
				if check.Status != "pass" && !strings.HasPrefix(check.Detail, "missing evidence: ") {
					t.Errorf("missing evidence not explicit: %+v", check)
				}
			}
		})
	}
}

func TestConduitsPollsInventoryCount(t *testing.T) {
	ev := passingConduits(t)
	if len(ev.stationPoints) != 42 || len(ev.state.Points) != 42 {
		t.Fatalf("loaded inventory=%d, observations=%d; want 42", len(ev.stationPoints), len(ev.state.Points))
	}
	start, _ := conduitWindow(ev)
	check := conduitPolls(ev, start)
	if check.Status != "pass" || !strings.Contains(check.Detail, "all 42 station IOAs") {
		t.Fatal(check)
	}
	for i, point := range ev.state.Points {
		if point.IOA == 1307 {
			ev.state.Points = append(ev.state.Points[:i], ev.state.Points[i+1:]...)
			break
		}
	}
	check = conduitPolls(ev, start)
	if check.Status != "fail" || !strings.Contains(check.Detail, "1 of 42 station IOAs [1307]") {
		t.Fatal(check)
	}
}

func TestConduitsSurvivesLinkUpEviction(t *testing.T) {
	ev := passingConduits(t)
	retained := ev.journal.Entries[:0]
	for _, entry := range ev.journal.Entries {
		if entry.Kind != "link_up" {
			retained = append(retained, entry)
		}
	}
	ev.journal.Entries = retained

	for _, check := range evaluateConduits(ev) {
		if check.Status != "pass" {
			t.Errorf("proof failed without retained link_up: %+v", check)
		}
	}
}

func TestConduitsDenyMatrix(t *testing.T) {
	ev := passingConduits(t)
	if len(ev.denies) != 23 {
		t.Fatalf("deny pairs=%d, want23", len(ev.denies))
	}
	// In particular, vendor and engineering are different hosts, not a dmz
	// aggregate that could certify an overbroad maintenance allow.
	for _, source := range []string{"10.10.10.50", "10.20.20.10", "10.20.20.20"} {
		for _, dest := range []string{"10.40.40.20", "10.40.40.21", "10.40.40.22", "10.40.40.23", "10.40.40.24"} {
			found := false
			for _, row := range ev.denies {
				found = found || (row.source == source && row.dest == dest && row.port == 502)
			}
			if !found {
				t.Errorf("missing %s→%s:502", source, dest)
			}
		}
	}
}

func TestConduitsPeriodicGIDoesNotRestartAttempt(t *testing.T) {
	ev := passingConduits(t)
	started, complete := ev.journal.Entries[0], ev.journal.Entries[1]
	started.Seq, complete.Seq = 5, 6
	started.At, complete.At = ev.collectedAt.Add(-2*time.Second), ev.collectedAt.Add(-time.Second)
	ev.journal.Entries = append(ev.journal.Entries, started, complete)
	for _, check := range evaluateConduits(ev) {
		if check.Status != "pass" {
			t.Errorf("periodic GI invalidated proof: %+v", check)
		}
	}
}

func TestConduitsGIAnchor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*conduitEvidence)
		pass   bool
	}{
		{"quiet GI receipt before completion", func(*conduitEvidence) {}, true},
		{"start missing", func(ev *conduitEvidence) { ev.journal.Entries = ev.journal.Entries[1:] }, false},
		{"start after completion sequence", func(ev *conduitEvidence) { ev.journal.Entries[0].Seq = 3 }, false},
		{"start after completion time", func(ev *conduitEvidence) { ev.journal.Entries[0].At = at("12:01:02") }, false},
		{"GI starts before boundary", func(ev *conduitEvidence) { ev.rangeStart = at("12:01:00").Add(500 * time.Millisecond) }, false},
		{"GI outside ten minute cap", func(ev *conduitEvidence) { ev.collectedAt = at("12:12:00") }, false},
		{"later running periodic GI", func(ev *conduitEvidence) {
			entry := ev.journal.Entries[0]
			entry.Seq = 5
			entry.At = at("12:04:00")
			ev.journal.Entries = append(ev.journal.Entries, entry)
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := passingConduits(t)
			tc.mutate(&ev)
			start, check := conduitWindow(ev)
			if (check.Status == "pass") != tc.pass {
				t.Fatalf("%+v", check)
			}
			if tc.pass {
				if !start.Equal(at("12:01:00")) {
					t.Fatalf("anchor = %s", start)
				}
				if check := conduitPolls(ev, start); check.Status != "pass" {
					t.Fatal(check)
				}
			} else if !strings.Contains(check.Detail, "Interrogate station") {
				t.Fatal(check)
			}
		})
	}
}
