// SPDX-License-Identifier: Apache-2.0

package iec104go

import "bytes"

// A threshold receive at the controlled station has one causal ACK decision.
// The first admitted handler response (or callback return without a response)
// resolves it; t2 can independently resolve it while the handler is blocked.
// All mutable fields below are guarded by Session.mu, never callback timing
// guesses, scheduler yields or a wall-clock delay.
type receiveACK struct {
	request []byte
	first   []byte
	done    bool
}

func (s *Session) beginCallbackACK(ticket *receiveACK) {
	s.mu.Lock()
	s.callbackACK = ticket
	s.mu.Unlock()
}
func (s *Session) endCallbackACK(ticket *receiveACK) {
	s.mu.Lock()
	if ticket != nil {
		ticket.done = true
	}
	s.callbackACK = nil
	s.mu.Unlock()
	signal(s.wake)
}
func (st *sessionState) callbackACKDecision() (ready, piggyback bool) {
	s := st.s
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket := st.ackWait
	if ticket.first == nil {
		return ticket.done, false
	}
	// The callback can advance a test clock before admitting its response,
	// after the actor's loop-level expiry check. Observe time after that
	// admission under the same mutex, not only before entering pump.
	if due(s.opts.Clock.Now(), st.t2) {
		return true, false
	}
	// C/C negative-addressing distinguishes early envelope rejection (44/46)
	// from callback-level rejection (45/47). The library never creates these
	// responses: only an explicitly admitted, exact application mirror qualifies.
	// If k or application admission ordering prevents transmission, w still
	// wins and requires S. Writer queue occupancy must not choose the policy:
	// it changes when the writer goroutine is scheduled, not on a protocol input.
	if st.outstanding() >= s.opts.APCI.K || len(s.sends) == 0 || s.sends[0].ack != ticket {
		return true, false
	}
	b, request := ticket.first, ticket.request
	if len(b) != len(request) || len(b) < 3 || b[2]&0x40 == 0 {
		return true, false
	}
	cause := COT(b[2] & 63)
	if cause != UnknownType && cause != UnknownCA {
		return true, false
	}
	return true, bytes.Equal(b[:2], request[:2]) && bytes.Equal(b[3:], request[3:]) && b[2]&0x80 == request[2]&0x80
}
