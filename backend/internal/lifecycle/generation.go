package lifecycle

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// Generation is one started range: its package, manifest and containd
// client, and everything that works against it. Range-bound handlers and
// background workers register with the generation; stopping it cancels its
// context and waits for them to drain.
type Generation struct {
	ID       uint64
	Package  labs.Package
	Manifest *manifest.Manifest

	containd *containd.Client
	ctx      context.Context
	cancel   context.CancelFunc

	// commitMu orders Commit against Stop: a completion either runs before
	// the stop or is dropped.
	commitMu sync.Mutex

	mu      sync.Mutex
	stopped bool
	nextID  uint64
	active  map[uint64]string // registered handlers and workers, by name
	drained chan struct{}     // closed once stopped with nothing active
}

// NewGeneration builds a live generation.
func NewGeneration(id uint64, pkg labs.Package, man *manifest.Manifest, client *containd.Client) *Generation {
	ctx, cancel := context.WithCancel(context.Background())
	return &Generation{
		ID:       id,
		Package:  pkg,
		Manifest: man,
		containd: client,
		ctx:      ctx,
		cancel:   cancel,
		active:   map[uint64]string{},
		drained:  make(chan struct{}),
	}
}

// Context is cancelled when the generation stops.
func (g *Generation) Context() context.Context { return g.ctx }

// Containd is the client for this generation's firewall.
func (g *Generation) Containd() *containd.Client { return g.containd }

// Enter registers a handler or worker. It fails once the generation is
// stopping; release must be called exactly once otherwise.
func (g *Generation) Enter(name string) (release func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return nil, false
	}
	g.nextID++
	id := g.nextID
	g.active[id] = name
	var once sync.Once
	return func() { once.Do(func() { g.leave(id) }) }, true
}

func (g *Generation) leave(id uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.active, id)
	g.closeIfDrainedLocked()
}

func (g *Generation) closeIfDrainedLocked() {
	if !g.stopped || len(g.active) > 0 {
		return
	}
	select {
	case <-g.drained:
	default:
		close(g.drained)
	}
}

// Go runs fn as a registered background worker with the generation's
// context. It reports false, without running fn, once the generation is
// stopping.
func (g *Generation) Go(name string, fn func(ctx context.Context)) bool {
	release, ok := g.Enter(name)
	if !ok {
		return false
	}
	go func() {
		defer release()
		fn(g.ctx)
	}()
	return true
}

// Commit runs fn only while the generation is live, and reports whether it
// ran. Work that finishes after its generation stopped publishes nothing.
func (g *Generation) Commit(fn func()) bool {
	g.commitMu.Lock()
	defer g.commitMu.Unlock()
	g.mu.Lock()
	stopped := g.stopped
	g.mu.Unlock()
	if stopped {
		return false
	}
	fn()
	return true
}

// Stop is the barrier: refuse new entries, cancel the context (which ends
// streams and workers), then wait up to timeout for everything registered
// to return. The error names whatever is still running.
func (g *Generation) Stop(timeout time.Duration) error {
	g.commitMu.Lock()
	g.mu.Lock()
	g.stopped = true
	g.closeIfDrainedLocked()
	g.mu.Unlock()
	g.commitMu.Unlock()
	g.cancel()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-g.drained:
		return nil
	case <-timer.C:
	}
	g.mu.Lock()
	names := make([]string, 0, len(g.active))
	for _, name := range g.active {
		names = append(names, name)
	}
	g.mu.Unlock()
	sort.Strings(names)
	return fmt.Errorf("generation %d did not drain within %s; still running: %s", g.ID, timeout, strings.Join(names, ", "))
}
