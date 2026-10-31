package server

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

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

// Golden hashes cover the strict TCP report wording; only its clock is normalized.
func TestUSValidationReportGolden(t *testing.T) {
	rows := []validationRow{
		{validationProbe: validationProbe{SrcLabel: "source", Dst: "10.0.0.1", Port: "502", Expected: "allow", Note: "positive", Category: "authorized"}, Actual: "allow", Pass: true, DurationMs: 1},
		{validationProbe: validationProbe{SrcLabel: "attacker", Dst: "10.0.0.2", Port: "2404", Expected: "deny", Note: "negative", Category: "unauthorized"}, Actual: "deny", Pass: true, DurationMs: 3000},
	}
	for _, tc := range []struct {
		active  string
		pcap    bool
		skipped int
		pass    bool
		hash    string
	}{
		{"custom", true, 0, true, "8383523c3329c1b0e2e299a28f29e5f85e6f22d3fda0d4196691c54bc0309dbd"},
		{"improved", false, 0, true, "e2757058350b0f09aa632685b0eedf09209bc1fba2b9b9dc17d79c940d045464"},
		{"weak", true, 0, false, "81c0c2ee23c8613ed8d4a64877c3077c810f3b6d8c1118d90b7bb1e25b5f8902"},
		{"other", false, 1, false, "0ac1b5846eb6bb1be090431682f99673ba935d6eea3c6cfa2342e28f0da4cff2"},
	} {
		t.Run(tc.active, func(t *testing.T) {
			md := usRecipe.renderValidationMarkdown(tc.active, "source", "abc", "data/firewall/captures/validation-fixed.pcap", tc.pcap, "capture-sources", rows, 1, 1, 1, 1, tc.skipped, tc.pass)
			md = regexp.MustCompile(`(?m)^\| \*\*Generated\*\* \| .* \|$`).ReplaceAllString(md, "| **Generated** | GENERATED |")
			if got := fmt.Sprintf("%x", sha256.Sum256([]byte(md))); got != tc.hash {
				t.Fatalf("US report changed (%s):\n%s", got, md)
			}
		})
	}
}

func TestEUReportNarrowClaimsAndToolErrors(t *testing.T) {
	for _, pcap := range []bool{true, false} {
		md := euRecipe.renderValidationMarkdown("custom", "plan", "abc", "path", pcap, "sources", nil, 9, 9, 16, 16, 0, true)
		for _, required := range []string{"TCP Reachability", "not certification", "PCAP completeness", "No DPI", "Capture availability and stable hash are required by acceptance"} {
			if !strings.Contains(md, required) {
				t.Fatalf("missing scope limit %q: %s", required, md)
			}
		}
		if pcap && !strings.Contains(md, "pinned PLC-to-OLTC conduit crosses the firewall from station to field") {
			t.Fatal(md)
		}
		for _, forbidden := range []string{"RTAC", "GPS/NTP", "every authorized flow works", "same-subnet"} {
			if strings.Contains(md, forbidden) {
				t.Fatalf("US/overbroad claim %q", forbidden)
			}
		}
	}
	md := euRecipe.renderValidationMarkdown("custom", "source", "hash", "path", false, "", nil, 0, 0, 0, 0, 1, false)
	if !strings.Contains(md, "probe tool") || strings.Contains(md, "Bring the full lab stack up") {
		t.Fatal(md)
	}

}

func TestEUReportProbeErrorDetails(t *testing.T) {
	rows := []validationRow{{validationProbe: validationProbe{SrcLabel: "Kali", Dst: "10.0.0.1", Port: "2404", Expected: "deny", Category: "unauthorized"}, Actual: "skipped", ProbeError: "tool not found"}}
	md := euRecipe.renderValidationMarkdown("custom", "source", "hash", "path", false, "", rows, 0, 0, 0, 0, 1, false)
	for _, required := range []string{"tool not found", "not denied", "do not establish why a connection failed", "A refused connection is reached (allow)", "fails an unauthorized row"} {
		if !strings.Contains(md, required) {
			t.Fatal(md)
		}
	}
	if strings.Contains(md, "positive evidence the segmentation is enforcing") {
		t.Fatal(md)
	}
}
