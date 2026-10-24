// SPDX-License-Identifier: Apache-2.0

package iec104go

import "context"

// A generation's identity is its allocation, never reused while work retains it.
// Cancellation happens at the APCI admission boundary, not in a later callback.
type transferGeneration struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (s *Session) admission(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v && !s.accepting {
		ctx, cancel := context.WithCancel(s.ctx)
		s.generation = &transferGeneration{ctx, cancel}
	} else if !v && s.generation != nil {
		s.generation.cancel()
	}
	s.accepting = v
	s.notifySendLocked()
}

// Broadcast a capacity/state change to all waiters. Capturing the channel and
// checking admission under the same mutex prevents a lost capacity wakeup.
func (s *Session) notifySendLocked() {
	if s.sendReady != nil {
		close(s.sendReady)
	}
	s.sendReady = make(chan struct{})
}

func (s *Session) admitFileLocked(ctx context.Context, generation *transferGeneration, b []byte) error {
	if s.gone {
		return ErrConnectionGone
	}
	if generation == nil || generation != s.generation || generation.ctx.Err() != nil {
		return ErrNotStarted
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.admitLocked(b, nil)
}
