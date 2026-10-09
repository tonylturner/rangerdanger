package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

type notFoundError struct{}

func (notFoundError) Error() string { return "No such image" }
func (notFoundError) NotFound()     {}

type fakeDockerAPI struct {
	images     map[string]error
	containers []types.Container
	networks   []network.Summary
	listed     []container.ListOptions
}

func (f *fakeDockerAPI) ImageInspectWithRaw(_ context.Context, ref string) (types.ImageInspect, []byte, error) {
	return types.ImageInspect{}, nil, f.images[ref]
}

func (f *fakeDockerAPI) ContainerList(_ context.Context, options container.ListOptions) ([]types.Container, error) {
	f.listed = append(f.listed, options)
	return f.containers, nil
}

func (f *fakeDockerAPI) NetworkList(context.Context, network.ListOptions) ([]network.Summary, error) {
	return f.networks, nil
}

func TestDockerEngineMissingImages(t *testing.T) {
	api := &fakeDockerAPI{images: map[string]error{"rd/b:1": notFoundError{}, "rd/c:1": notFoundError{}}}
	engine := NewDockerEngine(api, nil)
	missing, err := engine.MissingImages(t.Context(), []string{"rd/a:1", "rd/b:1", "rd/c:1"})
	if err != nil || !reflect.DeepEqual(missing, []string{"rd/b:1", "rd/c:1"}) {
		t.Fatalf("MissingImages = %q, %v", missing, err)
	}
	api.images["rd/a:1"] = errors.New("daemon unreachable")
	if _, err := engine.MissingImages(t.Context(), []string{"rd/a:1"}); err == nil {
		t.Error("an inspect failure other than not-found was reported as missing")
	}
}

func TestDockerEngineProjectResourcesFiltersByLabel(t *testing.T) {
	api := &fakeDockerAPI{
		containers: []types.Container{{Names: []string{"/rangerdanger-plc"}}, {Names: []string{"/rangerdanger-firewall"}}},
		networks:   []network.Summary{{Name: "rangerdanger_field_net"}},
	}
	containers, networks, err := NewDockerEngine(api, nil).ProjectResources(t.Context(), "rangerdanger")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(containers, []string{"rangerdanger-firewall", "rangerdanger-plc"}) || !reflect.DeepEqual(networks, []string{"rangerdanger_field_net"}) {
		t.Errorf("resources = %q %q", containers, networks)
	}
	options := api.listed[0]
	if !options.All || !reflect.DeepEqual(options.Filters.Get("label"), []string{"com.docker.compose.project=rangerdanger"}) {
		t.Errorf("container list options = %+v, want all containers by project label", options)
	}
}

func TestDockerEngineContainersNamedIsExact(t *testing.T) {
	// The Engine's name filter is a pattern; a longer name can come back.
	api := &fakeDockerAPI{containers: []types.Container{{Names: []string{"/rangerdanger-kali-old"}}, {Names: []string{"/rangerdanger-kali"}}}}
	held, err := NewDockerEngine(api, nil).ContainersNamed(t.Context(), []string{"rangerdanger-kali"})
	if err != nil || !reflect.DeepEqual(held, []string{"rangerdanger-kali"}) {
		t.Fatalf("ContainersNamed = %q, %v", held, err)
	}
	if got := api.listed[0].Filters.Get("name"); !reflect.DeepEqual(got, []string{"^/rangerdanger-kali$"}) {
		t.Errorf("name filter = %q", got)
	}
}

func TestDockerEngineExecCombinesOutput(t *testing.T) {
	exec := func(_ context.Context, container string, cmd []string, timeoutSec int) (string, string, int, error) {
		if container != "rangerdanger-proxy" || !reflect.DeepEqual(cmd, []string{"nginx", "-t"}) || timeoutSec != execTimeoutSec {
			t.Errorf("exec %s %q %d", container, cmd, timeoutSec)
		}
		return "", "nginx: configuration file test failed\n", 1, nil
	}
	code, output, err := NewDockerEngine(&fakeDockerAPI{}, exec).Exec(t.Context(), "rangerdanger-proxy", []string{"nginx", "-t"})
	if err != nil || code != 1 || output != "nginx: configuration file test failed" {
		t.Errorf("Exec = %d %q %v", code, output, err)
	}
}
