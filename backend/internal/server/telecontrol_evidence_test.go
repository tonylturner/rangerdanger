package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// Wire fixtures of one passing exercise, in the contract shapes: the CC
// journal and state (§6, §13), the device state and audit (§4) and a
// containd rule hit. The range started at 12:00:00; the CC lost its link
// at 12:02, the trip came at 12:03, the CC recovered at 12:05.
const (
	passJournal = `{"cache_epoch":"c0ffee01","latest_seq":12,"oldest_seq":1,"entries":[
	 {"seq":1,"at":"2026-10-09T12:00:01Z","kind":"link_up","detail":{"peer":"10.30.30.20:2404","connection_seq":1}},
	 {"seq":2,"at":"2026-10-09T12:00:01.1Z","kind":"gi_started","detail":{"connection_seq":1}},
	 {"seq":3,"at":"2026-10-09T12:00:02Z","kind":"gi_complete","detail":{"connection_seq":1,"points_good":4}},
	 {"seq":4,"at":"2026-10-09T12:02:00Z","kind":"link_down","detail":{"peer":"10.30.30.20:2404","connection_seq":1,"reason":"t1_timeout"}},
	 {"seq":5,"at":"2026-10-09T12:02:00.1Z","kind":"point_stale","detail":{"point":"feeder-breaker-position","stale_reason":"link_lost","value":"closed"}},
	 {"seq":6,"at":"2026-10-09T12:02:00.1Z","kind":"point_stale","detail":{"point":"feeder-current","stale_reason":"link_lost","value":28.9}},
	 {"seq":7,"at":"2026-10-09T12:05:00Z","kind":"link_up","detail":{"peer":"10.30.30.20:2404","connection_seq":2}},
	 {"seq":8,"at":"2026-10-09T12:05:00.1Z","kind":"gi_started","detail":{"connection_seq":2}},
	 {"seq":9,"at":"2026-10-09T12:05:00.5Z","kind":"point_good","detail":{"point":"feeder-breaker-position","value":"open"}},
	 {"seq":10,"at":"2026-10-09T12:05:01Z","kind":"gi_complete","detail":{"connection_seq":2,"points_good":4}},
	 {"seq":11,"at":"2026-10-09T12:06:00Z","kind":"command","detail":{"id":"9","point":"feeder-breaker-command","operation":"close","status":"rejected"}},
	 {"seq":12,"at":"2026-10-09T12:06:01Z","kind":"point_good","detail":{"point":"feeder-current","value":0}}]}`
	passCCState = `{"schema":1,"cache_epoch":"c0ffee01","link":{"state":"started","peer":"10.30.30.20:2404","since":"2026-10-09T12:05:00Z","last_rx_at":"2026-10-09T12:06:01Z","connection_seq":2,"gi":{"state":"complete","started_at":"2026-10-09T12:05:00.1Z","completed_at":"2026-10-09T12:05:01Z"}},"points":[
	 {"id":"feeder-breaker-position","ioa":1001,"asdu":"M_DP_TB_1","value":"open","quality":{"iv":false,"nt":false,"sb":false,"bl":false,"ov":null},"stale_reason":null,"source_time":"2026-10-09T12:03:00Z","source_time_invalid":false,"received_at":"2026-10-09T12:05:00.5Z","cot":20},
	 {"id":"feeder-protection-trip","ioa":1002,"asdu":"M_SP_TB_1","value":true,"quality":{"iv":false,"nt":false,"sb":false,"bl":false,"ov":null},"stale_reason":null,"source_time":"2026-10-09T12:03:00Z","source_time_invalid":false,"received_at":"2026-10-09T12:05:00.5Z","cot":20},
	 {"id":"feeder-current","ioa":2001,"asdu":"M_ME_NC_1","value":0,"quality":{"iv":false,"nt":false,"sb":false,"bl":false,"ov":false},"stale_reason":null,"source_time":null,"source_time_invalid":null,"received_at":"2026-10-09T12:05:00.5Z","cot":20},
	 {"id":"feeder-voltage","ioa":2002,"asdu":"M_ME_NC_1","value":0,"quality":{"iv":false,"nt":false,"sb":false,"bl":false,"ov":false},"stale_reason":null,"source_time":null,"source_time_invalid":null,"received_at":"2026-10-09T12:05:00.5Z","cot":20}]}`
	passAudit = `{"generation":3735928559,"latest_seq":4,"oldest_seq":1,"entries":[
	 {"seq":1,"at":"2026-10-09T12:03:00Z","operation":"fault","result":"accepted","before":{"revision":3},"after":{"revision":4},"context":{"peer":"10.99.99.3:41000","protocol":"instructor","zone":"mgmt"}},
	 {"seq":2,"at":"2026-10-09T12:03:00Z","operation":"local_protection_trip","result":"accepted","before":{"revision":3},"after":{"revision":4},"context":{"peer":"protection","protocol":"local-protection","zone":"field"},"detail":{"cause":"fault_active"}},
	 {"seq":3,"at":"2026-10-09T12:03:01Z","operation":"first_solve","result":"accepted","before":{"revision":4},"after":{"revision":4},"context":{"peer":"10.50.50.20:50000","protocol":"process","zone":"process"},"detail":{"revision":4,"solver_epoch":"e1","solve_seq":181,"solved_at":"2026-10-09T12:03:01Z","current_a":0}},
	 {"seq":4,"at":"2026-10-09T12:03:01Z","operation":"measurements_valid","result":"accepted","before":{"revision":4},"after":{"revision":4},"context":{"peer":"10.50.50.20:50000","protocol":"process","zone":"process"}}]}`
	passDevice = `{"schema":1,"device_id":"feeder-breaker","generation":3735928559,"revision":4,"position":"open","remote_enabled":true,"lockout":true,"fault_active":true,"protection_tripped":true,"event_seq":4,"changed_at":"2026-10-09T12:03:00Z","measurements":{"current_a":0,"voltage_kv_ll":0,"valid":true,"solver_epoch":"e1","solve_seq":300,"solved_at":"2026-10-09T12:06:00Z","received_at":"2026-10-09T12:06:00Z"}}`
	passEvents = `[
	 {"id":90,"kind":"firewall.rule.hit","attributes":{"ruleId":"deny-unauthorized-iec104","action":"DENY","via":"nflog","proto":"tcp","port":"2404"},"timestamp":"2026-10-09T12:01:00Z","srcIp":"10.10.10.50","dstIp":"10.30.30.20","srcPort":51000,"dstPort":2404,"transport":"tcp"},
	 {"id":91,"kind":"firewall.rule.hit","attributes":{"ruleId":"control-centre-to-rtu-iec104","action":"ALLOW","via":"nflog"},"timestamp":"2026-10-09T12:00:30Z","srcIp":"10.60.60.20","dstIp":"10.30.30.20","srcPort":40000,"dstPort":2404,"transport":"tcp"}]`
)

func decodeFixture[T any](t *testing.T, data string) *T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return &out
}

// passingEvidence is the evidence of a complete exercise.
func passingEvidence(t *testing.T) telecontrolEvidence {
	return telecontrolEvidence{
		activePolicy: "improved",
		rangeStart:   euStartedAt,
		rtuIP:        "10.30.30.20",
		ccIP:         "10.60.60.20",
		events:       *decodeFixture[[]containd.Event](t, passEvents),
		ccState:      decodeFixture[ccState](t, passCCState),
		journal:      decodeFixture[ccJournal](t, passJournal),
		device:       decodeFixture[deviceTruth](t, passDevice),
		audit:        decodeFixture[deviceAudit](t, passAudit),
	}
}

func at(clock string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, "2026-10-09T"+clock+"Z")
	if err != nil {
		panic(err)
	}
	return parsed
}

// auditEntryAt is the passing audit's entry with operation op.
func auditEntryAt(ev *telecontrolEvidence, op string) *auditEntry {
	for i := range ev.audit.Entries {
		if ev.audit.Entries[i].Operation == op {
			return &ev.audit.Entries[i]
		}
	}
	panic("no audit entry " + op)
}

func journalEntryAt(ev *telecontrolEvidence, seq uint64) *journalEntry {
	for i := range ev.journal.Entries {
		if ev.journal.Entries[i].Seq == seq {
			return &ev.journal.Entries[i]
		}
	}
	panic("no journal entry")
}

func dropJournal(ev *telecontrolEvidence, seqs ...uint64) {
	kept := ev.journal.Entries[:0]
	for _, entry := range ev.journal.Entries {
		drop := false
		for _, seq := range seqs {
			drop = drop || entry.Seq == seq
		}
		if !drop {
			kept = append(kept, entry)
		}
	}
	ev.journal.Entries = kept
}

func pointAt(ev *telecontrolEvidence, id string) *ccPoint {
	for i := range ev.ccState.Points {
		if ev.ccState.Points[i].ID == id {
			return &ev.ccState.Points[i]
		}
	}
	panic("no point " + id)
}

func TestEvaluateTelecontrol(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*telecontrolEvidence)
		check  string // the one failing check; "" passes
		detail string
	}{
		{"passing sequence", func(*telecontrolEvidence) {}, "", ""},

		{"weak policy active", func(ev *telecontrolEvidence) { ev.activePolicy = "weak" }, checkPolicy, `the active policy is "weak"`},
		{"custom policy active", func(ev *telecontrolEvidence) { ev.activePolicy = "custom" }, checkPolicy, `the active policy is "custom"`},
		{"running firewall differs from the improved file", func(ev *telecontrolEvidence) {
			ev.policyErr = errors.New("running firewall hash a, policy file hash b")
		},
			checkPolicy, "not the improved policy file"},

		{"no deny", func(ev *telecontrolEvidence) { ev.events = ev.events[1:] }, checkDeny, "no firewall.rule.hit DENY to 10.30.30.20:2404"},
		{"deny before the range started", func(ev *telecontrolEvidence) { ev.events[0].Timestamp = at("11:59:59") }, checkDeny, "since the range started"},
		{"deny of the control centre", func(ev *telecontrolEvidence) { ev.events[0].Source = "10.60.60.20" }, checkDeny, "other than the control centre 10.60.60.20"},
		{"deny on another port", func(ev *telecontrolEvidence) { ev.events[0].DstPort = 502 }, checkDeny, "no firewall.rule.hit DENY"},
		{"deny to another host", func(ev *telecontrolEvidence) { ev.events[0].Dest = "10.40.40.20" }, checkDeny, "no firewall.rule.hit DENY"},
		{"allow, not deny", func(ev *telecontrolEvidence) { ev.events[0].Attributes["action"] = "ALLOW" }, checkDeny, "no firewall.rule.hit DENY"},
		{"events unavailable", func(ev *telecontrolEvidence) { ev.eventsErr = errors.New("get events failed") }, checkDeny, "firewall events unavailable"},

		{"CC restarted: epoch changed", func(ev *telecontrolEvidence) { ev.ccState.CacheEpoch = "beef0002" }, checkOutage, "cache epoch changed"},
		{"journal truncated", func(ev *telecontrolEvidence) { ev.journal.OldestSeq = 5 }, checkOutage, "journal is truncated"},
		{"journal unavailable", func(ev *telecontrolEvidence) { ev.journalErr = errors.New("control centre unreachable") }, checkOutage, "CC journal unavailable"},
		{"no link_down", func(ev *telecontrolEvidence) { dropJournal(ev, 4) }, checkOutage, "no link_down entry"},
		{"no stale position", func(ev *telecontrolEvidence) { dropJournal(ev, 5) }, checkOutage, "no point_stale for feeder-breaker-position"},
		{"stale of another point only", func(ev *telecontrolEvidence) { journalEntryAt(ev, 5).Detail.Point = "feeder-voltage" }, checkOutage, "no point_stale for feeder-breaker-position"},
		{"no recovery GI", func(ev *telecontrolEvidence) { dropJournal(ev, 10) }, checkOutage, "no later gi_complete"},
		{"GI on the same connection", func(ev *telecontrolEvidence) {
			one := uint64(1)
			journalEntryAt(ev, 10).Detail.ConnectionSeq = &one
		}, checkOutage, "no later gi_complete"},
		{"no link_up for the new connection", func(ev *telecontrolEvidence) { dropJournal(ev, 7) }, checkOutage, "no link_up for connection 2"},

		{"device restarted: generation changed", func(ev *telecontrolEvidence) { ev.device.Generation = 1 }, checkTrip, "device generation changed"},
		{"no trip", func(ev *telecontrolEvidence) { auditEntryAt(ev, "local_protection_trip").Operation = "fault" }, checkTrip, "no local-protection trip"},
		{"trip not by local protection", func(ev *telecontrolEvidence) {
			auditEntryAt(ev, "local_protection_trip").Context.Protocol = "instructor"
		}, checkTrip, "no local-protection trip"},
		{"trip before the link went down", func(ev *telecontrolEvidence) {
			auditEntryAt(ev, "local_protection_trip").At = at("12:01:59")
		}, checkTrip, "no local-protection trip between a link_down and its recovery GI"},
		{"trip after the recovery GI", func(ev *telecontrolEvidence) {
			auditEntryAt(ev, "local_protection_trip").At = at("12:05:02")
			auditEntryAt(ev, "first_solve").At = at("12:05:03")
		}, checkTrip, "no local-protection trip between a link_down and its recovery GI"},
		{"first solve after the recovery GI", func(ev *telecontrolEvidence) { auditEntryAt(ev, "first_solve").At = at("12:05:02") }, checkTrip, "no first_solve"},
		{"first solve before the trip", func(ev *telecontrolEvidence) { auditEntryAt(ev, "first_solve").At = at("12:02:59") }, checkTrip, "no first_solve"},
		{"first solve of another revision", func(ev *telecontrolEvidence) {
			auditEntryAt(ev, "first_solve").Detail = json.RawMessage(`{"revision":3}`)
		}, checkTrip, "no first_solve"},
		{"no first solve", func(ev *telecontrolEvidence) { auditEntryAt(ev, "first_solve").Operation = "measurements" }, checkTrip, "no first_solve"},
		{"audit unavailable", func(ev *telecontrolEvidence) { ev.auditErr = errors.New("instructor device unreachable") }, checkTrip, "device audit unavailable"},

		{"position point stale now", func(ev *telecontrolEvidence) {
			p := pointAt(ev, positionPoint)
			p.Quality.IV, p.Quality.NT = true, true
			reason := "link_lost"
			p.StaleReason = &reason
		}, checkOperator, "feeder-breaker-position is not GOOD (IV, NT, stale_reason link_lost)"},
		{"measurement invalid now", func(ev *telecontrolEvidence) { pointAt(ev, "feeder-current").Quality.IV = true }, checkOperator, "feeder-current is not GOOD (IV)"},
		{"measurement overflow now", func(ev *telecontrolEvidence) {
			overflow := true
			pointAt(ev, "feeder-voltage").Quality.OV = &overflow
		}, checkOperator, "feeder-voltage is not GOOD (OV)"},
		{"point missing", func(ev *telecontrolEvidence) { ev.ccState.Points = ev.ccState.Points[:3] }, checkOperator, "feeder-voltage is missing"},
		{"position differs from the device", func(ev *telecontrolEvidence) { ev.device.Position = "closed" }, checkOperator, `the operator's breaker position "open" differs from the device's "closed"`},
		{"device state unavailable", func(ev *telecontrolEvidence) { ev.deviceErr = errors.New("instructor device unreachable") }, "", "device state unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ev := passingEvidence(t)
			test.mutate(&ev)
			checks := evaluateTelecontrol(ev)
			if len(checks) != 5 {
				t.Fatalf("checks = %d, want 5", len(checks))
			}
			failing := map[string]ValidationCheck{}
			for _, check := range checks {
				if check.Status != "pass" {
					failing[check.Name] = check
				}
			}
			if test.check == "" && test.detail == "" {
				if len(failing) != 0 {
					t.Fatalf("passing sequence failed: %+v", failing)
				}
				return
			}
			if test.check == "" {
				// A missing source fails every check that reads it.
				if len(failing) == 0 {
					t.Fatal("missing evidence passed")
				}
				for _, check := range failing {
					if !strings.Contains(check.Detail, test.detail) || !strings.HasPrefix(check.Detail, "missing evidence: ") {
						t.Errorf("%s: %q, want %q", check.Name, check.Detail, test.detail)
					}
				}
				return
			}
			want := 1
			if test.check == checkOutage {
				// Without an outage window the trip has nowhere to be placed.
				want = 2
				if trip := failing[checkTrip]; !strings.Contains(trip.Detail, "no control-centre outage window") {
					t.Errorf("trip check = %+v, want it to name the missing window", trip)
				}
			}
			if len(failing) != want {
				t.Fatalf("failing checks = %+v, want only %q", failing, test.check)
			}
			check, ok := failing[test.check]
			if !ok || check.Status != "fail" || !strings.HasPrefix(check.Detail, "missing evidence: ") || !strings.Contains(check.Detail, test.detail) {
				t.Errorf("%s = %+v, want fail naming %q", test.check, check, test.detail)
			}
		})
	}
}

// TestEvaluateTelecontrolPicksTheOutageWithTheTrip passes when the trip
// falls in a later of two outages.
func TestEvaluateTelecontrolPicksTheOutageWithTheTrip(t *testing.T) {
	ev := passingEvidence(t)
	two, three := uint64(2), uint64(3)
	extra := []journalEntry{
		{Seq: 13, At: at("12:10:00"), Kind: "link_down"},
		{Seq: 14, At: at("12:10:00.1"), Kind: "point_stale"},
		{Seq: 15, At: at("12:12:00"), Kind: "link_up"},
		{Seq: 16, At: at("12:12:01"), Kind: "gi_complete"},
	}
	extra[0].Detail.ConnectionSeq = &two
	extra[1].Detail.Point = positionPoint
	extra[2].Detail.ConnectionSeq = &three
	extra[3].Detail.ConnectionSeq = &three
	ev.journal.Entries = append(ev.journal.Entries, extra...)
	auditEntryAt(&ev, "local_protection_trip").At = at("12:11:00")
	auditEntryAt(&ev, "first_solve").At = at("12:11:01")
	for _, check := range evaluateTelecontrol(ev) {
		if check.Status != "pass" {
			t.Errorf("%s: %s", check.Name, check.Detail)
		}
	}
}
