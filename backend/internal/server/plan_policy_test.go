package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

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
