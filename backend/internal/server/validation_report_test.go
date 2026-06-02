package server

import "testing"

// row is a small helper to build a validationRow for tally tests.
func row(category, actual string, pass bool) validationRow {
	return validationRow{
		validationProbe: validationProbe{Category: category},
		Actual:          actual,
		Pass:            pass,
	}
}

func TestTallyValidation(t *testing.T) {
	cases := []struct {
		name           string
		rows           []validationRow
		wantAuthPass   int
		wantAuthTotal  int
		wantUnauthPass int
		wantUnauthTot  int
		wantSkipped    int
		wantPass       bool
	}{
		{
			name: "all probes pass",
			rows: []validationRow{
				row("authorized", "allow", true),
				row("unauthorized", "deny", true),
			},
			wantAuthPass: 1, wantAuthTotal: 1,
			wantUnauthPass: 1, wantUnauthTot: 1,
			wantSkipped: 0, wantPass: true,
		},
		{
			name: "a genuine policy mismatch fails",
			rows: []validationRow{
				row("authorized", "deny", false), // authorized flow blocked = too tight
				row("unauthorized", "deny", true),
			},
			wantAuthPass: 0, wantAuthTotal: 1,
			wantUnauthPass: 1, wantUnauthTot: 1,
			wantSkipped: 0, wantPass: false,
		},
		{
			// The regression guard: a skipped probe must force FAIL even though
			// every probe that actually ran matched. Previously skipped rows
			// were dropped, letting a partially-running lab report PASS.
			name: "a skipped probe forces FAIL despite all run probes passing",
			rows: []validationRow{
				row("authorized", "allow", true),
				row("unauthorized", "deny", true),
				row("unauthorized", "skipped", false), // source container missing
			},
			wantAuthPass: 1, wantAuthTotal: 1,
			wantUnauthPass: 1, wantUnauthTot: 1,
			wantSkipped: 1, wantPass: false,
		},
		{
			// Skipped rows are excluded from the pass ratios (not counted as
			// failures in the numerator/denominator), only the verdict flips.
			name: "skipped rows excluded from pass ratios",
			rows: []validationRow{
				row("authorized", "skipped", false),
				row("authorized", "allow", true),
			},
			wantAuthPass: 1, wantAuthTotal: 1,
			wantUnauthPass: 0, wantUnauthTot: 0,
			wantSkipped: 1, wantPass: false,
		},
		{
			// An empty / fully-skipped matrix is never a PASS.
			name: "no probes ran is FAIL",
			rows: []validationRow{
				row("authorized", "skipped", false),
				row("unauthorized", "skipped", false),
			},
			wantAuthPass: 0, wantAuthTotal: 0,
			wantUnauthPass: 0, wantUnauthTot: 0,
			wantSkipped: 2, wantPass: false,
		},
		{
			name:        "empty matrix is FAIL",
			rows:        nil,
			wantPass:    false,
			wantSkipped: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tallyValidation(tc.rows)
			if got.authPass != tc.wantAuthPass || got.authTotal != tc.wantAuthTotal {
				t.Errorf("authorized: got %d/%d want %d/%d", got.authPass, got.authTotal, tc.wantAuthPass, tc.wantAuthTotal)
			}
			if got.unauthPass != tc.wantUnauthPass || got.unauthTotal != tc.wantUnauthTot {
				t.Errorf("unauthorized: got %d/%d want %d/%d", got.unauthPass, got.unauthTotal, tc.wantUnauthPass, tc.wantUnauthTot)
			}
			if got.skipped != tc.wantSkipped {
				t.Errorf("skipped: got %d want %d", got.skipped, tc.wantSkipped)
			}
			if got.pass != tc.wantPass {
				t.Errorf("pass: got %v want %v", got.pass, tc.wantPass)
			}
		})
	}
}
