package server

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var euWorkshopSlots = []string{"eu-baseline-assessment", "eu-firewall-implementation", "eu-hardening-configurations", "eu-vendor-rdp-compromise", "eu-validation-evidence"}

func passingEUWorkshop(t *testing.T, slot string) euWorkshopEvidence {
	t.Helper()
	conduit := passingConduits(t)
	ev := euWorkshopEvidence{
		stationPoints: conduit.stationPoints,
		start:         conduit.rangeStart, now: conduit.collectedAt, policyAt: at("12:00:30"), operationEnd: at("12:05:00"), hardeningSince: at("12:00:30"), policy: "custom", capture: true,
		cc: conduit.state, journal: conduit.journal, conduits: conduit,
		devices: map[string]*euWorkshopDevice{}, deviceErrors: map[string]error{},
		process: decodeFixture[euWorkshopProcess](t, `{"schema":3,"truth_stale":false,"solved_at":"2026-10-09T12:04:59Z","buses":{"bus20":{"live":true,"kv":20},"s1":{"live":true,"kv":20},"s2":{"live":true,"kv":20},"s3":{"live":true,"kv":20}},"devices":{"f2-breaker":{"delivered":true,"revision":1,"solved_revision":1},"feeder-breaker":{"delivered":true,"revision":1,"solved_revision":1},"rmu-1":{"delivered":true,"revision":1,"solved_revision":1},"rmu-2":{"delivered":true,"revision":1,"solved_revision":1},"oltc-1":{"delivered":true,"revision":1,"solved_revision":1}}}`),
		ccIP:    "10.60.60.20", rtuIP: "10.30.30.20",
		rtuLog:       fmt.Sprintf("set-gateway: default gateway set to 10.30.30.2\n2026-10-09T12:00:00Z rtu listening 0.0.0.0:2404 ca=1 points=%d devices=6 select-timeout=5000ms\n", len(conduit.stationPoints)),
		breakerAudit: decodeFixture[euWorkshopBreakerAudit](t, `{"generation":42,"oldest_seq":1,"latest_seq":2,"entries":[{"seq":1,"at":"2026-10-09T12:00:00Z","operation":"reset","result":"accepted","context":{"protocol":"lab"}},{"seq":2,"at":"2026-10-09T12:02:00Z","operation":"first_solve","result":"accepted"}]}`),
	}
	for i := range ev.cc.Points {
		if ev.cc.Points[i].IOA >= 2000 || ev.cc.Points[i].IOA == 1301 {
			ev.cc.Points[i].Quality.OV = new(bool)
		}
	}
	for _, node := range []string{"breaker-1", "f2-breaker", "rmu-1", "rmu-2", "oltc-1"} {
		id := node
		if node == "breaker-1" {
			id = "feeder-breaker"
		}
		position := "closed"
		if node == "rmu-2" {
			position = "open"
		}
		ev.devices[node] = decodeFixture[euWorkshopDevice](t, fmt.Sprintf(`{"schema":3,"device_id":%q,"generation":42,"revision":1,"event_seq":2,"position":%q,"remote_enabled":true,"auto":true,"tap":0,"blocked":false,"moving":false}`, id, position))
	}
	if slot == euWorkshopSlots[0] {
		ev.policy = "weak"
	}
	filter := slot
	if slot == euWorkshopSlots[0] || slot == euWorkshopSlots[4] {
		filter = euWorkshopSlots[1]
	}
	matrix, err := euResolveMatrix(euGeneration(t, nil, nil), euRecipe.validation, filter)
	if err != nil {
		t.Fatal(err)
	}
	ev.negativeExpected = len(matrix)
	for _, row := range matrix {
		ev.negative = append(ev.negative, validationRow{validationProbe: row, Actual: "deny", Pass: true})
	}

	ev.cc.Link.Since = at("12:00:59")
	ev.policyHash = "full-running-hash"
	ev.report = &euRetainedReport{Hash: ev.policyHash, Markdown: "retained report", RangeStart: ev.start, PolicyAt: ev.policyAt, StartedAt: at("12:02:00"), FinishedAt: at("12:03:00"), Pass: true}
	return ev
}

func assertEUChecks(t *testing.T, slot string, ev euWorkshopEvidence, wantPass bool) {
	t.Helper()
	checks := evaluateEUWorkshop(slot, ev)
	if len(checks) == 0 {
		t.Fatal("no checks")
	}
	pass := true
	for _, check := range checks {
		if check.Status != "pass" {
			pass = false
		}
	}
	if pass != wantPass {
		t.Fatalf("pass=%t want=%t checks=%+v", pass, wantPass, checks)
	}
}

func TestEUWorkshopValidators(t *testing.T) {
	for _, slot := range euWorkshopSlots {
		t.Run(slot, func(t *testing.T) {
			ev := passingEUWorkshop(t, slot)
			assertEUChecks(t, slot, ev, true)
			if validator, exists := scenarioValidators[slot]; !exists || validator.run == nil {
				t.Fatal("missing workshop validator")
			}
			for _, mutation := range []struct {
				name  string
				apply func(*euWorkshopEvidence)
			}{
				{"missing running policy", func(ev *euWorkshopEvidence) { ev.policyErr = errors.New("down") }},
				{"wrong label", func(ev *euWorkshopEvidence) { ev.policy = "unknown" }},
			} {
				t.Run(mutation.name, func(t *testing.T) {
					ev := passingEUWorkshop(t, slot)
					mutation.apply(&ev)
					assertEUChecks(t, slot, ev, false)
				})
			}
			if slot != euWorkshopSlots[0] {
				ev.policy = "improved"
				assertEUChecks(t, slot, ev, true)
				ev.policy = "weak"
				assertEUChecks(t, slot, ev, false)
				ev = passingEUWorkshop(t, slot)
				ev.policyAt = time.Time{}
				assertEUChecks(t, slot, ev, false)
			}
		})
	}
}

func TestEUWorkshopFailures(t *testing.T) {
	cases := []struct {
		name, slot string
		mutate     func(*euWorkshopEvidence)
	}{
		{"capture absent", euWorkshopSlots[0], func(ev *euWorkshopEvidence) { ev.capture = false }},
		{"CC error", euWorkshopSlots[0], func(ev *euWorkshopEvidence) { ev.ccErr = errors.New("down") }},
		{"CC epoch", euWorkshopSlots[0], func(ev *euWorkshopEvidence) { ev.cc.CacheEpoch = "other" }},
		{"GI missing", euWorkshopSlots[0], func(ev *euWorkshopEvidence) { ev.journal.Entries = nil }},
		{"bad quality", euWorkshopSlots[0], func(ev *euWorkshopEvidence) { ev.cc.Points[0].Quality.NT = true }},
		{"breaker open", euWorkshopSlots[0], func(ev *euWorkshopEvidence) { ev.devices["breaker-1"].Position = "open" }},
		{"no supply", euWorkshopSlots[0], func(ev *euWorkshopEvidence) {
			b := ev.process.Buses["s1"]
			b.Live = new(bool)
			ev.process.Buses["s1"] = b
		}},
		{"unknown supply", euWorkshopSlots[0], func(ev *euWorkshopEvidence) { delete(ev.process.Buses, "s2") }},
		{"stale process", euWorkshopSlots[0], func(ev *euWorkshopEvidence) { ev.process.SolvedAt = at("12:04:56") }},
		{"missing device", euWorkshopSlots[0], func(ev *euWorkshopEvidence) { delete(ev.devices, "breaker-1") }},
		{"positive expired", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.conduits.collectedAt = at("12:12:00") }},
		{"positive after newer policy only", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.policyAt = at("12:04:50") }},
		{"poll failure", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.conduits.state.Points = nil }},
		{"PLC operation missing", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.conduits.audit = nil }},
		{"feed missing", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.conduits.history = nil }},
		{"HTTP GET failure", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.conduits.reads[0].Status = "fail" }},
		{"HTTP evidence missing", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.conduits.reads = nil }},
		{"HTTP before attempt", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.conduits.readStart = at("12:00:00") }},
		{"probe missing", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.negative = ev.negative[1:] }},
		{"probe allowed", euWorkshopSlots[1], func(ev *euWorkshopEvidence) { ev.negative[0].Actual = "allow"; ev.negative[0].Pass = false }},
		{"probe tool unavailable", euWorkshopSlots[2], func(ev *euWorkshopEvidence) {
			ev.negative[0].Actual = "skipped"
			ev.negative[0].ProbeError = "python/tool missing"
			ev.negative[0].Pass = false
		}},
		{"matrix identity unavailable", euWorkshopSlots[2], func(ev *euWorkshopEvidence) { ev.negativeErr = errors.New("no source network") }},
		{"foreign command", euWorkshopSlots[2], func(ev *euWorkshopEvidence) {
			ev.rtuLog += "2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.10.10.10:5000 phase=execute result=accepted dcs=1\n"
		}},
		{"log unavailable", euWorkshopSlots[2], func(ev *euWorkshopEvidence) { ev.logErr = errors.New("down") }},
		{"log truncated", euWorkshopSlots[2], func(ev *euWorkshopEvidence) { ev.rtuLog = "" }},
		{"audit truncated", euWorkshopSlots[2], func(ev *euWorkshopEvidence) { ev.breakerAudit.Entries = ev.breakerAudit.Entries[1:] }},
		{"audit generation", euWorkshopSlots[2], func(ev *euWorkshopEvidence) { ev.breakerAudit.Generation++ }},
		{"audit gap", euWorkshopSlots[2], func(ev *euWorkshopEvidence) { ev.breakerAudit.Entries[1].Seq = 4 }},
		{"direct foreign Modbus", euWorkshopSlots[2], func(ev *euWorkshopEvidence) {
			e := &ev.breakerAudit.Entries[1]
			e.Operation = "open"
			e.Context = peerAuditContext{Protocol: "modbus", Peer: "10.10.10.10:1234"}
		}},
		{"reset erases history", euWorkshopSlots[2], func(ev *euWorkshopEvidence) { ev.breakerAudit.Entries[1].Operation = "reset" }},
		{"bonus first hop missing", euWorkshopSlots[3], func(ev *euWorkshopEvidence) { ev.negative = ev.negative[1:] }},
		{"RMU not restored", euWorkshopSlots[3], func(ev *euWorkshopEvidence) { ev.devices["rmu-2"].Position = "closed" }},
		{"OLTC local", euWorkshopSlots[3], func(ev *euWorkshopEvidence) { ev.devices["oltc-1"].Remote = new(bool) }},
		{"OLTC unknown mode", euWorkshopSlots[3], func(ev *euWorkshopEvidence) { ev.devices["oltc-1"].Auto = nil }},
		{"device revision stale", euWorkshopSlots[4], func(ev *euWorkshopEvidence) { *ev.devices["rmu-1"].Revision = 2 }},
		{"F2 missing", euWorkshopSlots[4], func(ev *euWorkshopEvidence) { delete(ev.devices, "f2-breaker") }},
		{"F2 revision stale", euWorkshopSlots[4], func(ev *euWorkshopEvidence) { *ev.devices["f2-breaker"].Revision = 2 }},
		{"no delivery", euWorkshopSlots[4], func(ev *euWorkshopEvidence) { ev.process.Devices = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := passingEUWorkshop(t, tc.slot)
			tc.mutate(&ev)
			assertEUChecks(t, tc.slot, ev, false)
		})
	}
}

func TestEURTUPeerEvidence(t *testing.T) {
	base := "set-gateway: default gateway set to 10.30.30.2\n2026-10-09T12:00:00Z rtu listening 0.0.0.0:2404\n"
	for _, tc := range []struct {
		name, line string
		ok         bool
	}{
		{"no commands", "", true},
		{"authorized CC", "2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.60.60.20:1234 phase=execute result=accepted", true},
		{"foreign before hardening", "2026-10-09T12:00:01Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=accepted\n2026-10-09T12:00:02Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=completed", true},
		{"foreign completion across boundary", "2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=completed", false},
		{"ambiguous foreign operation", "2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=unknown", false},
		{"pending foreign across boundary", "2026-10-09T12:00:01Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=accepted", false},
		{"CC feedback timeout", "2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.60.60.20:1234 phase=execute result=accepted\n2026-10-09T12:03:05Z rtu command ioa=3001 peer=10.60.60.20:1234 phase=execute result=failed reason=feedback-timeout", true},
		{"foreign feedback timeout", "2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=failed", false},
		{"foreign failed before hardening", "2026-10-09T12:00:01Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=accepted\n2026-10-09T12:00:02Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=failed", true},
		{"unknown terminal outcome", "2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.60.60.20:1234 phase=execute result=unexpected", false},
		{"CC terminal cannot close foreign gap", "2026-10-09T12:00:01Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=accepted\n2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.60.60.20:1234 phase=execute result=rejected", false},
		{"CC accept cannot close foreign gap", "2026-10-09T12:00:01Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=accepted\n2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.60.60.20:1234 phase=execute result=accepted", false},
		{"foreign refused", "2026-10-09T12:03:00Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=rejected", true},
		{"bad peer", "2026-10-09T12:03:00Z rtu command ioa=3001 peer=? phase=execute result=accepted", false},
		{"RTU restart", "2026-10-09T12:03:00Z rtu listening 0.0.0.0:2404", false},
		{"RTU stopped", "2026-10-09T12:03:00Z rtu stopping", false},
		{"malformed", "incomplete-record", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := euRTUPeers(base+tc.line, "10.60.60.20", at("12:00:30"), at("12:05:00"))
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestEUValidatorScopeClaims(t *testing.T) {
	ev := passingEUWorkshop(t, euWorkshopSlots[2])
	for _, check := range evaluateEUWorkshop(euWorkshopSlots[2], ev) {
		if strings.Contains(check.Detail, "DPI") && !strings.Contains(check.Detail, "No DPI") {
			t.Fatalf("overclaim: %+v", check)
		}
	}
}

func TestEUNegativeProbesDoNotUseConnectionWindow(t *testing.T) {
	ev := passingEUWorkshop(t, euWorkshopSlots[1])
	ev.conduits.journal.Entries = nil
	ev.conduits.events = nil // no deny logs of any age
	ev.policyAt = at("12:00:30")
	if check := euWorkshopNegative(ev); check.Status != "pass" {
		t.Fatal(check)
	}
	ev.negative[0].Actual = "allow"
	ev.negative[0].Pass = false
	if check := euWorkshopNegative(ev); check.Status != "fail" {
		t.Fatal(check)
	}
}

func TestEUPostCommitVisibility(t *testing.T) {
	for _, slot := range []string{euWorkshopSlots[1], euWorkshopSlots[2]} {
		for _, tc := range []struct {
			name   string
			mutate func(*euWorkshopEvidence)
		}{
			{"stale session despite new GI", func(ev *euWorkshopEvidence) { ev.cc.Link.Since = ev.policyAt.Add(-time.Second) }},
			{"session at commit", func(ev *euWorkshopEvidence) { ev.cc.Link.Since = ev.policyAt }},
			{"missing session time", func(ev *euWorkshopEvidence) { ev.cc.Link.Since = time.Time{} }},
			{"no GI", func(ev *euWorkshopEvidence) { ev.journal.Entries = nil }},
			{"old connection GI", func(ev *euWorkshopEvidence) { ev.cc.Link.ConnectionSeq++ }},
			{"GI before connection", func(ev *euWorkshopEvidence) { ev.cc.Link.Since = at("12:01:02") }},
			{"non GOOD", func(ev *euWorkshopEvidence) { ev.cc.Points[0].Quality.NT = true }},
			{"null observation", func(ev *euWorkshopEvidence) { ev.cc.Points[0].Value = nil }},
			{"missing point", func(ev *euWorkshopEvidence) { ev.cc.Points = ev.cc.Points[1:] }},
			{"pre GI observation", func(ev *euWorkshopEvidence) { ev.cc.Points[0].ReceivedAt = at("12:00:59") }},
			{"missing supply", func(ev *euWorkshopEvidence) { delete(ev.process.Buses, "s2") }},
		} {
			t.Run(slot+"/"+tc.name, func(t *testing.T) {
				ev := passingEUWorkshop(t, slot)
				tc.mutate(&ev)
				assertEUChecks(t, slot, ev, false)
			})
		}
	}
	for _, node := range []string{"breaker-1", "f2-breaker", "rmu-1", "rmu-2"} {
		t.Run("continuity/"+node, func(t *testing.T) {
			ev := passingEUWorkshop(t, euWorkshopSlots[1])
			ev.devices[node].Position = "unknown"
			assertEUChecks(t, euWorkshopSlots[1], ev, false)
		})
	}
}

func TestEURetainedReportAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*euWorkshopEvidence)
	}{
		{"missing", func(ev *euWorkshopEvidence) { ev.report = nil }},
		{"failed", func(ev *euWorkshopEvidence) { ev.report.Pass = false }},
		{"hash mismatch", func(ev *euWorkshopEvidence) { ev.report.Hash = "other" }},
		{"reapplied same hash", func(ev *euWorkshopEvidence) { ev.policyAt = at("12:03:30") }},
		{"old range", func(ev *euWorkshopEvidence) { ev.report.RangeStart = ev.start.Add(-time.Hour) }},
		{"before commit", func(ev *euWorkshopEvidence) { ev.report.StartedAt = ev.start }},
		{"future", func(ev *euWorkshopEvidence) { ev.report.FinishedAt = ev.now.Add(time.Second) }},
		{"inverted window", func(ev *euWorkshopEvidence) { ev.report.FinishedAt = ev.report.StartedAt.Add(-time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := passingEUWorkshop(t, euWorkshopSlots[4])
			tc.mutate(&ev)
			assertEUChecks(t, euWorkshopSlots[4], ev, false)
		})
	}
}
