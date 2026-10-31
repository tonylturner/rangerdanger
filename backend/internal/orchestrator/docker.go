package orchestrator

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// DockerAPI is the Docker surface used by the orchestrator. DockerClient
// separately exposes the concrete client to legacy server callers.
type DockerAPI interface {
	ContainerExecCreate(context.Context, string, container.ExecOptions) (types.IDResponse, error)
	ContainerExecAttach(context.Context, string, container.ExecAttachOptions) (types.HijackedResponse, error)
	ContainerExecResize(context.Context, string, container.ResizeOptions) error
	ContainerExecInspect(context.Context, string) (container.ExecInspect, error)
}

var _ DockerAPI = (*client.Client)(nil)
