package server

import (
	"fmt"
	"time"
)

// euRetainedReport survives page reloads, but not range/backend restart.
// Full hashes and commit boundaries prevent reuse after rollback or reapply.
type euRetainedReport struct {
	Hash, Markdown        string
	RangeStart, PolicyAt  time.Time
	StartedAt, FinishedAt time.Time
	Pass                  bool
}

func euWorkshopReport(ev euWorkshopEvidence) ValidationCheck {
	const name = "Retained PASS report matches running policy and window"
	r := ev.report
	if r == nil || !r.Pass || r.Markdown == "" || ev.policyErr != nil || ev.policyHash == "" ||
		r.Hash != ev.policyHash || !r.RangeStart.Equal(ev.start) || !r.PolicyAt.Equal(ev.policyAt) ||
		!insideEvidenceWindow(r.StartedAt, ev.policyAt, ev.now) ||
		!insideEvidenceWindow(r.FinishedAt, r.StartedAt, ev.now) {
		return checkFail(name, "generate and retain a PASS validation report for this running policy hash after its latest commit; missing, failed or older reports cannot complete Lab 2.4")
	}
	return checkPass(name, fmt.Sprintf("sha256:%s; retained probe window %s — %s", r.Hash, r.StartedAt.Format(time.RFC3339Nano), r.FinishedAt.Format(time.RFC3339Nano)))
}
