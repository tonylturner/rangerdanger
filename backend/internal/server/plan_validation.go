package server

// proofResult and acceptanceResult are deliberately independent: a faithful
// partial plan can pass its proof while leaving Hardened prevention gaps.
type proofRow struct {
	ID       string `json:"id"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Pass     bool   `json:"pass"`
}
type proofResult struct {
	Applicable bool       `json:"applicable"`
	Pass       bool       `json:"pass"`
	PolicyHash string     `json:"policyHash"`
	HashStable bool       `json:"hashStable"`
	Skipped    int        `json:"skipped"`
	Rows       []proofRow `json:"rows"`
}
type acceptanceResult struct {
	Pass           bool       `json:"pass"`
	Skipped        int        `json:"skipped"`
	Rows           []proofRow `json:"rows"`
	PreventionGaps []string   `json:"preventionGaps"`
}

func judgeValidation(rows []validationRow, applicable bool, hash string, stable, capture bool) (proofResult, acceptanceResult) {
	proof := proofResult{Applicable: applicable, PolicyHash: hash, HashStable: stable, Pass: applicable && stable && len(rows) > 0, Rows: []proofRow{}}
	acceptance := acceptanceResult{Pass: stable && capture && len(rows) > 0, Rows: []proofRow{}, PreventionGaps: []string{}}
	for _, row := range rows {
		measured := row.Actual != "skipped" && row.ProbeError == ""
		if row.Actual == "skipped" {
			acceptance.Skipped++
			if applicable {
				proof.Skipped++
			}
		}
		accepted := measured && row.Actual == row.HardenedExpected
		acceptance.Rows = append(acceptance.Rows, proofRow{row.ID, row.HardenedExpected, row.Actual, accepted})
		acceptance.Pass = acceptance.Pass && accepted
		if row.HardenedExpected == "deny" && row.Actual == "allow" {
			acceptance.PreventionGaps = append(acceptance.PreventionGaps, row.ID)
		}
		if applicable {
			passed := measured && row.Actual == row.PlanExpected
			proof.Rows = append(proof.Rows, proofRow{row.ID, row.PlanExpected, row.Actual, passed})
			proof.Pass = proof.Pass && passed
		}
	}
	return proof, acceptance
}
