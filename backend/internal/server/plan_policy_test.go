package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestCommandRouteSources(t *testing.T) {
	s := newTestServer(t, "http://127.0.0.1:1")
	gen := s.rng.(*fakeRange).gen
	for _, tc := range []struct {
		source, from string
	}{
		{"10.99.99.10:57108", "rtac-1"},
		{"10.99.99.10", "rtac-1"},
		{"operator", "rtac-1"},
		{"web-ui", "rtac-1"},
		{"reset-script", "rtac-1"},
		{"rtac", "rtac-1"},
		{"rtac-sim", "rtac-1"},
		{"rtac-auto", "rtac-1"},
		{"10.30.30.20:57108", "rtac-1"},
		{"10.10.10.50:57108", "kali-1"},
		{"10.10.10.50", "kali-1"},
		{"rangerdanger-kali", "kali-1"},
		{"kali-1", "kali-1"},
		{"10.99.99.11:57108", ""},
		{"10.99.99.3:57108", ""},
		{"10.99.99.100:57108", ""},
		{"10.10.10.99:57108", ""},
		{"10.99.99.10:not-a-port", ""},
		{"10.99.99.10:65536", ""},
		{"operator:57108", ""},
		{"web-ui-other", ""},
		{"unknown", ""},
		{"", ""},
	} {
		t.Run(tc.source, func(t *testing.T) {
			for _, command := range []struct {
				device, command string
				port            int
			}{{"regulator", "set_tap", 502}, {"recloser", "disable_reclose", 20000}} {
				got, err := commandRoute(gen, command.device, command.command, tc.source)
				if tc.from == "" {
					if err == nil {
						t.Fatalf("unknown source accepted: %+v", got)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				want, err := labs.ResolvePlanTuple(gen.Package, labs.PlanProof{
					From: tc.from, To: command.device + "-1", Network: "field_net", Port: command.port,
				})
				if err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("route = %+v, want %+v", got, want)
				}
			}
		})
	}
}

func TestRetainedCommandSources(t *testing.T) {
	// Retained entries from guided-proof.json, including the backend's
	// source-less RTAC API call and the later authored Restore step.
	const backendEntry = `{"timestamp":"2026-11-02T06:03:59.303740964Z","source":"10.99.99.10:57108","source_zone":"unknown","target":"regulator","command":"set_tap","result":"executed","detail":"tap set directly","process_impact":"voltage regulator tap changed — critical load voltage will shift [from unknown zone]"}`
	const restoreEntry = `{"timestamp":"2026-11-02T06:12:20.276101376Z","source":"operator","source_zone":"unknown","target":"regulator","command":"set_tap","result":"executed","detail":"tap set directly","process_impact":"voltage regulator tap changed — critical load voltage will shift [from unknown zone]"}`
	s := newTestServer(t, "http://127.0.0.1:1")
	gen := s.rng.(*fakeRange).gen
	for _, target := range []string{"manual-custom", "plan-all", "plan-deferred"} {
		t.Run(target, func(t *testing.T) {
			ids := []string{}
			if target != "plan-deferred" {
				for _, action := range gen.Package.Curriculum.Catalog {
					ids = append(ids, action.ID)
				}
			}
			compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, ids)
			if err != nil {
				t.Fatal(err)
			}
			s.appliedPlan, s.policySource = &compiled, "plan"
			if target == "manual-custom" {
				s.policySource = target
			}
			for _, tc := range []struct {
				name, entry, source, want string
			}{
				{"backend retained", backendEntry, "", "pass"},
				{"restore retained", restoreEntry, "", "pass"},
				{"HMI supervisory", backendEntry, "web-ui", "pass"},
				{"Reset Lab", backendEntry, "reset-script", "pass"},
				{"unknown management peer", backendEntry, "10.99.99.11:57108", "fail"},
				{"unknown teaching peer", backendEntry, "10.10.10.99:57108", "fail"},
				{"unknown label", backendEntry, "unknown", "fail"},
				{"Kali endpoint", backendEntry, "10.10.10.50:57108", "warn"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var entry map[string]any
					if err := json.Unmarshal([]byte(tc.entry), &entry); err != nil {
						t.Fatal(err)
					}
					if tc.source != "" {
						entry["source"] = tc.source
					}
					want := tc.want
					if want == "warn" && target == "plan-deferred" {
						want = "pass"
					}
					check := s.usAuditJudgment(gen, []map[string]any{entry})
					if check.Status != want {
						t.Fatalf("got %+v, want %s", check, want)
					}
					if want == "pass" && !strings.Contains(check.Detail, "1 permitted commands observed") {
						t.Fatalf("entry was not evaluated: %+v", check)
					}
					if want == "fail" && !strings.Contains(check.Detail, "cannot classify retained command") {
						t.Fatalf("unknown source must fail classification: %+v", check)
					}
				})
			}
		})
	}
}

func TestManualPolicyFirstMatch(t *testing.T) {
	for _, tc := range []struct {
		name, rules, fallback string
		want                  labs.Verdict
	}{
		{"deny first", `{"action":"DENY"},{"action":"ALLOW"}`, "ALLOW", labs.VerdictDeny},
		{"allow first", `{"action":"ALLOW"},{"action":"DENY"}`, "DENY", labs.VerdictAllow},
		{"effects continue", `{"action":"RATE_LIMIT"},{"action":"TAG"},{"action":"ALLOW"}`, "DENY", labs.VerdictAllow},
		{"effects fall through", `{"action":"TAG"},{"action":"RATE_LIMIT"}`, "ALLOW", labs.VerdictAllow},
		{"effects do not grant", `{"action":"RATE_LIMIT"},{"action":"TAG"}`, "DENY", labs.VerdictDeny},
		{"monitor continues", `{"action":"DENY","ics":{"protocol":"iec104","mode":"monitor"}},{"action":"ALLOW"}`, "DENY", labs.VerdictAllow},
		{"reset terminates", `{"action":"RESET"},{"action":"ALLOW"}`, "ALLOW", labs.VerdictDeny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := fmt.Sprintf(`{"firewall":{"defaultAction":%q,"rules":[%s]}}`, tc.fallback, tc.rules)
			policy, err := readEvaluatedFirewall([]byte(data))
			if err != nil {
				t.Fatal(err)
			}
			if got := labs.Evaluate(policy.Rules, policy.DefaultAction, labs.PlanTuple{}); got != tc.want {
				t.Fatalf("verdict %s, want %s", got, tc.want)
			}
		})
	}
}

func TestManualPolicyUnsupportedSelectors(t *testing.T) {
	for _, key := range []string{"viaForwards", "identities", "schedule"} {
		for _, rule := range []string{
			fmt.Sprintf(`{"action":"ALLOW",%q:["published-service"]}`, key),
			fmt.Sprintf(`{"action":"ALLOW","state":["ESTABLISHED"],%q:["published-service"]}`, key),
			fmt.Sprintf(`{"action":"TAG",%q:["published-service"]}`, key),
		} {
			data := `{"firewall":{"defaultAction":"DENY","rules":[` + rule + `]}}`
			if _, err := readEvaluatedFirewall([]byte(data)); err == nil || !strings.Contains(err.Error(), "unsupported "+key+" selector") {
				t.Fatalf("%s: expected clear selector refusal, got %v", rule, err)
			}
		}
	}
	data := `{"firewall":{"defaultAction":"DENY","rules":[{"action":"UNKNOWN"}]}}`
	if _, err := readEvaluatedFirewall([]byte(data)); err == nil || !strings.Contains(err.Error(), "unsupported action") {
		t.Fatalf("unknown action accepted: %v", err)
	}
}
