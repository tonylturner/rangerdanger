package server

import (
	"context"
	"fmt"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// readRunningFirewall evaluates exactly the document whose hash was checked,
// never a second read or the judging plan. Callers check identity again after use.
func readRunningFirewall(ctx context.Context, gen *lifecycle.Generation, want string) (evaluatedFirewall, string, error) {
	data, err := gen.Containd().GetConfigDocument(ctx)
	if err != nil {
		return evaluatedFirewall{}, "", err
	}
	hash, err := containd.FirewallHashFromBytes(data)
	if err != nil {
		return evaluatedFirewall{}, "", err
	}
	if want == "" || hash != want {
		return evaluatedFirewall{}, hash, fmt.Errorf("running policy differs from recorded identity; wait for the observer or reapply")
	}
	policy, err := readEvaluatedFirewall(data)
	return policy, hash, err
}

func resetDetail(decision labs.Decision) string {
	return fmt.Sprintf("blocked by RESET (%s): the timeout test cannot prove it; use DENY", decision.RuleID)
}

func reconcileTCP(observation string, decision labs.Decision) (string, string) {
	if decision.Verdict == labs.VerdictReset {
		return "reset", resetDetail(decision)
	}
	switch observation {
	case "connected", "refused":
		return "allow", ""
	case "timeout":
		return "deny", ""
	default:
		return "skipped", ""
	}
}

func attributeProbes(matrix []validationProbe, policy evaluatedFirewall) {
	for i := range matrix {
		matrix[i].Decision = labs.DecideRoute(policy.Rules, policy.DefaultAction, matrix[i].Route)
	}
}

// runningPolicy brackets observations/admission with the recorded commit boundary.
// Even a same-hash recommit invalidates evidence collected across that boundary.
func (s *Server) runningPolicy(ctx context.Context, gen *lifecycle.Generation) (evaluatedFirewall, func() error, error) {
	s.activeConfigMu.RLock()
	want, at, source := s.lastAppliedHash, s.policyChangedAt, s.policySource
	provenance, plan := s.planProvenance, s.appliedPlan
	s.activeConfigMu.RUnlock()
	policy, hash, err := readRunningFirewall(ctx, gen, want)
	if err != nil {
		return policy, nil, err
	}
	if source == "plan" && (provenance == nil || plan == nil || provenance.PolicyHash != hash || provenance.PackageID != gen.Package.ID || provenance.Revision != gen.Package.Revision) {
		return policy, nil, fmt.Errorf("applied plan identity missing or divergent")
	}
	stable := func() error {
		final, err := gen.Containd().GetFirewallHash(ctx)
		if err != nil || final != hash {
			return fmt.Errorf("%s", evidenceDetail("policy changed or became unavailable during observation; check again", err))
		}
		s.activeConfigMu.RLock()
		same := s.lastAppliedHash == want && s.policyChangedAt.Equal(at) && s.policySource == source && s.planProvenance == provenance && s.appliedPlan == plan
		s.activeConfigMu.RUnlock()
		if !same || ctx.Err() != nil || gen.Context().Err() != nil {
			return fmt.Errorf("policy boundary changed during observation")
		}
		return nil
	}
	return policy, stable, nil
}
