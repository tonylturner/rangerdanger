// SPDX-License-Identifier: Apache-2.0

package iec104go

// S acknowledgements retain their individual receive snapshots and FIFO order.
// Receive-window backpressure bounds their backlog even with a slow writer.
// Only repeated, unsent TESTFR confirmations share a FIFO position.
func (s *Session) enqueueWire(f Frame, closeAfter bool, leg *procedureLeg) error {
	b, err := f.MarshalBinary()
	if err != nil {
		return err
	}
	s.wireMu.Lock()
	defer s.wireMu.Unlock()
	var slot **wireWrite
	if f.Kind == UFrame && f.Function == TestFRCon {
		slot = &s.pendingTestCon
	}
	if slot != nil && *slot != nil {
		(*slot).bytes = b
		(*slot).closeAfter = (*slot).closeAfter || closeAfter
		return nil
	}
	w := &wireWrite{bytes: b, closeAfter: closeAfter, leg: leg}
	if f.Kind == IFrame || f.Kind == SFrame {
		w.receiveCount = s.received.Load()
	}
	select {
	case s.out <- w:
		if slot != nil {
			*slot = w
		}
	default:
		return ErrQueueFull
	}
	return nil
}

func (s *Session) claimWire(w *wireWrite) wireWrite {
	s.wireMu.Lock()
	defer s.wireMu.Unlock()
	if s.pendingTestCon == w {
		s.pendingTestCon = nil
	}
	result := *w
	return result
}
