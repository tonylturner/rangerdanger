// SPDX-License-Identifier: Apache-2.0

package iec104go

// Each SBO leg has a separate write boundary. Reader-side tagging prevents a
// response read before that boundary from becoming eligible after actor delay.
type procedureLeg struct {
	tx             *transaction
	begun, written bool // guarded by tx.mu
}

func (l *procedureLeg) beginWrite() {
	if l != nil {
		l.tx.mu.Lock()
		l.begun = true
		l.tx.mu.Unlock()
	}
}
func (l *procedureLeg) endWrite(err error) {
	if l == nil {
		return
	}
	l.tx.mu.Lock()
	l.written = err == nil
	if err != nil && l.tx.err == nil {
		l.tx.err = err
	}
	l.tx.mu.Unlock()
	signal(l.tx.wake)
}
func (s *Session) receivingLeg() *procedureLeg {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.procedure == nil {
		return nil
	}
	t := s.procedure
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.leg != nil && t.leg.begun {
		return t.leg
	}
	return nil
}
func (c *Client) sendProcedure(t *transaction, request ASDU, match func(Message) bool) error {
	b, err := request.MarshalBinary(c.opts.Params)
	if err != nil {
		return err
	}
	t.mu.Lock()
	if t.err != nil {
		err := t.err
		t.mu.Unlock()
		return err
	}
	leg := &procedureLeg{tx: t}
	t.leg = leg
	t.match = match
	// Already buffered replies from select must never satisfy execute.
	t.consumed = len(t.responses)
	t.mu.Unlock()
	return c.admitLeg(b, leg)
}
