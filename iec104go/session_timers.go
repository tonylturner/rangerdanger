// SPDX-License-Identifier: Apache-2.0

package iec104go

import "time"

func stopTimer(t Timer) {
	if !t.Stop() {
		select {
		case <-t.C():
		default:
		}
	}
}
func resetTimer(t Timer, deadline time.Time) {
	stopTimer(t)
	t.Reset(deadline)
}
func earlier(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() || a.Before(b) {
		return a
	}
	return b
}
func (st *sessionState) nextDeadline() time.Time {
	next := earlier(st.idle, earlier(st.t1, earlier(st.t2, st.drain)))
	for _, deadline := range st.u {
		next = earlier(next, deadline)
	}
	return next
}
func due(now, deadline time.Time) bool { return !deadline.IsZero() && !now.Before(deadline) }
func (st *sessionState) expire() error {
	now := st.s.opts.Clock.Now()
	if due(now, st.t1) || due(now, st.drain) {
		return ErrT1
	}
	for _, deadline := range st.u {
		if due(now, deadline) {
			return ErrT1
		}
	}
	if due(now, st.t2) {
		if err := st.ack(false); err != nil {
			return err
		}
	}
	if due(now, st.idle) {
		// A short t3 must not retransmit an unanswered TESTFR or move its t1.
		if _, pending := st.u[TestFRCon]; !pending {
			return st.sendU(TestFRAct)
		}
		st.idle = now.Add(st.s.opts.APCI.T3)
	}
	return nil
}
