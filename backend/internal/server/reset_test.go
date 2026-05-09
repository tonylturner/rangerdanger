package server

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// TestResetCommandsAreSupported pins the contract between the canonical
// resetDeviceCommands list and each field-device sim. Every (device,
// command) pair must resolve to a `case "X":` handler in the matching
// services/<device>-sim/main.go switch — otherwise the sim returns
// "unknown command" and the reset endpoint reports success:false at
// workshop time. This caught the original capbank/clear_alarm bug.
//
// The contract is one-directional: every reset command must be a real
// handler. The reverse isn't asserted (sims have handlers that aren't
// in the reset path — e.g. relay's `trip`, regulator's `raise_tap` —
// because reset only restores defaults, not exercise actions).
func TestResetCommandsAreSupported(t *testing.T) {
	supportedByDevice := map[string]map[string]bool{}
	for _, device := range []string{"relay", "recloser", "regulator", "capbank"} {
		supportedByDevice[device] = loadSimCommandHandlers(t, device)
	}

	for _, cmd := range resetDeviceCommands {
		t.Run(cmd.device+"/"+cmd.command, func(t *testing.T) {
			handlers, ok := supportedByDevice[cmd.device]
			if !ok {
				t.Fatalf("unknown device %q in resetDeviceCommands", cmd.device)
			}
			if !handlers[cmd.command] {
				t.Errorf("reset command %q is not a handler in services/%s-sim/main.go (would surface as success:false at runtime)",
					cmd.command, cmd.device)
			}
		})
	}
}

// loadSimCommandHandlers reads services/<device>-sim/main.go and extracts
// every `case "<command>":` token from the command-dispatch switch. The
// regex is deliberately narrow — anchored to the leading tab indent that
// case statements inside a switch produce — so other strings in the file
// (constants, comments, log messages) don't pollute the set.
func loadSimCommandHandlers(t *testing.T, device string) map[string]bool {
	t.Helper()

	// Resolve repo root from this test file's location: the test runs
	// under backend/internal/server, so services/ is three dirs up.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	simPath := filepath.Join(repoRoot, "services", device+"-sim", "main.go")

	data, err := os.ReadFile(simPath)
	if err != nil {
		t.Fatalf("read %s: %v", simPath, err)
	}

	re := regexp.MustCompile(`(?m)^\s+case "([a-z_]+)":`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	out := make(map[string]bool, len(matches))
	for _, m := range matches {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatalf("no case handlers found in %s — regex needs updating?", simPath)
	}
	return out
}
