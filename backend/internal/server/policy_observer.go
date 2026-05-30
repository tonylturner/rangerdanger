package server

import (
	"context"
	"log"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// policyObserver watches containd's running config for changes the
// backend didn't initiate. The dual-track firewall labs let students
// pick a "Technical" track where they author and commit policy
// directly in containd's web UI or CLI — that path skips the
// backend's apply handlers entirely, so without this observer the
// in-memory activeConfig + policySource would stay stuck on
// whatever the last backend-initiated apply set them to, the
// banner would never switch to "Your custom policy (your containd
// commit)", and the lab's validations would keep checking the
// stale value.
//
// Mechanism: every observerInterval, fetch the canonical hash of
// containd's firewall sub-document. If it diverges from
// lastAppliedHash (the hash recorded at the last successful backend
// apply) AND the divergence has persisted past the grace window
// (so we don't flip on the brief in-flight period between
// candidate → commit in a normal backend apply), set
// activeConfig=custom + policySource=manual-custom, and update
// lastAppliedHash to the observed value so we don't keep
// re-flipping every tick.
//
// The grace window is deliberately a soft check: the backend
// records lastAppliedAt when it sets lastAppliedHash, and the
// observer skips if (now - lastAppliedAt) < observerGracePeriod.

const (
	// observerInterval — how often to poll containd. Tighter would
	// catch manual commits faster but adds backend load; loose
	// would mean the banner takes a few seconds longer to flip on
	// a manual commit. 5s lines up with the frontend's pollState
	// cadence so a student who refreshes the page after committing
	// in containd sees the banner update within one frontend tick.
	observerInterval = 5 * time.Second

	// observerGracePeriod — minimum time after a backend apply
	// before the observer is willing to reclassify the state. A
	// normal apply takes <2s end to end; 5s leaves plenty of
	// headroom for slow CI without making manual-commit detection
	// feel laggy.
	observerGracePeriod = 5 * time.Second
)

// startPolicyObserver kicks off the background goroutine. Idempotent
// per Server instance — caller should invoke once from New() after
// the containd client is wired up. Returns immediately; the
// goroutine runs until ctx is cancelled (test code) or the
// process exits (production).
func (s *Server) startPolicyObserver(ctx context.Context) {
	if s.containdClient == nil {
		// Tests construct Server without a containd client; the
		// observer just becomes a no-op in that case.
		return
	}
	go s.policyObserverLoop(ctx)
}

func (s *Server) policyObserverLoop(ctx context.Context) {
	ticker := time.NewTicker(observerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.observePolicyOnce()
		}
	}
}

// observePolicyOnce is one tick of the observer. Extracted so the
// unit test can step it deterministically.
func (s *Server) observePolicyOnce() {
	hash, err := s.containdClient.GetFirewallHash()
	if err != nil {
		// Containd may be restarting / unreachable. The next tick
		// will retry; no value in spamming logs on a transient
		// failure, so this stays silent.
		return
	}

	s.activeConfigMu.Lock()
	defer s.activeConfigMu.Unlock()

	// Grace window: don't reclassify if a backend apply happened
	// very recently. This catches the brief in-flight period
	// between the apply handler recording lastAppliedHash and
	// containd actually finishing the candidate → commit cycle.
	if !s.lastAppliedAt.IsZero() && time.Since(s.lastAppliedAt) < observerGracePeriod {
		return
	}

	// No baseline yet (server just started, never applied anything
	// since boot) — adopt the current running hash as the baseline.
	// Don't try to label it; some other path (the seed-from-config
	// step in New()) sets activeConfig from the config path. This
	// just stops the next tick from spuriously flipping to
	// manual-custom because the very first observation looks
	// "different from empty".
	if s.lastAppliedHash == "" {
		s.lastAppliedHash = hash
		return
	}

	if hash == s.lastAppliedHash {
		return
	}

	// Divergence persists past the grace window — student
	// committed a policy outside this backend.
	prev := s.activeConfig
	s.activeConfig = "custom"
	s.policySource = "manual-custom"
	s.lastAppliedHash = hash
	// Don't touch lastAppliedAt — leaving it as-is so a subsequent
	// backend apply still wins the grace check correctly.
	log.Printf("policy-observer: detected manual containd commit (was %q, now custom/manual-custom)", prev)
}

// recordApply is called by the apply handlers (and apply-custom)
// to seed the observer's baseline. Pass the bytes of the config
// that was actually committed — the helper hashes its firewall
// sub-document the same way the observer hashes containd's running
// view, so the comparison is apples-to-apples. Caller holds
// activeConfigMu.
func (s *Server) recordApplyLocked(configBytes []byte) {
	// FirewallHashFromBytes will auto-unwrap a top-level "firewall"
	// key, so callers can pass either a full config doc or just the
	// firewall sub-object — whichever they already have in hand.
	// recordApply uses the imported containd helper, not a local
	// one, so the canonical form stays in lockstep with the
	// observer's GetFirewallHash.
	h, err := containd.FirewallHashFromBytes(configBytes)
	if err != nil {
		// A failure here just means the observer's next tick will
		// briefly classify this apply as a manual-custom (because
		// lastAppliedHash didn't get the new value). Not a
		// correctness regression — the next normal apply will
		// resync — so we log and move on rather than panic.
		log.Printf("policy-observer: recordApply hash failed: %v", err)
		return
	}
	s.lastAppliedHash = h
	s.lastAppliedAt = time.Now()
}
