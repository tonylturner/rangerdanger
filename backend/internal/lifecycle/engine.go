package lifecycle

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// projectLabel is the label Compose puts on every resource of a project.
const projectLabel = "com.docker.compose.project"

// Engine is what the lifecycle asks of the Docker Engine directly.
type Engine interface {
	// MissingImages returns the references not present in the local store.
	MissingImages(ctx context.Context, refs []string) ([]string, error)
	// ProjectResources returns the names of the containers and networks
	// that carry the project's Compose label.
	ProjectResources(ctx context.Context, project string) (containers, networks []string, err error)
	// ContainersNamed returns which of names are held by a container,
	// whatever its project.
	ContainersNamed(ctx context.Context, names []string) ([]string, error)
	// Exec runs argv in a running container and returns its exit code and
	// combined output.
	Exec(ctx context.Context, container string, argv []string) (int, string, error)
}

// DockerAPI is the Docker SDK surface DockerEngine uses.
type DockerAPI interface {
	ImageInspectWithRaw(ctx context.Context, image string) (types.ImageInspect, []byte, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
}

// ExecFunc runs a command in a container: stdout, stderr, exit code. It is
// the orchestrator's ExecCommand.
type ExecFunc func(ctx context.Context, container string, cmd []string, timeoutSec int) (string, string, int, error)

// execTimeoutSec bounds one exec, such as nginx -t.
const execTimeoutSec = 30

// DockerEngine implements Engine with the Docker SDK.
type DockerEngine struct {
	api  DockerAPI
	exec ExecFunc
}

var _ Engine = (*DockerEngine)(nil)

// NewDockerEngine builds an Engine over the SDK and an exec function.
func NewDockerEngine(api DockerAPI, exec ExecFunc) *DockerEngine {
	return &DockerEngine{api: api, exec: exec}
}

// MissingImages implements Engine.
func (e *DockerEngine) MissingImages(ctx context.Context, refs []string) ([]string, error) {
	var missing []string
	for _, ref := range refs {
		if _, _, err := e.api.ImageInspectWithRaw(ctx, ref); err != nil {
			if !client.IsErrNotFound(err) {
				return nil, fmt.Errorf("inspect image %s: %w", ref, err)
			}
			missing = append(missing, ref)
		}
	}
	return missing, nil
}

// ProjectResources implements Engine.
func (e *DockerEngine) ProjectResources(ctx context.Context, project string) ([]string, []string, error) {
	label := filters.NewArgs(filters.Arg("label", projectLabel+"="+project))
	containers, err := e.api.ContainerList(ctx, container.ListOptions{All: true, Filters: label})
	if err != nil {
		return nil, nil, fmt.Errorf("list containers of project %s: %w", project, err)
	}
	networks, err := e.api.NetworkList(ctx, network.ListOptions{Filters: label})
	if err != nil {
		return nil, nil, fmt.Errorf("list networks of project %s: %w", project, err)
	}
	var containerNames, networkNames []string
	for _, c := range containers {
		containerNames = append(containerNames, containerName(c))
	}
	for _, n := range networks {
		networkNames = append(networkNames, n.Name)
	}
	sort.Strings(containerNames)
	sort.Strings(networkNames)
	return containerNames, networkNames, nil
}

// ContainersNamed implements Engine. The Engine's name filter matches
// substrings, so the result is checked for exact names.
func (e *DockerEngine) ContainersNamed(ctx context.Context, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	args := filters.NewArgs()
	want := map[string]bool{}
	for _, name := range names {
		args.Add("name", "^/"+name+"$")
		want[name] = true
	}
	containers, err := e.api.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return nil, fmt.Errorf("list containers by name: %w", err)
	}
	var held []string
	for _, c := range containers {
		if name := containerName(c); want[name] {
			held = append(held, name)
		}
	}
	sort.Strings(held)
	return held, nil
}

// Exec implements Engine.
func (e *DockerEngine) Exec(ctx context.Context, container string, argv []string) (int, string, error) {
	stdout, stderr, code, err := e.exec(ctx, container, argv, execTimeoutSec)
	if err != nil {
		return -1, "", err
	}
	return code, strings.TrimSpace(stdout + stderr), nil
}

func containerName(c types.Container) string {
	if len(c.Names) == 0 {
		return c.ID
	}
	return strings.TrimPrefix(c.Names[0], "/")
}
