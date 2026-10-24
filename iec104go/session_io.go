// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"io"
	"sync"
)

type eventQueue struct {
	mu       sync.Mutex
	pending  []Event
	wake     chan struct{}
	terminal *Event
}

func (s *Session) queueEvent(e Event) bool {
	e.ConnID = s.id
	e.Session = s
	if e.Kind == ASDUReceived {
		s.mu.Lock()
		e.transfer = s.generation
		s.mu.Unlock()
	}
	s.events.mu.Lock()
	defer s.events.mu.Unlock()
	overflow := len(s.events.pending) >= s.opts.EventQueue
	// Reserve one overflow slot for the event that triggers closure. It must
	// still precede Closed: backpressure cannot silently discard a decoded ASDU.
	s.events.pending = append(s.events.pending, e)
	signal(s.events.wake)
	return !overflow
}
func (s *Session) dispatch() {
	defer func() {
		if s.retired != nil {
			s.retired()
		}
		close(s.done)
	}()
	for {
		<-s.events.wake
		for {
			s.events.mu.Lock()
			var e Event
			if len(s.events.pending) > 0 {
				e = s.events.pending[0]
				s.events.pending[0] = Event{}
				s.events.pending = s.events.pending[1:]
			} else if s.events.terminal != nil {
				e = *s.events.terminal
				s.events.terminal = nil
			} else {
				s.events.mu.Unlock()
				break
			}
			s.events.mu.Unlock()
			if s.handler != nil {
				s.handler(s.ctx, e)
			}
			if e.Kind == Closed {
				return
			}
		}
	}
}
func (s *Session) tap(dir Direction, b []byte) {
	if s.opts.Tap == nil {
		return
	}
	s.tapMu.Lock()
	defer s.tapMu.Unlock()
	s.opts.Tap(dir, s.id, s.opts.Clock.Now(), append([]byte(nil), b...))
}
func (s *Session) readLoop() {
	defer s.workers.Done()
	r := NewAPDUReader(s.conn)
	for {
		f, err := r.ReadFrame()
		leg := s.receivingLeg()
		at := s.opts.Clock.Now()
		if err == nil {
			b, encodeErr := f.MarshalBinary()
			if encodeErr != nil {
				err = encodeErr
			} else {
				s.tap(Receive, b)
			}
		}
		if err != nil {
			s.transportFailed.Store(true)
		}
		select {
		case s.in <- incoming{f, err, at, leg}:
		case <-s.ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}
func (s *Session) writeLoop() {
	defer s.workers.Done()
	for {
		// A local close takes precedence over queued traffic, but never races an
		// active Write. That Write may already cover the final cumulative ACK.
		select {
		case done := <-s.closeFlush:
			s.writeFinalACK(done)
			return
		default:
		}
		select {
		case <-s.ctx.Done():
			return
		case done := <-s.closeFlush:
			s.writeFinalACK(done)
			return
		case queued := <-s.out:
			// Queue capacity changed before the potentially blocking tap/Write.
			// Completion of the previous write is not a dequeue notification.
			signal(s.wake)
			select {
			case done := <-s.closeFlush:
				s.writeFinalACK(done)
				return
			default:
			}
			w := s.claimWire(queued)
			result := s.writeAPDU(w)
			select {
			case s.written <- result:
			case <-s.ctx.Done():
				return
			}
			if result.err != nil {
				return
			}
		}
	}
}
func (s *Session) writeAPDU(w wireWrite) writeResult {
	at := s.opts.Clock.Now()
	s.tap(Transmit, w.bytes)
	w.leg.beginWrite()
	// A short write is terminal; retrying would violate the one-Write tap and
	// packet-conformance contract and risks sending an ambiguous partial APDU.
	n, err := s.conn.Write(w.bytes)
	if err == nil && n != len(w.bytes) {
		err = io.ErrShortWrite
	}
	w.leg.endWrite(err)
	if err != nil {
		s.transportFailed.Store(true)
	} else if w.receiveCount != 0 {
		s.acknowledged.Store(w.receiveCount)
	}
	return writeResult{err, w.closeAfter, at}
}
func (s *Session) finish(err error) {
	s.mu.Lock()
	s.gone = true
	s.accepting = false
	s.reason = err
	s.sends = nil
	if s.generation != nil {
		s.generation.cancel()
	}
	s.notifySendLocked()
	if s.procedure != nil {
		s.procedure.fail(err)
	}
	s.mu.Unlock()
	s.cancel()
	// Closing the transport interrupts blocked reads and writes before joining.
	_ = s.conn.Close()
	s.workers.Wait()
	close(s.transportDone)
	s.events.mu.Lock()
	s.events.terminal = &Event{Kind: Closed, ConnID: s.id, Session: s, Err: err}
	s.events.mu.Unlock()
	signal(s.events.wake)
}
