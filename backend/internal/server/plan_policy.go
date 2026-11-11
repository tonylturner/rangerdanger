package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

type evaluatedFirewall struct {
	DefaultAction string          `json:"defaultAction"`
	Rules         []labs.PlanRule `json:"rules"`
}

func readEvaluatedFirewall(data []byte) (evaluatedFirewall, error) {
	var document struct {
		Firewall evaluatedFirewall `json:"firewall"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return evaluatedFirewall{}, err
	}
	if document.Firewall.DefaultAction != "ALLOW" && document.Firewall.DefaultAction != "DENY" {
		return evaluatedFirewall{}, fmt.Errorf("missing or unsupported firewall default action")
	}
	// The finite evaluator cannot certify non-L4 selectors on a manual policy.
	var raw struct {
		Firewall struct {
			Rules []map[string]json.RawMessage `json:"rules"`
		} `json:"firewall"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return evaluatedFirewall{}, err
	}
	eligible := []labs.PlanRule{}
	for i, rule := range raw.Firewall.Rules {
		for _, key := range []string{"identities", "schedule", "viaForwards"} {
			value := string(rule[key])
			if value != "" && value != "null" && value != "[]" && value != "{}" && value != `""` {
				return evaluatedFirewall{}, fmt.Errorf("manual rule uses unsupported %s selector", key)
			}
		}
		switch document.Firewall.Rules[i].Action {
		case "ALLOW", "DENY", "RESET", "RATE_LIMIT", "TAG":
		default:
			return evaluatedFirewall{}, fmt.Errorf("manual rule uses unsupported action %q", document.Firewall.Rules[i].Action)
		}
		// These are new source → device connections. Established-only return
		// rules in named Weak policies cannot match them.
		var states []string
		if value := rule["state"]; len(value) > 0 {
			if err := json.Unmarshal(value, &states); err != nil {
				return evaluatedFirewall{}, err
			}
			if len(states) > 0 && !slices.Contains(states, "NEW") {
				continue
			}
		}
		eligible = append(eligible, document.Firewall.Rules[i])
	}
	document.Firewall.Rules = eligible
	return document.Firewall, nil
}

func commandRoute(gen *lifecycle.Generation, device, command, source string) (labs.PlanRoute, error) {
	if endpoint, err := netip.ParseAddrPort(source); err == nil {
		source = endpoint.Addr().String()
	}
	from := source
	switch source {
	case "operator", "web-ui", "rtac", "rtac-sim", "rtac-auto", "reset-script":
		from = "rtac-1"
	case "10.99.99.10":
		// The platform Compose twins pin the backend to this management
		// address; range templates/manifests and runtime config do not carry
		// platform service addresses. Source-less API calls use this peer,
		// but the RTAC performs the field write, just as for "operator".
		// Do not extend this attribution to other management peers.
		from = "rtac-1"
	}
	for _, node := range gen.Package.Template.Nodes {
		if node.IP == source || node.Container == source {
			from = node.ID
		}
	}
	to := ""
	for _, node := range gen.Package.Template.Nodes {
		if node.Type == device+"_sim" || node.ID == device+"-1" {
			to = node.ID
		}
	}
	port := 502
	if strings.HasPrefix(command, "crob_") || device == "recloser" {
		port = 20000
	}
	return labs.ResolvePlanTuple(gen.Package, labs.PlanProof{From: from, To: to, Network: "field_net", Port: port})
}

func (s *Server) commandVerdict(ctx context.Context, gen *lifecycle.Generation, device, command, source string) (labs.Decision, error) {
	route, err := commandRoute(gen, device, command, source)
	if err != nil {
		return labs.Decision{}, err
	}
	policy, stable, err := s.runningPolicy(ctx, gen)
	if err != nil {
		return labs.Decision{}, err
	}
	decision := labs.DecideRoute(policy.Rules, policy.DefaultAction, route)
	if err := stable(); err != nil {
		return labs.Decision{}, err
	}
	return decision, nil
}

// policyJudgment replaces label-only claims with evaluation of the running
// firewall over the same authored proof universe used by diagnostics.
func (s *Server) policyJudgment(ctx context.Context, gen *lifecycle.Generation) ValidationCheck {
	const name = "Running policy matches judging target"
	target, targetErr := s.judgingTarget(ctx, gen)
	fail := func(err error) ValidationCheck { return checkFail(name, target.detail+": "+err.Error()) }
	if targetErr != nil {
		return fail(targetErr)
	}
	data, err := gen.Containd().GetConfigDocument(ctx)
	if err != nil {
		return fail(err)
	}
	hash, err := containd.FirewallHashFromBytes(data)
	if err != nil {
		return fail(err)
	}
	s.activeConfigMu.RLock()
	source, want, boundary := s.policySource, s.lastAppliedHash, s.policyChangedAt
	provenance := s.planProvenance
	s.activeConfigMu.RUnlock()
	if want == "" || hash != want {
		return fail(fmt.Errorf("running policy differs from recorded identity; wait for the observer or reapply"))
	}
	if source == "plan" {
		if provenance == nil || provenance.PolicyHash != hash {
			return fail(fmt.Errorf("applied plan identity missing or divergent"))
		}
	} else {
		path, err := s.policyPath(gen.Package, "weak")
		if err != nil {
			return fail(err)
		}
		weak, err := readPolicyJSONWithRetry(path)
		if err != nil {
			return fail(err)
		}
		weakHash, err := containd.FirewallHashFromBytes(weak)
		if err != nil {
			return fail(err)
		}
		if hash == weakHash {
			return fail(fmt.Errorf("weak baseline remains active"))
		}
	}
	policy, err := readEvaluatedFirewall(data)
	if err != nil {
		return fail(err)
	}
	gaps := []string{}
	mismatches := []string{}
	mismatchCount := 0
	const mismatchLimit = 8
	for _, expectation := range target.plan.Expectations {
		route, err := labs.ResolvePlanTuple(gen.Package, labs.PlanProof{From: expectation.From, To: expectation.To, Network: expectation.Network, Port: expectation.Port})
		if err != nil {
			return fail(err)
		}
		decision := labs.DecideRoute(policy.Rules, policy.DefaultAction, route)
		verdict := decision.Verdict
		if verdict != expectation.PlanVerdict {
			mismatchCount++
			if len(mismatches) < mismatchLimit {
				owners := []string{}
				for _, owner := range expectation.Owners {
					for _, action := range gen.Package.Curriculum.Catalog {
						if action.ID == owner {
							owners = append(owners, action.Title)
						}
					}
				}
				ownership := "required conduit"
				if len(owners) > 0 {
					ownership = strings.Join(owners, ", ")
				}
				if verdict == labs.VerdictReset {
					ownership += "; " + resetDetail(decision)
				}
				mismatches = append(mismatches, fmt.Sprintf("%s -> %s:%d (%s): running %s; judging target %s (Hardened %s)", expectation.From, expectation.To, expectation.Port, ownership, verdict, expectation.PlanVerdict, expectation.HardenedVerdict))
			}
		}
		if verdict != expectation.HardenedVerdict {
			gaps = append(gaps, expectation.ID)
		}
	}
	if mismatchCount > 0 {
		detail := fmt.Sprintf("%d policy mismatches: %s", mismatchCount, strings.Join(mismatches, "; "))
		if mismatchCount > len(mismatches) {
			detail += fmt.Sprintf("; %d more not shown", mismatchCount-len(mismatches))
		}
		return fail(fmt.Errorf("%s. Review these paths in your committed firewall rules, then check again", detail))
	}
	finalHash, err := gen.Containd().GetFirewallHash(ctx)
	if err != nil || finalHash != hash {
		return fail(fmt.Errorf("%s", evidenceDetail("policy changed or became unavailable during evaluation; check again", err)))
	}
	s.activeConfigMu.RLock()
	stable := want == s.lastAppliedHash && boundary.Equal(s.policyChangedAt)
	s.activeConfigMu.RUnlock()
	if !stable {
		return fail(fmt.Errorf("policy boundary changed during evaluation"))
	}
	detail := target.detail
	if source == "plan" || len(gaps) > 0 {
		detail += "; Hardened prevention gaps: " + strings.Join(gaps, ", ")
	}
	return checkPass(name, "running L4 rules match "+detail+". The L4 source pin enforces; DPI detects and logs. TCP dataplane evidence requires separate wire observations")
}

// usAuditJudgment calls a command unauthorized only if its judging target
// does not admit the tuple. A deferred exposure is a visible consequence, not a proof
// failure. This is an advisory retained audit, not an absence certificate.
func (s *Server) usAuditJudgment(ctx context.Context, gen *lifecycle.Generation, entries []map[string]any) ValidationCheck {
	const name = "Audit: commands against judging target"
	target, err := s.judgingTarget(ctx, gen)
	fail := func(detail string) ValidationCheck { return checkFail(name, target.detail+": "+detail) }
	if err != nil {
		return fail(err.Error())
	}
	policy, err := readEvaluatedFirewall(target.plan.Config)
	if err != nil {
		return fail(err.Error())
	}
	denied, exposed := 0, 0
	for _, entry := range entries {
		command, _ := entry["command"].(string)
		result, _ := entry["result"].(string)
		if result != "executed" || command != "set_tap" && command != "crob_reclose" && command != "disable_reclose" {
			continue
		}
		source, _ := entry["source"].(string)
		device, _ := entry["target"].(string)
		route, err := commandRoute(gen, device, command, source)
		if err != nil {
			return fail("cannot classify retained command: " + err.Error())
		}
		decision := labs.DecideRoute(policy.Rules, policy.DefaultAction, route)
		verdict := decision.Verdict
		if verdict != labs.VerdictAllow {
			denied++
		} else {
			exposed++
		}
	}
	if denied > 0 {
		return ValidationCheck{Name: name, Status: "warn", Detail: target.detail + ": " + fmt.Sprintf("%d retained command(s) contradict the judging target; historical entries do not establish the current policy's effect", denied)}
	}
	return checkPass(name, target.detail+": "+fmt.Sprintf("no retained commands contradict the judging target; %d permitted commands observed (including deferred exposures). Hardened remains the comparison", exposed))
}
