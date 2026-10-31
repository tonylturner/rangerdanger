package server

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type resetRunnerStub struct {
	container string
	command   []string
	timeout   int
	stderr    string
	exitCode  int
	err       error
}

func (r *resetRunnerStub) ExecCommand(_ context.Context, container string, command []string, timeout int) (string, string, int, error) {
	r.container, r.command, r.timeout = container, command, timeout
	return "", r.stderr, r.exitCode, r.err
}

func TestResetCleanupReportsFailures(t *testing.T) {
	for _, test := range []struct {
		name, stderr string
		code         int
		err          error
		want         string
	}{
		{"success", "", 0, nil, ""},
		{"docker error", "", -1, errors.New("exec attach failed"), "exec attach failed"},
		{"command failed", "permission denied\n", 1, nil, "exited 1: permission denied"},
		{"unknown exit status", "", -1, nil, "exited -1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &resetRunnerStub{stderr: test.stderr, exitCode: test.code, err: test.err}
			err := runResetCleanup(t.Context(), runner, "firewall", "rm -f /data/captures/*.pcap")
			if (err == nil) != (test.want == "") || (err != nil && !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("cleanup error = %v, want %q", err, test.want)
			}
			if runner.container != "firewall" || runner.timeout != 5 ||
				!reflect.DeepEqual(runner.command, []string{"sh", "-c", "rm -f /data/captures/*.pcap"}) {
				t.Fatalf("wrong cleanup invocation: %+v", runner)
			}
		})
	}
}
