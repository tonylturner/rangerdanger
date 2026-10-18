package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

var euLab3TestStart = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func euLab3TestAt(seconds int) time.Time {
	return euLab3TestStart.Add(time.Duration(seconds) * time.Second)
}

func euLab3TestJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func euLab3TestEntry(second int, operation, result, peer string) euLab3AuditEntry {
	entry := euLab3AuditEntry{Seq: uint64(second), At: euLab3TestAt(second), Operation: operation, Result: result}
	entry.Context.Protocol, entry.Context.Peer = "modbus", peer
	entry.Before.Generation, entry.After.Generation = 1, 1
	return entry
}

func euLab3TestCommand(t *testing.T, ev *euLab3Evidence, second int, device, operation, result, reason string, ioa int, value any) {
	t.Helper()
	detail := map[string]any{"point": device + "-command", "operation": operation, "result": result,
		"selected_at": euLab3TestAt(second), "executed_at": euLab3TestAt(second + 1), "con_at": euLab3TestAt(second + 3)}
	if result == "completed" {
		detail["term_at"] = euLab3TestAt(second + 5)
		detail["feedback"] = map[string]any{"ioa": ioa, "at": euLab3TestAt(second + 4), "value": value}
	}
	ev.journal.Entries = append(ev.journal.Entries, euLab3JournalEntry{
		Seq: uint64(second), At: euLab3TestAt(second + 6), Kind: "command", Detail: euLab3TestJSON(t, detail)})
	entry := euLab3TestEntry(second+2, operation, "accepted", "10.30.30.20:43001")
	entry.Before.Position, entry.After.Position = "closed", "open"
	if operation == "close" {
		entry.Before.Position, entry.After.Position = "open", "closed"
	}
	if result != "completed" {
		entry.Result = "rejected"
		entry.After = entry.Before
		entry.Detail = euLab3TestJSON(t, reason)
	}
	ev.audits[device].Entries = append(ev.audits[device].Entries, entry)
}

func euLab3TestEvidence(t *testing.T) euLab3Evidence {
	t.Helper()
	ev := euLab3Evidence{start: euLab3TestStart, end: euLab3TestAt(200), epoch: "cc1",
		journal: &euLab3Journal{CacheEpoch: "cc1"}, audits: map[string]*euLab3Audit{
			"rmu-1": {Generation: 1}, "rmu-2": {Generation: 1}, "breaker-1": {Generation: 1}}}
	euLab3TestCommand(t, &ev, 10, "rmu-1", "open", "completed", "", 1101, "open")
	euLab3TestCommand(t, &ev, 20, "rmu-1", "close", "completed", "", 1101, "closed")
	euLab3TestCommand(t, &ev, 30, "rmu-1", "open", "rejected", "not_remote", 1101, nil)
	euLab3TestCommand(t, &ev, 40, "rmu-2", "close", "rejected", "live_live", 1201, nil)
	trip := euLab3TestEntry(50, "local_protection_trip", "accepted", "protection")
	trip.Context.Protocol = "local-protection"
	ev.audits["breaker-1"].Entries = []euLab3AuditEntry{trip}
	euLab3TestCommand(t, &ev, 60, "rmu-1", "open", "completed", "", 1101, "open")
	euLab3TestCommand(t, &ev, 70, "rmu-2", "close", "completed", "", 1201, "closed")
	vendor := euLab3TestEntry(80, "open", "accepted", "10.20.20.10:44000")
	vendor.Before.Position, vendor.After.Position = "closed", "open"
	ev.audits["rmu-2"].Entries = append(ev.audits["rmu-2"].Entries, vendor)
	ev.events = []containd.Event{{Timestamp: euLab3TestAt(90), Kind: "firewall.rule.hit",
		Source: "10.20.20.10", Dest: "10.40.40.22", DstPort: 502, Attributes: map[string]any{"action": "DENY"}}}
	return ev
}

func euLab3AssertPass(t *testing.T, checks []ValidationCheck) {
	t.Helper()
	for _, check := range checks {
		if check.Status != "pass" {
			t.Errorf("%s: %s", check.Name, check.Detail)
		}
	}
}

func TestEULab3ValidatorPass(t *testing.T) {
	ev := euLab3TestEvidence(t)
	euLab3AssertPass(t, evaluateEULab3(ev, true))
}

func TestEULab3RejectsUnprovenEvidence(t *testing.T) {
	tests := []struct {
		name   string
		change func(*euLab3Evidence)
		check  int
	}{
		{"empty", func(ev *euLab3Evidence) { ev.journal.Entries = nil }, 0},
		{"old attempt", func(ev *euLab3Evidence) { ev.start = euLab3TestAt(25) }, 0},
		{"future attempt", func(ev *euLab3Evidence) { ev.end = euLab3TestAt(15) }, 0},
		{"missing audit", func(ev *euLab3Evidence) { delete(ev.audits, "rmu-1") }, 0},
		{"state only", func(ev *euLab3Evidence) { ev.audits["rmu-1"].Entries = nil }, 0},
		{"wrong peer", func(ev *euLab3Evidence) {
			for i := range ev.audits["rmu-1"].Entries {
				ev.audits["rmu-1"].Entries[i].Context.Peer = "10.20.20.10:43001"
			}
		}, 0},
		{"audit outside command", func(ev *euLab3Evidence) { ev.audits["rmu-1"].Entries[1].At = euLab3TestAt(100) }, 0},
		{"unknown not rejected", func(ev *euLab3Evidence) {
			var cmd map[string]any
			_ = json.Unmarshal(ev.journal.Entries[2].Detail, &cmd)
			cmd["result"] = "unknown"
			ev.journal.Entries[2].Detail = euLab3TestJSON(t, cmd)
		}, 1},
		{"wrong refusal", func(ev *euLab3Evidence) { ev.audits["rmu-2"].Entries[0].Detail = euLab3TestJSON(t, "no_measurement") }, 2},
		{"no trip", func(ev *euLab3Evidence) { ev.audits["breaker-1"].Entries = nil }, 3},
		{"trip after transfer", func(ev *euLab3Evidence) { ev.audits["breaker-1"].Entries[0].At = euLab3TestAt(100) }, 3},
		{"wrong deny source", func(ev *euLab3Evidence) { ev.events[0].Source = "10.10.10.50" }, 4},
		{"deny before write", func(ev *euLab3Evidence) { ev.events[0].Timestamp = euLab3TestAt(79) }, 4},
		{"no deny", func(ev *euLab3Evidence) { ev.events = nil }, 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ev := euLab3TestEvidence(t)
			test.change(&ev)
			if checks := evaluateEULab3(ev, true); checks[test.check].Status != "fail" {
				t.Fatalf("unexpected pass: %+v", checks[test.check])
			}
		})
	}
	t.Run("wrong policy", func(t *testing.T) {
		if checks := evaluateEULab3(euLab3TestEvidence(t), false); checks[4].Status != "fail" {
			t.Fatal("wrong policy passed")
		}
	})
	t.Run("source unavailable", func(t *testing.T) {
		ev := euLab3TestEvidence(t)
		ev.err = errors.New("upstream unavailable")
		if checks := evaluateEULab3(ev, true); len(checks) != 1 || checks[0].Status != "fail" {
			t.Fatal(checks)
		}
	})
}

func TestEULab3CommandRequiresFreshFeedback(t *testing.T) {
	for _, field := range []string{"term_at", "con_at", "feedback", "executed_at", "selected_at", "wrong-ioa", "wrong-value", "stale-feedback"} {
		t.Run(field, func(t *testing.T) {
			ev := euLab3TestEvidence(t)
			// Remove the later open so it cannot mask missing evidence.
			ev.journal.Entries = ev.journal.Entries[:2]
			var cmd map[string]any
			_ = json.Unmarshal(ev.journal.Entries[0].Detail, &cmd)
			switch field {
			case "wrong-ioa":
				cmd["feedback"].(map[string]any)["ioa"] = 1201
			case "wrong-value":
				cmd["feedback"].(map[string]any)["value"] = "closed"
			case "stale-feedback":
				cmd["feedback"].(map[string]any)["at"] = euLab3TestAt(9)
			default:
				cmd[field] = nil
			}
			ev.journal.Entries[0].Detail = euLab3TestJSON(t, cmd)
			if ev.commandAudit("rmu-1", "rmu-1-command", "open", "completed", "", 1101) {
				t.Fatal("incomplete command passed")
			}
		})
	}
}

func TestEULab3CollectorFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name, state, audit string
		code               int
	}{
		{"epoch changed", `{"cache_epoch":"other"}`, `{"generation":1,"entries":[]}`, http.StatusOK},
		{"device restarted", `{"cache_epoch":"cc1"}`, `{"generation":2,"entries":[]}`, http.StatusOK},
		{"audit unavailable", `{"cache_epoch":"cc1"}`, `{}`, http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, cc := newUpstreamStub(t, map[string]stubResponse{
				"GET /api/state":           {http.StatusOK, test.state},
				"GET /api/journal?after=0": {http.StatusOK, `{"cache_epoch":"cc1","entries":[]}`}})
			_, device := newUpstreamStub(t, map[string]stubResponse{
				"GET /api/state":         {http.StatusOK, `{"generation":1}`},
				"GET /api/audit?after=0": {test.code, test.audit}})
			gen := euGeneration(t, nil, map[string]string{"control_centre": cc.URL, "rmu_1": device.URL})
			ev := collectEULab3(t.Context(), gen, "rmu-1")
			if ev.err == nil {
				t.Fatal("missing source/changed identity accepted")
			}
		})
	}
	if ev := collectEULab3(t.Context(), nil); ev.err == nil {
		t.Fatal("nil generation accepted")
	}
}

func TestEULab3And4Knowledge(t *testing.T) {
	// Operational deep dives moved out of the seven-slot lesson sequence.
	for file, concepts := range map[string][]string{
		"eu-switching-deep-dive.md":  {"not_remote", "live_live", "fault passage", "isolation", "back-feed", "300 A", "2000 A", "1000 ms", "sample-quantised", "normal open point"},
		"eu-regulation-deep-dive.md": {"20.196–20.604", "30 s", "2 s", "+8", "−6", "110 kV", "866 A", "1307", "2003–2005", "2303–2305", "daily", "zero", "manual", "auto"},
	} {
		data, err := os.ReadFile(filepath.Join(repoRoot(), "lab-definitions", "packages", euPackageID, "knowledge", file))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(data), "#### ") != 8 {
			t.Errorf("%s lost its eight investigation questions", file)
		}
		for _, concept := range concepts {
			if !strings.Contains(string(data), concept) {
				t.Errorf("%s lost %s", file, concept)
			}
		}
	}
}

// All endpoints are loopback-only; the handler rejects writes so collection
// tests cannot quietly perform lab actions.
func euLab3TestRange(t *testing.T, ev euLab3Evidence, history, clock *euLab4History) (*Server, *lifecycle.Generation) {
	t.Helper()
	policy, err := os.ReadFile(filepath.Join(manifest.Dir(repoRoot(), euPackageID), "firewall", "improved.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("validator tried %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/config":
			_, _ = w.Write(policy)
		case "/api/v1/events":
			_ = json.NewEncoder(w).Encode(ev.events)
		case "/cc/api/state":
			_ = json.NewEncoder(w).Encode(map[string]any{"cache_epoch": ev.epoch})
		case "/cc/api/journal":
			_ = json.NewEncoder(w).Encode(ev.journal)
		case "/historian/api/history":
			query := r.URL.Query()
			since, err1 := time.Parse(time.RFC3339Nano, query.Get("since"))
			until, err2 := time.Parse(time.RFC3339Nano, query.Get("until"))
			if err1 != nil || err2 != nil || since.After(until) {
				t.Errorf("bad history window %s", r.URL.RawQuery)
			}
			switch query.Get("point") {
			case "oltc-1-bus-kv":
				_ = json.NewEncoder(w).Encode(history)
			case "oltc-1-auto":
				_ = json.NewEncoder(w).Encode(clock)
			default:
				t.Errorf("unexpected point %s", query.Get("point"))
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) != 3 || parts[1] != "api" {
				t.Errorf("unexpected path %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			audit, ok := ev.audits[parts[0]]
			if !ok {
				t.Errorf("unexpected device %s", parts[0])
				w.WriteHeader(http.StatusNotFound)
				return
			}
			switch parts[2] {
			case "state":
				_ = json.NewEncoder(w).Encode(map[string]any{"generation": audit.Generation})
			case "audit":
				_ = json.NewEncoder(w).Encode(audit)
			default:
				t.Errorf("unexpected device route %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}
	})
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	gen := euGeneration(t, containd.NewClient(upstream.URL), map[string]string{
		"control_centre": upstream.URL + "/cc", "rmu_1": upstream.URL + "/rmu-1", "rmu_2": upstream.URL + "/rmu-2",
		"feeder_breaker": upstream.URL + "/breaker-1", "oltc_1": upstream.URL + "/oltc-1", "historian_eu": upstream.URL + "/historian"})
	return rangeServer(gen, "improved"), gen
}

func TestEULab3EvidenceCollectorReadsManifest(t *testing.T) {
	ev := euLab3TestEvidence(t)
	s, gen := euLab3TestRange(t, ev, nil, nil)
	euLab3AssertPass(t, evaluateEULab3(collectEULab3(t.Context(), gen, "rmu-1", "rmu-2", "breaker-1"), s.runningPolicyIs(t.Context(), gen, "improved") == nil))
}

func TestEULab3ResetStartsNewAttempt(t *testing.T) {
	ev := euLab3TestEvidence(t)
	reset := euLab3TestEntry(100, "reset", "accepted", "lab")
	reset.Context.Protocol = "lab"
	ev.audits["rmu-1"].Entries = append(ev.audits["rmu-1"].Entries, reset)
	_, gen := euLab3TestRange(t, ev, nil, nil)
	collected := collectEULab3(t.Context(), gen, "rmu-1", "rmu-2", "breaker-1")
	if collected.err != nil {
		t.Fatal(collected.err)
	}
	if !collected.start.Equal(reset.At) {
		t.Fatalf("start %s, want last reset %s", collected.start, reset.At)
	}
	for _, check := range evaluateEULab3(collected, true) {
		if check.Status != "fail" {
			t.Errorf("pre-reset evidence passed: %+v", check)
		}
	}
}
