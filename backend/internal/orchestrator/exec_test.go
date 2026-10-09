package orchestrator

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

type scriptedDocker struct {
	mu       sync.Mutex
	calls    []string
	exitCode int
}

func (f *scriptedDocker) record(op, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op+":"+id)
}
func (f *scriptedDocker) ContainerExecCreate(_ context.Context, id string, _ container.ExecOptions) (types.IDResponse, error) {
	f.record("exec-create", id)
	return types.IDResponse{ID: "exec-" + id}, nil
}
func (f *scriptedDocker) ContainerExecAttach(_ context.Context, id string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
	f.record("exec-attach", id)
	server, peer := net.Pipe()
	go func() {
		defer peer.Close()
		out := stdcopy.NewStdWriter(peer, stdcopy.Stdout)
		errout := stdcopy.NewStdWriter(peer, stdcopy.Stderr)
		_, _ = out.Write([]byte("hello"))
		_, _ = errout.Write([]byte("warning"))
	}()
	return types.NewHijackedResponse(server, ""), nil
}
func (f *scriptedDocker) ContainerExecInspect(_ context.Context, id string) (container.ExecInspect, error) {
	f.record("exec-inspect", id)
	return container.ExecInspect{ExitCode: f.exitCode}, nil
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
