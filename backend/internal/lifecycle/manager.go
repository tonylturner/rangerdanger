// Package lifecycle owns the range: the one Compose project of the package
// being served. It switches packages through a serialized transition
// (preflight, stopping, starting, configuring, ready), keeps the runtime
// record that survives backend restarts, reconciles that record when the
// backend starts, and hands range-bound work the current Generation.
//
// The platform (backend, frontend, proxy) is a separate Compose project that
// this package never touches.
package lifecycle

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// RoutesFile is the active package's routes file inside the proxy routes
// directory, which the proxy includes.
const RoutesFile = "range.conf"

var (
	// ErrBusy rejects a request while a transition or reconcile runs.
	ErrBusy = errors.New("a range transition is already running")
	// ErrUnknownPackage rejects a package that is not in the catalog.
	ErrUnknownPackage = errors.New("unknown package")
)

// Timeouts bound the lifecycle's waits. Zero fields take the defaults.
type Timeouts struct {
	Drain         time.Duration // stopping barrier
	Config        time.Duration // each `compose config` run
	Up            time.Duration // `compose up --wait`, including container creation
	Down          time.Duration // `compose down`
	ContaindReady time.Duration // firewall API answering after up
}

func (t Timeouts) withDefaults() Timeouts {
	set := func(value *time.Duration, fallback time.Duration) {
		if *value == 0 {
			*value = fallback
		}
	}
	set(&t.Drain, 15*time.Second)
	set(&t.Config, 60*time.Second)
	set(&t.Up, upWaitTimeout+30*time.Second)
	set(&t.Down, 180*time.Second)
	set(&t.ContaindReady, 120*time.Second)
	return t
}

// Options wires a Manager.
type Options struct {
	Root           string        // installation root H, the Compose project directory
	Mode           manifest.Mode // which Compose file of a package runs
	DefinitionsDir string        // base of labs.Package.FirewallConfigPath
	RoutesDir      string        // directory the proxy includes routes from
	ProxyContainer string        // platform proxy, for nginx -t and reload
	DefaultPackage string        // RANGERDANGER_PACKAGE: used until a range is recorded
	Version        string        // backend build, written to the record

	Store       RecordStore
	Compose     Compose
	Engine      Engine
	Catalog     func() *labs.Catalog
	NewContaind func(baseURL string) *containd.Client
	// Activate starts a new generation's background workers. It runs
	// before the generation serves requests.
	Activate func(*Generation)

	Timeouts Timeouts
	Now      func() time.Time
}

func (o Options) validate() error {
	switch {
	case !filepath.IsAbs(o.Root):
		return fmt.Errorf("installation root %q is not an absolute path", o.Root)
	case o.Mode != manifest.ModeSource && o.Mode != manifest.ModeRelease:
		return fmt.Errorf("mode %q is neither %s nor %s", o.Mode, manifest.ModeSource, manifest.ModeRelease)
	case o.DefinitionsDir == "" || o.RoutesDir == "" || o.ProxyContainer == "" || o.DefaultPackage == "" || o.Store.Path == "":
		return errors.New("definitions dir, routes dir, proxy container, default package and record path are required")
	case o.Compose == nil || o.Engine == nil || o.Catalog == nil || o.NewContaind == nil || o.Activate == nil:
		return errors.New("compose, engine, catalog, containd factory and activate hook are required")
	}
	return nil
}

// Status is the public view of the lifecycle (GET /api/range).
type Status struct {
	Generation uint64        `json:"generation"`
	Phase      Phase         `json:"phase"`
	Package    string        `json:"package"`
	Target     string        `json:"target"` // only in preflight
	Mode       manifest.Mode `json:"mode"`
	Error      string        `json:"error"`
	UpdatedAt  *time.Time    `json:"updated_at"`
}

// Manager runs the range lifecycle. Exactly one transition or reconcile
// runs at a time, on its own goroutine with a context independent of any
// HTTP request.
type Manager struct {
	opts     Options
	timeouts Timeouts

	mu      sync.Mutex
	rec     *Record // last record written or loaded; nil before the first range
	loadErr error   // the record on disk could not be read
	status  Status
	current *Generation // serving generation, if any
	busy    bool
	idle    chan struct{} // closed when the running job ends
}

// New loads the runtime record. A record that cannot be read does not stop
// the backend: Start treats it like an interrupted transition.
func New(opts Options) (*Manager, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	m := &Manager{opts: opts, timeouts: opts.Timeouts.withDefaults(), idle: closedChan()}
	rec, err := opts.Store.Load()
	switch {
	case err != nil:
		m.loadErr = err
		now := opts.Now()
		m.status = Status{Phase: PhaseFailed, Mode: opts.Mode, Error: err.Error(), UpdatedAt: &now}
	case rec != nil:
		m.rec = rec
		m.status = statusOf(*rec)
	default:
		m.status = Status{Phase: PhaseNone, Mode: opts.Mode}
	}
	return m, nil
}

func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func statusOf(rec Record) Status {
	updated := rec.UpdatedAt
	return Status{
		Generation: rec.Generation,
		Phase:      rec.Phase,
		Package:    rec.Package,
		Mode:       rec.Mode,
		Error:      rec.Error,
		UpdatedAt:  &updated,
	}
}

// Status returns the current lifecycle status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// ActivePackage is the package the backend serves: the recorded package,
// else the configured default.
func (m *Manager) ActivePackage() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activePackageLocked()
}

func (m *Manager) activePackageLocked() string {
	if m.rec != nil && m.rec.Package != "" {
		return m.rec.Package
	}
	return m.opts.DefaultPackage
}

// Enter admits one range-bound request. It succeeds while the phase is
// ready, or preflight with the previous range still serving, and returns
// the generation with the release to call when the request ends. Otherwise
// it returns the status that explains the refusal.
func (m *Manager) Enter(name string) (*Generation, func(), Status, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.status
	if m.current == nil || (st.Phase != PhaseReady && st.Phase != PhasePreflight) {
		return nil, nil, st, false
	}
	release, ok := m.current.Enter(name)
	if !ok {
		return nil, nil, st, false
	}
	return m.current, release, st, true
}

// Start reconciles the record left by the previous backend process. The
// backend never starts a range on its own: no record means phase none.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy || (m.loadErr == nil && (m.rec == nil || m.rec.Phase == PhaseFailed)) {
		return
	}
	m.launchLocked(m.reconcile)
}

// Request starts a transition to packageID (empty: the active package)
// and returns the status it starts from.
func (m *Manager) Request(packageID string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy {
		return m.status, ErrBusy
	}
	if packageID == "" {
		packageID = m.activePackageLocked()
	}
	pkg, ok := findPackage(m.opts.Catalog(), packageID)
	if !ok {
		return m.status, fmt.Errorf("%w %q", ErrUnknownPackage, packageID)
	}
	previous := m.status
	now := m.opts.Now()
	m.status.Phase = PhasePreflight
	m.status.Target = pkg.ID
	m.status.Error = ""
	m.status.UpdatedAt = &now
	m.launchLocked(func() { m.transition(pkg, previous) })
	return m.status, nil
}

func (m *Manager) launchLocked(job func()) {
	m.busy = true
	idle := make(chan struct{})
	m.idle = idle
	go func() {
		defer close(idle)
		job()
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
	}()
}

// wait blocks until the running job, if any, has ended.
func (m *Manager) wait() {
	m.mu.Lock()
	idle := m.idle
	m.mu.Unlock()
	<-idle
}

func findPackage(catalog *labs.Catalog, id string) (labs.Package, bool) {
	if catalog == nil {
		return labs.Package{}, false
	}
	for _, pkg := range catalog.Packages {
		if pkg.ID == id {
			return pkg, true
		}
	}
	return labs.Package{}, false
}

// persist writes rec as the new record and status. The in-memory state
// follows rec even when the write fails, so the failure stays visible.
func (m *Manager) persist(rec Record) error {
	rec.Schema = RecordSchema
	rec.UpdatedAt = m.opts.Now().UTC()
	err := m.opts.Store.Save(rec)
	m.mu.Lock()
	m.rec = &rec
	m.loadErr = nil
	m.status = statusOf(rec)
	if err != nil && rec.Error == "" {
		m.status.Error = err.Error()
	}
	m.mu.Unlock()
	return err
}

// record returns a copy of the current record, or a blank one.
func (m *Manager) record() Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rec == nil {
		return Record{}
	}
	return *m.rec
}
