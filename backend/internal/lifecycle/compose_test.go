package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestComposeArgv(t *testing.T) {
	cli := &ComposeCLI{Project: "rangerdanger", Root: "/H"}
	file := "/H/lab-definitions/packages/pkg-a/compose.release.yml"
	model := []string{"-p", "rangerdanger", "--project-directory", "/H", "-f", file}
	tests := map[string]struct {
		got, want []string
	}{
		"config": {cli.configArgs(file), append(append([]string(nil), model...), "config", "--format", "json")},
		"images": {cli.imagesArgs(file), append(append([]string(nil), model...), "config", "--images")},
		"up": {cli.upArgs(file), append(append([]string(nil), model...),
			"up", "-d", "--wait", "--wait-timeout", "300", "--no-build", "--pull", "never")},
		// Label-only: no -f, no --project-directory.
		"down": {cli.downArgs(), []string{"-p", "rangerdanger", "down", "-v", "--remove-orphans"}},
	}
	for name, tt := range tests {
		if !reflect.DeepEqual(tt.got, tt.want) {
			t.Errorf("%s argv = %q, want %q", name, tt.got, tt.want)
		}
	}
}

func TestComposeEnvIsMinimal(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	t.Setenv("RANGERDANGER_PACKAGE", "pkg-a")
	env := ComposeEnv("s3cret")
	if env[0] != "CONTAIND_JWT_SECRET=s3cret" {
		t.Errorf("env[0] = %q, want the containd secret", env[0])
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "DOCKER_HOST=unix:///var/run/docker.sock") {
		t.Errorf("env %q lacks DOCKER_HOST", env)
	}
	if strings.Contains(joined, "RANGERDANGER_") {
		t.Errorf("env %q leaks backend configuration; Compose reads H/.env", env)
	}
}

// fakeComposeBinary writes a shell script that stands in for Compose.
func fakeComposeBinary(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker-compose")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestComposeRunsWithoutShellInDirAndEnv(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	binary := fakeComposeBinary(t, `{ pwd; echo "$CONTAIND_JWT_SECRET"; for a in "$@"; do echo "arg:$a"; done; } > "`+out+`"
printf 'rd/fw:1\n\nrd/plc:1\nrd/fw:1\n'
`)
	workdir := t.TempDir()
	cli := &ComposeCLI{Binary: binary, Project: "rangerdanger", Root: "/H", Dir: workdir, Env: []string{"CONTAIND_JWT_SECRET=s3cret"}}

	images, err := cli.Images(t.Context(), "/H/x.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(images, []string{"rd/fw:1", "rd/plc:1"}) {
		t.Errorf("images = %q, want deduplicated non-empty lines", images)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	resolved, _ := filepath.EvalSymlinks(workdir)
	if got, _ := filepath.EvalSymlinks(lines[0]); got != resolved {
		t.Errorf("working directory = %q, want %q", lines[0], workdir)
	}
	if lines[1] != "s3cret" {
		t.Errorf("CONTAIND_JWT_SECRET = %q", lines[1])
	}
	// "with spaces" stays one argument: no shell parses the argv.
	if _, err := cli.Config(t.Context(), "/H/with spaces.yml"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(out)
	if !strings.Contains(string(data), "arg:/H/with spaces.yml\n") {
		t.Errorf("argv was split: %s", data)
	}
}

func TestComposeErrorCarriesStderr(t *testing.T) {
	binary := fakeComposeBinary(t, `echo "dependency failed to start: container x is unhealthy" >&2; exit 1`)
	cli := &ComposeCLI{Binary: binary, Project: "rangerdanger", Root: "/H", Dir: t.TempDir()}
	err := cli.Up(t.Context(), "/H/x.yml")
	if err == nil || !strings.Contains(err.Error(), "unhealthy") || !strings.Contains(err.Error(), "--pull never") {
		t.Fatalf("Up error = %v, want argv and stderr", err)
	}
}

func TestComposeCancelKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	// The child stands in for anything Compose spawns.
	binary := fakeComposeBinary(t, `sleep 30 &
echo $! > "`+pidFile+`"
wait
`)
	cli := &ComposeCLI{Binary: binary, Project: "rangerdanger", Root: "/H", Dir: t.TempDir()}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- cli.Down(ctx) }()

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		data, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("fake compose never started its child")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Down after cancel = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Down did not return after cancel")
	}
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("child %d survived the cancel", pid)
}
