// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"fmt"
	"time"
)

type sessionView struct {
	sent, received, acked uint16
	pending               int
	started               bool
	t1, t2, idle          time.Time
}
type sessionState struct {
	s                     *Session
	sent, received, acked uint16
	pending               int
	started               bool
	peerStop, localStop   bool
	startWait, stopWait   []chan error
	u                     map[UFunction]time.Time
	t1, t2, idle, drain   time.Time
	closeAfter            error
	ackWait               *receiveACK
}

func seqDistance(a, b uint16) int         { return int((b - a) & 32767) }
func (st *sessionState) outstanding() int { return seqDistance(st.acked, st.sent) }
func (s *Session) run() {
	st := sessionState{s: s, u: make(map[UFunction]time.Time), idle: s.opts.Clock.Now().Add(s.opts.APCI.T3)}
	timer := s.opts.Clock.NewTimer(st.idle)
	var failure error
	var deferred *incoming
	defer func() {
		timer.Stop()
		if failure == nil {
			failure = ErrClosed
		}
		for _, waiters := range [][]chan error{st.startWait, st.stopWait} {
			for _, w := range waiters {
				w <- failure
			}
		}
		s.finish(failure)
	}()
	for {
		// An already-expired injected deadline wins over a simultaneously ready
		// callback result. Otherwise a select lottery changes S versus piggyback.
		if err := st.expire(); err != nil {
			failure = err
			return
		}
		if err := st.pump(); err != nil {
			failure = err
			return
		}
		if deferred != nil && s.received.Load()-s.acknowledged.Load() < uint64(s.opts.APCI.W) {
			in := *deferred
			deferred = nil
			if err := st.input(in); err != nil {
				failure = err
				return
			}
			continue
		}
		resetTimer(timer, st.nextDeadline())
		incoming := s.in
		// Preserve every threshold S frame rather than coalescing it. Apply
		// backpressure at the receive window, not by losing cumulative replies.
		if deferred != nil || len(s.out) >= s.opts.SendQueue {
			incoming = nil
		}
		select {
		case <-s.ctx.Done():
			failure = s.ctx.Err()
			return
		case <-s.stop:
			failure = ErrClosed
			if st.closeAfter == nil {
				s.flushClose(timer)
			}
			return
		case <-s.wake:
		case op := <-s.ops:
			if err := st.operation(op); err != nil {
				failure = err
				return
			}
		case in := <-incoming:
			if in.err == nil && in.frame.Kind == IFrame && s.received.Load()-s.acknowledged.Load() >= uint64(s.opts.APCI.W) {
				// Keep U/S processing independent of a blocked callback. Only a
				// further I-frame needs a window slot. Its arrival must not change
				// an existing callback decision from piggyback to S by a select race.
				deferred = &in
				continue
			}
			if err := st.input(in); err != nil {
				failure = err
				return
			}
		case w := <-s.written:
			if w.err != nil {
				failure = w.err
				return
			}
			if w.closeAfter {
				failure = st.closeAfter
				return
			}
			st.activity(w.at)
		case <-timer.C():
			if err := st.expire(); err != nil {
				failure = err
				return
			}
		case result := <-s.inspect:
			result <- sessionView{st.sent, st.received, st.acked, st.pending, st.started, st.t1, st.t2, st.idle}
		}
	}
}
func (st *sessionState) input(in incoming) error {
	if st.closeAfter != nil {
		return nil
	}
	if in.err != nil {
		return in.err
	}
	st.activity(in.at)
	return st.receive(in.frame, in.leg)
}
func (st *sessionState) activity(at time.Time) {
	deadline := at.Add(st.s.opts.APCI.T3)
	if deadline.After(st.idle) {
		st.idle = deadline
	}
}
func (st *sessionState) emit(f Frame, closeAfter bool) error {
	return st.emitLeg(f, closeAfter, nil)
}
func (st *sessionState) emitLeg(f Frame, closeAfter bool, leg *procedureLeg) error {
	if err := st.s.enqueueWire(f, closeAfter, leg); err != nil {
		return err
	}
	st.idle = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T3)
	return nil
}
func (st *sessionState) sendU(u UFunction) error {
	if err := st.emit(Frame{Kind: UFrame, Function: u}, false); err != nil {
		return err
	}
	switch u {
	case StartDTAct:
		st.u[StartDTCon] = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T1)
	case StopDTAct:
		st.u[StopDTCon] = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T1)
	case TestFRAct:
		st.u[TestFRCon] = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T1)
	}
	return nil
}
func (st *sessionState) ack(closeAfter bool) error {
	if err := st.emit(Frame{Kind: SFrame, Receive: st.received}, closeAfter); err != nil {
		return err
	}
	st.pending = 0
	st.ackWait = nil
	st.t2 = time.Time{}
	return nil
}
func (st *sessionState) acknowledge(n uint16) error {
	distance := seqDistance(st.acked, n)
	if distance > st.outstanding() {
		return fmt.Errorf("%w: N(R) outside outstanding range", ErrProtocol)
	}
	if distance == 0 {
		return nil
	}
	st.acked = n
	st.t1 = time.Time{}
	if st.outstanding() > 0 {
		st.t1 = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T1)
	}
	return nil
}
func (st *sessionState) pump() error {
	if st.closeAfter != nil || !st.started {
		return nil
	}
	if st.ackWait != nil {
		ready, piggyback := st.callbackACKDecision()
		if !ready {
			return nil
		}
		if piggyback {
			// Retain t2 until the chosen I-frame is actually emitted. In
			// particular, writer scheduling cannot turn queue pressure into
			// an immediate S or silently remove the pending ACK's deadline.
			if len(st.s.out) >= st.s.opts.SendQueue {
				return nil
			}
			st.ackWait = nil
		} else if err := st.ack(false); err != nil {
			return err
		}
	}
	for st.outstanding() < st.s.opts.APCI.K {
		// A large k is legal even with a tiny admission queue. A slow transport
		// must leave admitted data queued, not turn internal writer saturation
		// into a connection failure. Reserve the extra slots for control frames.
		if len(st.s.out) >= st.s.opts.SendQueue {
			break
		}
		st.s.mu.Lock()
		if len(st.s.sends) == 0 {
			st.s.mu.Unlock()
			break
		}
		payload := st.s.sends[0]
		st.s.sends[0] = queuedASDU{}
		st.s.sends = st.s.sends[1:]
		st.s.notifySendLocked()
		st.s.mu.Unlock()
		if err := st.emitLeg(Frame{Kind: IFrame, Send: st.sent, Receive: st.received, ASDU: payload.bytes}, false, payload.leg); err != nil {
			return err
		}
		if st.outstanding() == 0 {
			st.t1 = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T1)
		}
		st.sent = (st.sent + 1) & 32767
		st.pending = 0
		st.t2 = time.Time{}
	}
	st.s.mu.Lock()
	empty := len(st.s.sends) == 0
	st.s.mu.Unlock()
	if st.localStop && empty {
		if _, waiting := st.u[StopDTCon]; !waiting {
			if err := st.sendU(StopDTAct); err != nil {
				return err
			}
		}
	}
	if st.peerStop && empty && st.outstanding() == 0 {
		if st.pending > 0 {
			if err := st.ack(false); err != nil {
				return err
			}
		}
		if err := st.sendU(StopDTCon); err != nil {
			return err
		}
		st.peerStop = false
		st.drain = time.Time{}
		st.started = false
		if !st.s.queueEvent(Event{Kind: Stopped}) {
			return ErrEventQueueFull
		}
	}
	return nil
}
func (st *sessionState) operation(op operation) error {
	if op.start {
		if st.localStop || st.peerStop {
			op.result <- ErrNotStarted
			return nil
		}
		if st.started {
			op.result <- nil
			return nil
		}
		st.startWait = append(st.startWait, op.result)
		if len(st.startWait) == 1 {
			return st.sendU(StartDTAct)
		}
	} else {
		if len(st.startWait) > 0 {
			op.result <- ErrNotStarted
			return nil
		}
		if !st.started {
			op.result <- nil
			return nil
		}
		st.s.admission(false)
		st.localStop = true
		st.stopWait = append(st.stopWait, op.result)
		if st.drain.IsZero() {
			st.drain = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T1)
		}
	}
	return nil
}
func (st *sessionState) receive(f Frame, leg *procedureLeg) error {
	switch f.Kind {
	case UFrame:
		return st.receiveU(f.Function)
	case IFrame, SFrame:
		if !st.started {
			return fmt.Errorf("%w: I/S in STOPPED", ErrProtocol)
		}
		if err := st.acknowledge(f.Receive); err != nil {
			return err
		}
		if f.Kind == SFrame {
			return nil
		}
		if f.Send != st.received {
			err := fmt.Errorf("%w: unexpected N(S)", ErrProtocol)
			if st.pending == 0 {
				return err
			}
			st.closeAfter = err
			st.s.admission(false)
			st.drain = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T1)
			return st.ack(true)
		}
		m, err := DecodeASDU(f.ASDU, st.s.opts.Params)
		if err != nil {
			return err
		}
		st.received = (st.received + 1) & 32767
		st.s.received.Add(1)
		if st.pending == 0 {
			st.t2 = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T2)
		}
		st.pending++
		// Procedure observation gets its own copy before application code can mutate
		// the delivered message, and never runs on the callback goroutine.
		st.s.observe(f.ASDU, leg)
		var ticket *receiveACK
		if st.pending >= st.s.opts.APCI.W && st.s.role == ServerRole && st.s.handler != nil {
			ticket = &receiveACK{request: append([]byte(nil), f.ASDU...)}
			st.ackWait = ticket
		}
		if !st.s.queueEvent(Event{Kind: ASDUReceived, ASDU: m, ack: ticket}) {
			return ErrEventQueueFull
		}
		if st.pending >= st.s.opts.APCI.W && ticket == nil {
			return st.ack(false)
		}
	}
	return nil
}
func (st *sessionState) receiveU(u UFunction) error {
	switch u {
	case TestFRAct:
		return st.sendU(TestFRCon)
	case TestFRCon:
		if _, ok := st.u[u]; !ok {
			return fmt.Errorf("%w: unsolicited TESTFR con", ErrProtocol)
		}
		delete(st.u, u)
		return nil
	case StartDTAct:
		if st.s.role != ServerRole || st.peerStop {
			return fmt.Errorf("%w: unexpected STARTDT act", ErrProtocol)
		}
		if err := st.sendU(StartDTCon); err != nil {
			return err
		}
		if !st.started {
			st.started = true
			st.s.admission(true)
			if !st.s.queueEvent(Event{Kind: Started}) {
				return ErrEventQueueFull
			}
		}
	case StopDTAct:
		if st.s.role != ServerRole || st.peerStop {
			return fmt.Errorf("%w: unexpected STOPDT act", ErrProtocol)
		}
		if !st.started {
			return st.sendU(StopDTCon)
		}
		st.s.admission(false)
		st.peerStop = true
		st.drain = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T1)
		if st.pending > 0 {
			return st.ack(false)
		}
	case StartDTCon, StopDTCon:
		if st.s.role != ClientRole {
			return fmt.Errorf("%w: server received transfer con", ErrProtocol)
		}
		if _, ok := st.u[u]; !ok {
			return fmt.Errorf("%w: unsolicited transfer con", ErrProtocol)
		}
		delete(st.u, u)
		if u == StartDTCon {
			st.started = true
			st.s.admission(true)
			if !st.s.queueEvent(Event{Kind: Started}) {
				return ErrEventQueueFull
			}
			for _, w := range st.startWait {
				w <- nil
			}
			st.startWait = nil
		} else {
			if st.outstanding() != 0 {
				return fmt.Errorf("%w: STOPDT con before drain", ErrProtocol)
			}
			// Clear any final receive ACK before entering STOPPED. Otherwise t2
			// could emit an S-frame after the peer has stopped transfer.
			if st.pending > 0 {
				if err := st.ack(false); err != nil {
					return err
				}
			}
			st.started = false
			st.localStop = false
			st.drain = time.Time{}
			if !st.s.queueEvent(Event{Kind: Stopped}) {
				return ErrEventQueueFull
			}
			for _, w := range st.stopWait {
				w <- nil
			}
			st.stopWait = nil
		}
	}
	return nil
}
