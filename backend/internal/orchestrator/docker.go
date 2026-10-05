package orchestrator

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// DockerAPI is the Docker surface used by the orchestrator. DockerClient
// separately exposes the concrete client to legacy server callers.
type DockerAPI interface {
	ContainerCreate(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *ocispec.Platform, string) (container.CreateResponse, error)
	NetworkConnect(context.Context, string, string, *network.EndpointSettings) error
	ContainerStart(context.Context, string, container.StartOptions) error
	ContainerStop(context.Context, string, container.StopOptions) error
	ContainerRemove(context.Context, string, container.RemoveOptions) error
	ContainerInspect(context.Context, string) (types.ContainerJSON, error)
	ContainerExecCreate(context.Context, string, container.ExecOptions) (types.IDResponse, error)
	ContainerExecAttach(context.Context, string, container.ExecAttachOptions) (types.HijackedResponse, error)
	ContainerExecStart(context.Context, string, container.ExecStartOptions) error
	ContainerExecResize(context.Context, string, container.ResizeOptions) error
	ContainerExecInspect(context.Context, string) (container.ExecInspect, error)
	ContainerLogs(context.Context, string, container.LogsOptions) (io.ReadCloser, error)
}

var _ DockerAPI = (*client.Client)(nil)
