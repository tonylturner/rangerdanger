// SPDX-License-Identifier: Apache-2.0

package iec104go

import "encoding/binary"

// Keep FIFO positions, but replace unsent cumulative replies in place. A slow
// Write can then hold at most one pending S ACK and one pending TESTFR con,
// regardless of how many inbound frames the peer sends in the meantime.
func (s *Session) enqueueWire(f Frame, closeAfter bool, leg *procedureLeg) error {
	b, err := f.MarshalBinary()
	if err != nil {
		return err
	}
	s.wireMu.Lock()
	defer s.wireMu.Unlock()
	var slot **wireWrite
	if f.Kind == SFrame {
		slot = &s.pendingAck
	} else if f.Kind == UFrame && f.Function == TestFRCon {
		slot = &s.pendingTestCon
	}
	if slot != nil && *slot != nil {
		(*slot).bytes = b
		(*slot).closeAfter = (*slot).closeAfter || closeAfter
		return nil
	}
	w := &wireWrite{bytes: b, closeAfter: closeAfter, leg: leg}
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
	if s.pendingAck == w {
		s.pendingAck = nil
	}
	if s.pendingTestCon == w {
		s.pendingTestCon = nil
	}
	result := *w
	// A coalesced S ACK can precede an older queued I-frame. Refresh every
	// I/S acknowledgement at the write boundary so N(R) never moves backward,
	// including across wrap; taps observe exactly these final wire bytes.
	if result.bytes[2]&1 == 0 || result.bytes[2] == 1 {
		binary.LittleEndian.PutUint16(result.bytes[4:6], uint16(s.received.Load())<<1)
	}
	return result
}
