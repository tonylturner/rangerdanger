package server

import (
	"fmt"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// retainedValidationReport survives page reloads, but not range/backend restart.
// Full hashes and commit boundaries prevent reuse after rollback or reapply.
type retainedValidationReport struct {
	Hash, Markdown        string
	RangeStart, PolicyAt  time.Time
	StartedAt, FinishedAt time.Time
	PlanProof             proofResult
	Acceptance            acceptanceResult
}

type reportJudgment struct {
	report                    *retainedValidationReport
	hash                      string
	isPlan                    bool
	rangeStart, policyAt, now time.Time
	policyErr                 error
}

func checkRetainedReport(in reportJudgment) ValidationCheck {
	const name = "Retained PASS report matches running policy and window"
	r := in.report
	passed := r != nil && r.Acceptance.Pass
	if r != nil && in.isPlan {
		passed = r.PlanProof.Applicable && r.PlanProof.Pass && r.PlanProof.HashStable && r.PlanProof.PolicyHash == in.hash
	}
	if r == nil || !passed || r.Markdown == "" || in.policyErr != nil || in.hash == "" ||
		r.Hash != in.hash || !r.RangeStart.Equal(in.rangeStart) || !r.PolicyAt.Equal(in.policyAt) ||
		!insideEvidenceWindow(r.StartedAt, in.policyAt, in.now) || !insideEvidenceWindow(r.FinishedAt, r.StartedAt, in.now) {
		return checkFail(name, "generate and retain a PASS validation report for this running policy hash after its latest commit; missing, failed or older reports cannot complete Lab 2.4")
	}
	target := "Hardened acceptance"
	if in.isPlan {
		target = fmt.Sprintf("applied plan proof; Hardened acceptance PASS: %t; prevention gaps: %v", r.Acceptance.Pass, r.Acceptance.PreventionGaps)
	}
	return checkPass(name, fmt.Sprintf("%s; sha256:%s; retained probe window %s — %s", target, r.Hash, r.StartedAt.Format(time.RFC3339Nano), r.FinishedAt.Format(time.RFC3339Nano)))
}

func euWorkshopReport(ev euWorkshopEvidence) ValidationCheck {
	return checkRetainedReport(reportJudgment{report: ev.report, hash: ev.policyHash, isPlan: ev.isPlan, rangeStart: ev.start, policyAt: ev.policyAt, now: ev.now, policyErr: ev.policyErr})
}

func (s *Server) usRetainedReport(gen *lifecycle.Generation) ValidationCheck {
	s.activeConfigMu.RLock()
	in := reportJudgment{report: s.lastValidationReport, hash: s.lastAppliedHash, policyAt: s.policyChangedAt, rangeStart: gen.StartedAt, now: time.Now().UTC(), isPlan: s.policySource == "plan"}
	s.activeConfigMu.RUnlock()
	return checkRetainedReport(in)
}
