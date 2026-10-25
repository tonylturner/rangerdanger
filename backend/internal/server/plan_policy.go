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
		for _, key := range []string{"identities", "schedule"} {
			value := string(rule[key])
			if value != "" && value != "null" && value != "[]" && value != "{}" && value != `""` {
				return evaluatedFirewall{}, fmt.Errorf("manual rule uses unsupported %s selector", key)
			}
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
	from := source
	switch source {
	case "operator", "rtac", "rtac-sim", "rtac-auto", "reset-script":
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

func (s *Server) commandVerdict(ctx context.Context, gen *lifecycle.Generation, device, command, source string) (labs.Verdict, error) {
	route, err := commandRoute(gen, device, command, source)
	if err != nil {
		return "", err
	}
	data, err := gen.Containd().GetConfigDocument(ctx)
	if err != nil {
		return "", err
	}
	policy, err := readEvaluatedFirewall(data)
	if err != nil {
		return "", err
	}
	if route.Bridged {
		return labs.VerdictAllow, nil
	}
	return labs.Evaluate(policy.Rules, policy.DefaultAction, route.Tuple), nil
}

// usPolicyJudgment replaces label-only claims with evaluation of the running
// firewall over the same authored proof universe used by the report.
func (s *Server) usPolicyJudgment(ctx context.Context, gen *lifecycle.Generation) ValidationCheck {
	const name = "Running policy matches judging target"
	fail := func(err error) ValidationCheck { return checkFail(name, err.Error()) }
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
	if source != "plan" {
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
	} else if provenance == nil || provenance.PolicyHash != hash {
		return fail(fmt.Errorf("applied plan identity missing or divergent"))
	}
	policy, err := readEvaluatedFirewall(data)
	if err != nil {
		return fail(err)
	}
	expectations, err := s.judgingExpectations(gen)
	if err != nil {
		return fail(err)
	}
	gaps := []string{}
	for _, expectation := range expectations {
		route, err := labs.ResolvePlanTuple(gen.Package, labs.PlanProof{From: expectation.From, To: expectation.To, Network: expectation.Network, Port: expectation.Port})
		if err != nil {
			return fail(err)
		}
		verdict := labs.VerdictAllow
		if !route.Bridged {
			verdict = labs.Evaluate(policy.Rules, policy.DefaultAction, route.Tuple)
		}
		if verdict != expectation.PlanVerdict {
			return fail(fmt.Errorf("tuple %s: running %s; judging target %s (Hardened %s)", expectation.ID, verdict, expectation.PlanVerdict, expectation.HardenedVerdict))
		}
		if verdict != expectation.HardenedVerdict {
			gaps = append(gaps, expectation.ID)
		}
	}
	finalHash, err := gen.Containd().GetFirewallHash(ctx)
	if err != nil || finalHash != hash {
		return fail(fmt.Errorf("policy changed or became unavailable during evaluation: %v", err))
	}
	s.activeConfigMu.RLock()
	stable := want == s.lastAppliedHash && boundary.Equal(s.policyChangedAt)
	s.activeConfigMu.RUnlock()
	if !stable {
		return fail(fmt.Errorf("policy boundary changed during evaluation"))
	}
	target := "Hardened (not an applied plan)"
	if source == "plan" {
		target = "applied plan; Hardened prevention gaps: " + strings.Join(gaps, ", ")
	}
	return checkPass(name, "running L4 rules match "+target+". The L4 source pin enforces; DPI detects and logs. TCP dataplane evidence requires the validation report")
}

func planDenyFilter(rows []conduitDeny, plan *labs.CompiledPlan, pkg labs.Package) ([]conduitDeny, error) {
	if plan == nil {
		return rows, nil
	}
	policy, err := readEvaluatedFirewall(plan.Config)
	if err != nil {
		return nil, err
	}
	filtered := []conduitDeny{}
	zone := func(ip string) (string, error) {
		address, err := netip.ParseAddr(ip)
		if err != nil {
			return "", err
		}
		for _, network := range pkg.Template.Networks {
			subnet := network.Subnet
			if subnet == "" {
				subnet = network.CIDR
			}
			prefix, err := netip.ParsePrefix(subnet)
			if err == nil && prefix.Contains(address) {
				return network.Zone, nil
			}
		}
		return "", fmt.Errorf("no teaching zone for %s", ip)
	}
	for _, row := range rows {
		src, err := zone(row.source)
		if err != nil {
			return nil, err
		}
		dst, err := zone(row.dest)
		if err != nil {
			return nil, err
		}
		if labs.Evaluate(policy.Rules, policy.DefaultAction, labs.PlanTuple{SrcZone: src, DstZone: dst, SrcIP: row.source, DstIP: row.dest, Protocol: "tcp", Port: row.port}) == labs.VerdictDeny {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

// usAuditJudgment calls a command unauthorized only if its judging target
// denies the tuple. A deferred exposure is a visible consequence, not a proof
// failure. This is an advisory retained audit, not an absence certificate.
func (s *Server) usAuditJudgment(gen *lifecycle.Generation, entries []map[string]any) ValidationCheck {
	const name = "Audit: commands against judging target"
	s.activeConfigMu.RLock()
	plan := s.appliedPlan
	isPlan := s.policySource == "plan" && plan != nil
	s.activeConfigMu.RUnlock()
	if !isPlan {
		ids := []string{}
		for _, action := range gen.Package.Curriculum.Catalog {
			ids = append(ids, action.ID)
		}
		compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, ids)
		if err != nil {
			return checkFail(name, err.Error())
		}
		plan = &compiled
	}
	policy, err := readEvaluatedFirewall(plan.Config)
	if err != nil {
		return checkFail(name, err.Error())
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
			return checkFail(name, "cannot classify retained command: "+err.Error())
		}
		verdict := labs.VerdictAllow
		if !route.Bridged {
			verdict = labs.Evaluate(policy.Rules, policy.DefaultAction, route.Tuple)
		}
		if verdict == labs.VerdictDeny {
			denied++
		} else {
			exposed++
		}
	}
	if denied > 0 {
		return ValidationCheck{Name: name, Status: "warn", Detail: fmt.Sprintf("%d retained command(s) contradict the judging target; historical entries do not establish the current policy's effect", denied)}
	}
	return checkPass(name, fmt.Sprintf("no retained commands contradict the judging target; %d permitted commands observed (including deferred exposures). Hardened remains the comparison", exposed))
}
