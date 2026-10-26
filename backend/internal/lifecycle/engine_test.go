package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
)

type notFoundError struct{}

func (notFoundError) Error() string { return "No such image" }
func (notFoundError) NotFound()     {}

type fakeDockerAPI struct {
	images     map[string]error
	containers []types.Container
	networks   []network.Summary
	volumes    map[string]error
	listed     []container.ListOptions
	netListed  []network.ListOptions
}

func (f *fakeDockerAPI) VolumeInspect(_ context.Context, name string) (volume.Volume, error) {
	return volume.Volume{Name: name}, f.volumes[name]
}

func (f *fakeDockerAPI) ImageInspectWithRaw(_ context.Context, ref string) (types.ImageInspect, []byte, error) {
	return types.ImageInspect{}, nil, f.images[ref]
}

func (f *fakeDockerAPI) ContainerList(_ context.Context, options container.ListOptions) ([]types.Container, error) {
	f.listed = append(f.listed, options)
	return f.containers, nil
}

func (f *fakeDockerAPI) NetworkList(_ context.Context, options network.ListOptions) ([]network.Summary, error) {
	f.netListed = append(f.netListed, options)
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
	labels := options.Filters.Get("label")
	slices.Sort(labels)
	if !options.All || !reflect.DeepEqual(labels, []string{"com.docker.compose.oneoff", "com.docker.compose.project=rangerdanger"}) {
		t.Errorf("container list options = %+v, want all Compose-created project containers", options)
	}
}

func TestDockerEngineComposeOwnershipFilters(t *testing.T) {
	for _, method := range []string{"ProjectResources", "MountedVolumes"} {
		t.Run(method, func(t *testing.T) {
			// The fake does not apply filters. Check the request rather than
			// pretending its response proves inherited labels are excluded.
			api := &fakeDockerAPI{}
			engine := NewDockerEngine(api, nil)
			const project = "training-range"
			var err error
			if method == "ProjectResources" {
				_, _, err = engine.ProjectResources(t.Context(), project)
			} else {
				_, err = engine.MountedVolumes(t.Context(), project)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(api.listed) != 1 {
				t.Fatalf("container list calls = %d, want 1", len(api.listed))
			}
			options := api.listed[0]
			labels := options.Filters.Get("label")
			slices.Sort(labels)
			// Key-only oneoff accepts both service and one-off containers.
			want := []string{"com.docker.compose.oneoff", "com.docker.compose.project=" + project}
			if !options.All || options.Filters.Len() != 1 || !reflect.DeepEqual(labels, want) {
				t.Errorf("container list options = %+v, want all containers with labels %q", options, want)
			}
			if method == "ProjectResources" {
				if len(api.netListed) != 1 {
					t.Fatalf("network list calls = %d, want 1", len(api.netListed))
				}
				filters := api.netListed[0].Filters
				if filters.Len() != 1 || !reflect.DeepEqual(filters.Get("label"), []string{"com.docker.compose.project=" + project}) {
					t.Errorf("network filters = %+v, want only the project label", filters)
				}
			}
		})
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

func TestDockerEngineVolumes(t *testing.T) {
	api := &fakeDockerAPI{
		containers: []types.Container{
			{Names: []string{"/rangerdanger-eng-ws"}, Mounts: []types.MountPoint{
				{Type: mount.TypeBind, Source: "/H/scripts/set-gateway.sh"},
				{Type: mount.TypeVolume, Name: "c0ffee"},
			}},
			{Names: []string{"/rangerdanger-corp-ws"}, Mounts: []types.MountPoint{{Type: mount.TypeVolume, Name: "beef"}}},
		},
		volumes: map[string]error{"c0ffee": notFoundError{}},
	}
	engine := NewDockerEngine(api, nil)
	mounted, err := engine.MountedVolumes(t.Context(), "rangerdanger")
	if err != nil || !reflect.DeepEqual(mounted, []string{"beef", "c0ffee"}) {
		t.Fatalf("MountedVolumes = %q, %v; want the volume mounts only", mounted, err)
	}
	labels := api.listed[0].Filters.Get("label")
	slices.Sort(labels)
	if !reflect.DeepEqual(labels, []string{"com.docker.compose.oneoff", "com.docker.compose.project=rangerdanger"}) {
		t.Errorf("label filter = %q", labels)
	}
	existing, err := engine.ExistingVolumes(t.Context(), mounted)
	if err != nil || !reflect.DeepEqual(existing, []string{"beef"}) {
		t.Fatalf("ExistingVolumes = %q, %v", existing, err)
	}
	api.volumes["beef"] = errors.New("daemon unreachable")
	if _, err := engine.ExistingVolumes(t.Context(), []string{"beef"}); err == nil {
		t.Error("an inspect failure other than not-found was reported as gone")
	}
}
