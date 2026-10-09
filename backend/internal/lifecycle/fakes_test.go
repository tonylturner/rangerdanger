package lifecycle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

const proxyContainer = "rd-test-proxy"

// fakeEngine is the Docker Engine as the lifecycle sees it.
type fakeEngine struct {
	mu        sync.Mutex
	missing   map[string]bool // image references not in the store
	project   []string        // containers labelled with the range project
	networks  []string        // networks labelled with the range project
	foreign   []string        // container names held outside the project
	execs     []string
	execCodes map[string]int // exit code by joined argv
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{missing: map[string]bool{}, execCodes: map[string]int{}}
}

func (e *fakeEngine) MissingImages(_ context.Context, refs []string) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var missing []string
	for _, ref := range refs {
		if e.missing[ref] {
			missing = append(missing, ref)
		}
	}
	return missing, nil
}

func (e *fakeEngine) ProjectResources(_ context.Context, project string) ([]string, []string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if project != manifest.RangeProject {
		return nil, nil, nil
	}
	return append([]string(nil), e.project...), append([]string(nil), e.networks...), nil
}

func (e *fakeEngine) ContainersNamed(_ context.Context, names []string) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	holders := append(append([]string(nil), e.project...), e.foreign...)
	var held []string
	for _, name := range names {
		for _, holder := range holders {
			if holder == name {
				held = append(held, name)
			}
		}
	}
	return held, nil
}

func (e *fakeEngine) Exec(_ context.Context, container string, argv []string) (int, string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.execs = append(e.execs, container+" "+strings.Join(argv, " "))
	if code := e.execCodes[strings.Join(argv, " ")]; code != 0 {
		return code, "nginx: [emerg] test failure", nil
	}
	return 0, "", nil
}

func (e *fakeEngine) execLog() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.execs...)
}

// fakeCompose models the range project in the fake engine: up creates the
// manifest's containers, down removes everything with the project label.
type fakeCompose struct {
	engine *fakeEngine
	images []string

	mu         sync.Mutex
	containers map[string][]string // what up of a package's file creates, by package dir
	calls      []string
	upErr      error
	downLeaves bool   // down leaves a container behind
	onConfig   func() // runs at the start of every config
	onDown     func() // runs at the start of every down
	// models replaces the normalized model of a package's file, by package dir.
	models map[string][]byte
}

func (c *fakeCompose) record(call string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
}

func (c *fakeCompose) callLog() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

func (c *fakeCompose) resetCalls() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = nil
}

func packageOf(file string) string { return filepath.Base(filepath.Dir(file)) }

func (c *fakeCompose) Config(_ context.Context, file string) ([]byte, error) {
	c.record("config " + packageOf(file))
	c.mu.Lock()
	onConfig := c.onConfig
	c.mu.Unlock()
	if onConfig != nil {
		onConfig()
	}
	c.mu.Lock()
	model, replaced := c.models[packageOf(file)]
	c.mu.Unlock()
	if replaced {
		return model, nil
	}
	return normalizedModel(filepath.Dir(file))
}

// normalizedModel is what `compose config --format json` prints for a
// Compose file that agrees with the package's manifest.
func normalizedModel(dir string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifest.FileName))
	if err != nil {
		return nil, err
	}
	var man manifest.Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, err
	}
	services := map[string]any{}
	for _, svc := range man.Services {
		networks := map[string]any{}
		for _, iface := range svc.Interfaces {
			networks[iface.Network] = map[string]any{"ipv4_address": iface.IPv4}
		}
		services[svc.Key] = map[string]any{"container_name": svc.Container, "networks": networks}
	}
	networks := map[string]any{}
	for _, network := range man.Networks {
		if network.Platform {
			networks[network.Key] = map[string]any{"name": network.Name, "external": true}
			continue
		}
		networks[network.Key] = map[string]any{"name": network.Name,
			"ipam": map[string]any{"config": []any{map[string]any{"subnet": network.Subnet, "gateway": network.Gateway}}}}
	}
	return json.Marshal(map[string]any{"name": manifest.RangeProject, "services": services, "networks": networks})
}

func (c *fakeCompose) Images(_ context.Context, file string) ([]string, error) {
	c.record("images " + packageOf(file))
	return c.images, nil
}

func (c *fakeCompose) Up(_ context.Context, file string) error {
	c.record("up " + packageOf(file))
	c.mu.Lock()
	upErr, created := c.upErr, c.containers[packageOf(file)]
	c.mu.Unlock()
	c.engine.mu.Lock()
	defer c.engine.mu.Unlock()
	// A failed up still leaves resources behind, like a real half-start.
	c.engine.project = append([]string(nil), created...)
	c.engine.networks = []string{"rangerdanger_field_net"}
	return upErr
}

func (c *fakeCompose) Down(context.Context) error {
	c.record("down")
	c.mu.Lock()
	onDown, leaves := c.onDown, c.downLeaves
	c.mu.Unlock()
	if onDown != nil {
		onDown()
	}
	c.engine.mu.Lock()
	defer c.engine.mu.Unlock()
	c.engine.project = nil
	c.engine.networks = nil
	if leaves {
		c.engine.project = []string{"rd-test-stuck"}
	}
	return nil
}

// fakeFirewall is a containd API that is ready and accepts imports.
type fakeFirewall struct {
	server  *httptest.Server
	imports atomic.Int32
}

func newFakeFirewall(t *testing.T) *fakeFirewall {
	t.Helper()
	fw := &fakeFirewall{}
	fw.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			_ = json.NewEncoder(w).Encode(containd.HealthStatus{Status: "ok"})
		case "/api/v1/config/candidate":
			fw.imports.Add(1)
		case "/api/v1/config/commit":
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fw.server.Close)
	return fw
}

// harness is one Manager's world over a temporary installation root.
type harness struct {
	t         *testing.T
	root      string
	defs      string
	routesDir string
	store     RecordStore
	engine    *fakeEngine
	compose   *fakeCompose
	firewall  *fakeFirewall
	catalog   *labs.Catalog
	activated chan *Generation
}

// testContainers are the containers of a test package: a firewall and a PLC.
func testContainers(pkg string) []string {
	return []string{"rd-test-" + pkg + "-fw", "rd-test-" + pkg + "-plc"}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	base := t.TempDir()
	engine := newFakeEngine()
	h := &harness{
		t:         t,
		root:      filepath.Join(base, "H"),
		defs:      filepath.Join(base, "defs"),
		routesDir: filepath.Join(base, "routes"),
		store:     RecordStore{Path: filepath.Join(base, "data", RecordFile)},
		engine:    engine,
		compose:   &fakeCompose{engine: engine, images: []string{"rd-test/fw:1", "rd-test/plc:1"}, containers: map[string][]string{}},
		firewall:  newFakeFirewall(t),
		catalog:   &labs.Catalog{},
		activated: make(chan *Generation, 8),
	}
	for _, dir := range []string{filepath.Join(h.defs, "firewall"), h.routesDir, filepath.Dir(h.store.Path)} {
		mustMkdir(t, dir)
	}
	h.addPackage("pkg-a")
	h.addPackage("pkg-b")
	return h
}

func (h *harness) addPackage(id string) {
	h.t.Helper()
	dir := manifest.Dir(h.root, id)
	mustMkdir(h.t, dir)
	names := testContainers(id)
	h.writeManifest(testManifest(id, h.firewall.server.URL))
	mustWrite(h.t, filepath.Join(dir, "package.yml"), []byte("id: "+id+"\n"))
	mustWrite(h.t, filepath.Join(dir, manifest.RoutesFile), []byte("# routes of "+id+"\n"))
	for _, mode := range []manifest.Mode{manifest.ModeSource, manifest.ModeRelease} {
		mustWrite(h.t, filepath.Join(dir, manifest.ComposeFile(mode)), []byte("name: rangerdanger\n"))
	}
	mustWrite(h.t, filepath.Join(h.defs, "firewall", id+".json"), []byte(`{"firewall":{"rules":[]}}`))
	h.catalog.Packages = append(h.catalog.Packages, labs.Package{ID: id, Revision: 3, FirewallConfigPath: "firewall/" + id + ".json",
		Template: labs.LabYAML{
			ID:       "tpl-" + id,
			Networks: []labs.NetworkYAML{{ID: "field_net", Zone: "lan2"}},
			Nodes:    []labs.NodeYAML{{ID: "fw-1", Container: names[0]}, {ID: "plc-1", Container: names[1]}},
		}})
	h.compose.containers[id] = names
}

// testManifest is a valid manifest: the firewall answers the fake containd
// API on the loopback "management" network, the PLC sits on the field.
func testManifest(id, firewallAPI string) manifest.Manifest {
	names := testContainers(id)
	return manifest.Manifest{
		Schema:  manifest.Schema,
		Package: id,
		Networks: []manifest.Network{
			{Key: "mgmt_net", Name: "rangerdanger_mgmt_net", Subnet: "127.0.0.0/24", Gateway: "127.0.0.254", Platform: true},
			{Key: "field_net", Name: "rangerdanger_field_net", Subnet: "10.199.40.0/24", Gateway: "10.199.40.1", Zone: "field_net"},
		},
		Services: []manifest.Service{
			{Key: "firewall", Container: names[0], Node: "fw-1", Roles: []manifest.Role{manifest.RoleFirewall},
				Interfaces: []manifest.Interface{{Network: "mgmt_net", IPv4: "127.0.0.1"}, {Network: "field_net", IPv4: "10.199.40.2"}},
				Endpoints:  map[string]string{"api": firewallAPI}},
			{Key: "plc", Container: names[1], Node: "plc-1", Roles: []manifest.Role{manifest.RolePLC},
				Interfaces: []manifest.Interface{{Network: "field_net", IPv4: "10.199.40.30"}}},
		},
	}
}

func (h *harness) writeManifest(man manifest.Manifest) {
	h.t.Helper()
	data, err := json.Marshal(man)
	if err != nil {
		h.t.Fatal(err)
	}
	mustWrite(h.t, filepath.Join(manifest.Dir(h.root, man.Package), manifest.FileName), data)
}

func (h *harness) options() Options {
	return Options{
		Root:           h.root,
		Mode:           manifest.ModeRelease,
		DefinitionsDir: h.defs,
		RoutesDir:      h.routesDir,
		ProxyContainer: proxyContainer,
		DefaultPackage: "pkg-a",
		Version:        "test",
		Store:          h.store,
		Compose:        h.compose,
		Engine:         h.engine,
		Catalog:        func() *labs.Catalog { return h.catalog },
		NewContaind:    containd.NewClient,
		Activate:       func(g *Generation) { h.activated <- g },
		Timeouts:       Timeouts{Drain: 2 * time.Second, ContaindReady: 10 * time.Second},
	}
}

func (h *harness) manager(opts Options) *Manager {
	h.t.Helper()
	m, err := New(opts)
	if err != nil {
		h.t.Fatalf("New: %v", err)
	}
	return m
}

// switchTo requests pkg and waits for the transition to end.
func (h *harness) switchTo(m *Manager, pkg string) Status {
	h.t.Helper()
	if _, err := m.Request(pkg); err != nil {
		h.t.Fatalf("Request(%s): %v", pkg, err)
	}
	m.wait()
	return m.Status()
}

// ready returns a manager serving pkg, with the call logs cleared.
func (h *harness) ready(opts Options, pkg string) *Manager {
	h.t.Helper()
	m := h.manager(opts)
	if st := h.switchTo(m, pkg); st.Phase != PhaseReady {
		h.t.Fatalf("setup switch to %s: phase %s, error %q", pkg, st.Phase, st.Error)
	}
	<-h.activated
	h.compose.resetCalls()
	h.engine.mu.Lock()
	h.engine.execs = nil
	h.engine.mu.Unlock()
	return m
}

func (h *harness) loadRecord() *Record {
	h.t.Helper()
	rec, err := h.store.Load()
	if err != nil {
		h.t.Fatalf("load record: %v", err)
	}
	return rec
}

func (h *harness) installedRoutes() string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.routesDir, RoutesFile))
	if err != nil {
		h.t.Fatalf("read installed routes: %v", err)
	}
	return string(data)
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
