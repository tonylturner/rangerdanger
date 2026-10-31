// SPDX-License-Identifier: Apache-2.0

package iec104go

import "time"

// This is a shutdown profile bound, not a new APCI timer. A short configured t2
// remains an upper bound; virtual time must interrupt a blocked final Write too.
const closeACKLimit = 100 * time.Millisecond

// Called only by the state goroutine: accepted receive state is frozen here.
// Do not use st.pending: it clears on queueing an ACK, before Write succeeds.
func (s *Session) flushClose(timer Timer) {
	s.admission(false)
	if s.ctx.Err() != nil || s.transportFailed.Load() || s.received.Load() == s.acknowledged.Load() {
		return
	}
	resetTimer(timer, s.opts.Clock.Now().Add(min(closeACKLimit, s.opts.APCI.T2)))
	done := make(chan struct{})
	s.closeFlush <- done
	for {
		select {
		case <-done:
			return
		case <-timer.C():
			return
		case <-s.ctx.Done():
			return
		case in := <-s.in:
			if in.err != nil {
				return
			}
		case w := <-s.written:
			// Drain results so an active Write can release the sole writer to
			// service the flush. Ordinary APCI timers and sends no longer run.
			if w.err != nil {
				return
			}
		}
	}
}

// The sole writer rechecks successful wire acknowledgements after any in-flight
// I/S Write. No redundant S frame is emitted if that Write covered the target.
func (s *Session) writeFinalACK(done chan struct{}) {
	defer close(done)
	count := s.received.Load()
	if s.ctx.Err() != nil || s.transportFailed.Load() || count == s.acknowledged.Load() {
		return
	}
	b, _ := (Frame{Kind: SFrame, Receive: uint16(count) & 32767}).MarshalBinary()
	s.writeAPDU(wireWrite{bytes: b, receiveCount: count})
}
