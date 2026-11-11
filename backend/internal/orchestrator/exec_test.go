package orchestrator

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type scriptedDocker struct {
	mu         sync.Mutex
	calls      []string
	exitCode   int
	running    bool
	holdOpen   bool
	inspectErr error
	createErr  error
	attachErr  error
}

func (f *scriptedDocker) record(op, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op+":"+id)
}
func (f *scriptedDocker) ContainerExecCreate(_ context.Context, id string, _ container.ExecOptions) (types.IDResponse, error) {
	f.record("exec-create", id)
	return types.IDResponse{ID: "exec-" + id}, f.createErr
}
func (f *scriptedDocker) ContainerExecAttach(_ context.Context, id string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
	f.record("exec-attach", id)
	if f.attachErr != nil {
		return types.HijackedResponse{}, f.attachErr
	}
	server, peer := net.Pipe()
	go func() {
		defer peer.Close()
		out := stdcopy.NewStdWriter(peer, stdcopy.Stdout)
		errout := stdcopy.NewStdWriter(peer, stdcopy.Stderr)
		_, _ = out.Write([]byte("hello"))
		_, _ = errout.Write([]byte("warning"))
		if f.holdOpen {
			_, _ = io.Copy(io.Discard, peer)
		}
	}()
	return types.NewHijackedResponse(server, ""), nil
}

func TestExecCommandTimeoutDuringStart(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		fake       *scriptedDocker
	}{
		{"create", "the process was not started", &scriptedDocker{createErr: context.DeadlineExceeded}},
		{"attach", "process state is unknown and it may still be running", &scriptedDocker{attachErr: context.DeadlineExceeded}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := &Orchestrator{dockerClient: tc.fake}
			_, _, code, err := o.ExecCommand(t.Context(), "alpha", []string{"tcpdump"}, 1)
			if code != -1 || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "command timed out") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %d %v", code, err)
			}
		})
	}
}
func (f *scriptedDocker) ContainerExecInspect(_ context.Context, id string) (container.ExecInspect, error) {
	f.record("exec-inspect", id)
	return container.ExecInspect{ExitCode: f.exitCode, Running: f.running}, f.inspectErr
}

func TestExecCommandTimeoutReportsProcessState(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		running    bool
		inspectErr error
	}{
		{"running", "the process is still running in the container; Run did not stop it", true, nil},
		{"stopped", "the process is no longer running", false, nil},
		{"unavailable", "process state is unknown (status unavailable); it may still be running", false, errors.New("unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &scriptedDocker{running: tc.running, holdOpen: true, inspectErr: tc.inspectErr}
			o := &Orchestrator{dockerClient: fake}
			start := time.Now()
			out, stderr, code, err := o.ExecCommand(t.Context(), "alpha", []string{"tcpdump"}, 1)
			if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "command timed out") ||
				!strings.Contains(err.Error(), tc.want) || code != -1 || out != "hello" || stderr != "warning" {
				t.Fatalf("got %q %q %d %v", out, stderr, code, err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("deadline did not close attached stream promptly")
			}
		})
	}
}

func TestExecCommandDoesNotReportUnfinishedCommandAsSuccess(t *testing.T) {
	for _, tc := range []struct {
		name       string
		running    bool
		inspectErr error
	}{
		{"running after output EOF", true, nil},
		{"inspect unavailable", false, errors.New("unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := &Orchestrator{dockerClient: &scriptedDocker{running: tc.running, inspectErr: tc.inspectErr}}
			_, _, code, err := o.ExecCommand(t.Context(), "alpha", []string{"ls"}, 1)
			if code != -1 || err == nil || !strings.Contains(err.Error(), "still be running") && !strings.Contains(err.Error(), "still running") {
				t.Fatalf("got %d %v", code, err)
			}
		})
	}
}

// Exercise the SDK's actual HTTP upgrade and multiplexed reader, without Docker.
func TestExecCommandTimeoutThroughDockerSDK(t *testing.T) {
	detached := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.47/containers/alpha/exec":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"Id":"run"}`)
		case "/v1.47/exec/run/start":
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			defer close(detached)
			_, _ = buf.WriteString("HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			if err := buf.Flush(); err != nil {
				t.Error(err)
				return
			}
			_, _ = stdcopy.NewStdWriter(conn, stdcopy.Stdout).Write([]byte("capture started"))
			_, _ = io.Copy(io.Discard, conn)
		case "/v1.47/exec/run/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Running":true,"ExitCode":0}`)
		default:
			t.Errorf("unexpected SDK request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	cli, err := client.NewClientWithOpts(client.WithHost("tcp://"+strings.TrimPrefix(upstream.URL, "http://")), client.WithVersion("1.47"))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	o := &Orchestrator{dockerClient: cli}
	out, _, code, err := o.ExecCommand(t.Context(), "alpha", []string{"tcpdump"}, 1)
	if !errors.Is(err, context.DeadlineExceeded) || code != -1 || out != "capture started" || !strings.Contains(err.Error(), "process is still running") {
		t.Fatalf("got %q %d %v", out, code, err)
	}
	select {
	case <-detached:
	case <-time.After(time.Second):
		t.Fatal("SDK attachment was not closed")
	}
}
func (f *scriptedDocker) ContainerExecResize(_ context.Context, id string, _ container.ResizeOptions) error {
	f.record("resize", id)
	return nil
}

var _ DockerAPI = (*scriptedDocker)(nil)

func TestExecCommand(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(string(rune('0'+code)), func(t *testing.T) {
			fake := &scriptedDocker{exitCode: code}
			o := &Orchestrator{dockerClient: fake}
			out, stderr, got, err := o.ExecCommand(context.Background(), "alpha", []string{"echo", "hello"}, 1)
			if err != nil || out != "hello" || stderr != "warning" || got != code {
				t.Fatalf("got %q %q %d %v", out, stderr, got, err)
			}
			want := []string{"exec-create:alpha", "exec-attach:exec-alpha", "exec-inspect:exec-alpha"}
			if len(fake.calls) != len(want) {
				t.Fatalf("calls = %v, want %v", fake.calls, want)
			}
			for i := range want {
				if fake.calls[i] != want[i] {
					t.Fatalf("calls = %v, want %v", fake.calls, want)
				}
			}
		})
	}
}
