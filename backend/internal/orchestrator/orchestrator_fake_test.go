package orchestrator

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type scriptedDocker struct {
	*client.Client // All unlisted SDK calls fail the test by panic.
	mu             sync.Mutex
	calls          []string
	fails          map[string]error
	exitCode       int
}

func (f *scriptedDocker) record(op, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op+":"+id)
	return f.fails[op+":"+id]
}
func (f *scriptedDocker) ContainerCreate(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
	err := f.record("create", name)
	return container.CreateResponse{ID: name}, err
}
func (f *scriptedDocker) NetworkConnect(_ context.Context, network, id string, _ *network.EndpointSettings) error {
	return f.record("connect", id+"@"+network)
}
func (f *scriptedDocker) ContainerStart(_ context.Context, id string, _ container.StartOptions) error {
	return f.record("start", id)
}
func (f *scriptedDocker) ContainerStop(_ context.Context, id string, _ container.StopOptions) error {
	return f.record("stop", id)
}
func (f *scriptedDocker) ContainerRemove(_ context.Context, id string, _ container.RemoveOptions) error {
	return f.record("remove", id)
}
func (f *scriptedDocker) ContainerInspect(_ context.Context, id string) (types.ContainerJSON, error) {
	err := f.record("inspect", id)
	return types.ContainerJSON{NetworkSettings: &types.NetworkSettings{Networks: map[string]*network.EndpointSettings{"rangerdanger_field_net": {IPAddress: "10.40.40.20"}}}}, err
}
func (f *scriptedDocker) ContainerExecCreate(_ context.Context, id string, _ container.ExecOptions) (types.IDResponse, error) {
	err := f.record("exec-create", id)
	return types.IDResponse{ID: "exec-" + id}, err
}
func (f *scriptedDocker) ContainerExecStart(_ context.Context, id string, _ container.ExecStartOptions) error {
	return f.record("exec-start", id)
}
func (f *scriptedDocker) ContainerExecAttach(_ context.Context, id string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
	if err := f.record("exec-attach", id); err != nil {
		return types.HijackedResponse{}, err
	}
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
	return container.ExecInspect{ExitCode: f.exitCode}, f.record("exec-inspect", id)
}
func (f *scriptedDocker) ContainerLogs(_ context.Context, id string, _ container.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), f.record("logs", id)
}
func (f *scriptedDocker) ContainerExecResize(_ context.Context, id string, _ container.ResizeOptions) error {
	return f.record("resize", id)
}

var _ DockerAPI = (*scriptedDocker)(nil)
