// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func procedureResponse(t *testing.T, m Message, cot COT) []byte {
	t.Helper()
	m, err := Mirror(m, cot, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.MarshalBinary(DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type procedureProgress struct {
	tx        *transaction
	responses int
	consumed  int
	written   bool
}

func currentProcedureProgress(s *Session) procedureProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := procedureProgress{tx: s.procedure}
	if p.tx != nil {
		p.tx.mu.Lock()
		p.responses = len(p.tx.responses)
		p.consumed = p.tx.consumed
		p.written = p.tx.leg != nil && p.tx.leg.written
		p.tx.mu.Unlock()
	}
	return p
}

func requireSameProcedureProgress(t *testing.T, s *Session, want procedureProgress) {
	t.Helper()
	got := currentProcedureProgress(s)
	if got.tx != want.tx || got.responses != want.responses || got.consumed != want.consumed || got.written != want.written {
		t.Fatalf("procedure advanced on ineligible response: got=%+v want=%+v", got, want)
	}
}

func TestProcedureQueuedRequestCannotComplete(t *testing.T) {
	tag := CP56FromTime(time.Now(), false)
	for _, name := range []string{"command", "GI", "counter", "read", "clock", "test", "reset"} {
		t.Run(name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.APCI.K = 1
			s, p, _ := manualSession(t, ClientRole, opts, nil)
			startClientSession(t, s, p)
			if err := s.Send(sampleMessage()); err != nil {
				t.Fatal(err)
			}
			readFrame(t, p) // Occupy k without acknowledging it.
			c := &Client{s}
			req := request(1, 0, C_DC_NA_1, 9, DoubleCommand{Qualifier: 2})
			term := false
			var run func(context.Context) ([]Message, error)
			switch name {
			case "command":
				term = true
				run = func(ctx context.Context) ([]Message, error) { return c.Command(ctx, req) }
			case "GI":
				req = request(1, 0, C_IC_NA_1, 0, Interrogation{QOI: 20})
				term = true
				run = func(ctx context.Context) ([]Message, error) { return c.Interrogate(ctx, 1, 0, 20) }
			case "counter":
				req = request(1, 0, C_CI_NA_1, 0, CounterInterrogation{QCC: 5})
				term = true
				run = func(ctx context.Context) ([]Message, error) { return c.CounterInterrogate(ctx, 1, 0, 5) }
			case "read":
				req = request(1, 0, M_SP_NA_1, 9, SinglePoint{SIQ: 1})
				run = func(ctx context.Context) ([]Message, error) { return c.Read(ctx, 1, 0, 9) }
			case "clock":
				req = request(1, 0, C_CS_NA_1, 0, ClockSync{Time: tag})
				run = func(ctx context.Context) ([]Message, error) { return c.SyncClock(ctx, 1, 0, tag) }
			case "test":
				req = request(1, 0, C_TS_TA_1, 0, TestCommand{Sequence: 1, Time: tag})
				run = func(ctx context.Context) ([]Message, error) { return c.Test(ctx, 1, 0, 1, tag) }
			case "reset":
				req = request(1, 0, C_RP_NA_1, 0, ResetProcess{QRP: 1})
				run = func(ctx context.Context) ([]Message, error) { return c.Reset(ctx, 1, 0, 1) }
			}
			done := make(chan error, 1)
			ctx := testContext(t)
			go func() { _, err := run(ctx); done <- err }()
			eventually(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.sends) == 1 })
			cause := ActivationCon
			if name == "read" {
				cause = Request
			}
			ns := uint16(0)
			reply := func(nr uint16) {
				writeFrame(t, p, Frame{Kind: IFrame, Send: ns, Receive: nr, ASDU: procedureResponse(t, req, cause)})
				ns++
				if term {
					writeFrame(t, p, Frame{Kind: IFrame, Send: ns, Receive: nr, ASDU: procedureResponse(t, req, ActivationTerm)})
					ns++
				}
				waitView(t, s, func(v sessionView) bool { return v.received == ns })
			}
			reply(0)
			pending := currentProcedureProgress(s)
			if pending.tx == nil || pending.responses != 0 || pending.consumed != 0 || pending.written {
				t.Fatalf("stale response advanced an unwritten request: %+v", pending)
			}
			select {
			case err := <-done:
				t.Fatalf("queued %s completed on stale response: %v", name, err)
			default:
			}
			writeFrame(t, p, Frame{Kind: SFrame, Receive: 1})
			if f := readFrame(t, p); f.Kind != IFrame || f.Send != 1 {
				t.Fatalf("request not transmitted: %+v", f)
			}
			reply(2)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProcedureOverflowDominatesCompletion(t *testing.T) {
	for _, buffered := range []bool{true, false} {
		t.Run(map[bool]string{true: "buffered-CON-TERM", false: "finalization"}[buffered], func(t *testing.T) {
			s := &Session{opts: DefaultOptions(), accepting: true, wake: make(chan struct{}, 1)}
			c := &Client{s}
			req := request(1, 0, C_DC_NA_1, 9, DoubleCommand{Qualifier: 2})
			m, err := Mirror(req, ActivationCon, false)
			if err != nil {
				t.Fatal(err)
			}
			term, err := Mirror(req, ActivationTerm, false)
			if err != nil {
				t.Fatal(err)
			}
			responses, err := c.procedure(testContext(t), req, func(Message) bool { return true }, func(ctx context.Context, tx *transaction) error {
				// Stop the helper consumer until overflow has already been recorded.
				tx.mu.Lock()
				tx.responses = []Message{m, term}
				tx.err = ErrQueueFull
				tx.mu.Unlock()
				if buffered {
					return awaitConfirmation(ctx, tx, true, req)
				}
				return nil // Overflow races the successful wait's finalization.
			})
			if !errors.Is(err, ErrQueueFull) || len(responses) != 2 {
				t.Fatalf("overflow hidden: responses=%d error=%v", len(responses), err)
			}
			if s.procedure != nil || !s.procedureUncertain {
				t.Fatal("transaction not atomically detached and fenced")
			}
		})
	}
}

func TestProcedureSBOEachLegWaitsForWrite(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.K = 1
	s, p, _ := manualSession(t, ClientRole, opts, nil)
	startClientSession(t, s, p)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	readFrame(t, p)
	c := &Client{s}
	req := request(1, 0, C_DC_NA_1, 9, DoubleCommand{Qualifier: 2})
	selected, err := commandSelection(req, true)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	ctx := testContext(t)
	go func() { _, err := c.SelectExecute(ctx, req); done <- err }()
	eventually(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.sends) == 1 })
	writeFrame(t, p, Frame{Kind: IFrame, ASDU: procedureResponse(t, selected, ActivationCon)})
	waitView(t, s, func(v sessionView) bool { return v.received == 1 })
	// A stale select CON must not advance to execute while select is unsent.
	pending := currentProcedureProgress(s)
	if pending.tx == nil || pending.responses != 0 || pending.consumed != 0 || pending.written {
		t.Fatalf("response advanced an unwritten select leg: %+v", pending)
	}
	s.mu.Lock()
	queued := len(s.sends)
	s.mu.Unlock()
	if queued != 1 {
		t.Fatalf("unsent select advanced to execute: %d queued", queued)
	}
	writeFrame(t, p, Frame{Kind: SFrame, Receive: 1})
	readFrame(t, p)
	writeFrame(t, p, Frame{Kind: IFrame, Send: 1, Receive: 1, ASDU: procedureResponse(t, selected, ActivationCon)})
	var before procedureProgress
	eventually(t, func() bool {
		s.mu.Lock()
		queued := len(s.sends)
		s.mu.Unlock()
		before = currentProcedureProgress(s)
		return queued == 1 && before.tx != nil && before.consumed == 1
	})
	// k is still occupied by select. The execute leg must be armed separately.
	writeFrame(t, p, Frame{Kind: IFrame, Send: 2, Receive: 1, ASDU: procedureResponse(t, req, ActivationCon)})
	writeFrame(t, p, Frame{Kind: IFrame, Send: 3, Receive: 1, ASDU: procedureResponse(t, req, ActivationTerm)})
	waitView(t, s, func(v sessionView) bool { return v.received == 4 })
	requireSameProcedureProgress(t, s, before)
	select {
	case err := <-done:
		t.Fatalf("unsent execute completed: %v", err)
	default:
	}
	writeFrame(t, p, Frame{Kind: SFrame, Receive: 2})
	readFrame(t, p)
	writeFrame(t, p, Frame{Kind: IFrame, Send: 4, Receive: 3, ASDU: procedureResponse(t, req, ActivationCon)})
	writeFrame(t, p, Frame{Kind: IFrame, Send: 5, Receive: 3, ASDU: procedureResponse(t, req, ActivationTerm)})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// The peer can receive/respond before net.Conn.Write returns to its caller.
// Keep that race deterministic, without sleeping inside transport supervision.
type procedureWriteGate struct {
	net.Conn
	release chan struct{}
	once    sync.Once
}

func (c *procedureWriteGate) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if b[2]&1 == 0 {
		<-c.release
	}
	return n, err
}
func (c *procedureWriteGate) unblock() { c.once.Do(func() { close(c.release) }) }

func TestProcedureWaitsForCompleteWrite(t *testing.T) {
	a, p := net.Pipe()
	gate := &procedureWriteGate{Conn: a, release: make(chan struct{})}
	opts := DefaultOptions()
	opts.Clock = newFakeClock()
	s, err := NewSession(context.Background(), gate, ClientRole, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gate.unblock(); _ = p.Close(); closeSession(t, s) })
	startClientSession(t, s, p)
	c := &Client{s}
	req := request(1, 0, C_DC_NA_1, 9, DoubleCommand{Qualifier: 2})
	done := make(chan error, 1)
	ctx := testContext(t)
	go func() { _, err := c.Command(ctx, req); done <- err }()
	readFrame(t, p)
	writeFrame(t, p, Frame{Kind: IFrame, Receive: 1, ASDU: procedureResponse(t, req, ActivationCon)})
	writeFrame(t, p, Frame{Kind: IFrame, Send: 1, Receive: 1, ASDU: procedureResponse(t, req, ActivationTerm)})
	waitView(t, s, func(v sessionView) bool { return v.received == 2 })
	progress := currentProcedureProgress(s)
	if progress.tx == nil || progress.responses != 2 || progress.consumed != 0 || progress.written {
		t.Fatalf("responses advanced before Write returned: %+v", progress)
	}
	select {
	case err := <-done:
		t.Fatalf("helper completed before Write returned: %v", err)
	default:
	}
	gate.unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProcedureSBOExecuteQueuedRequestCannotComplete(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.K = 1
	s, p, _ := manualSession(t, ClientRole, opts, nil)
	startClientSession(t, s, p)
	c := &Client{s}
	req := request(1, 0, C_DC_NA_1, 9, DoubleCommand{Qualifier: 2})
	done := make(chan error, 1)
	ctx := testContext(t)
	go func() { _, err := c.SelectExecute(ctx, req); done <- err }()
	f := readFrame(t, p)
	selected, err := DecodeASDU(f.ASDU, opts.Params)
	if err != nil {
		t.Fatal(err)
	}
	// Positive select CON without acknowledging the select I-frame holds k.
	writeFrame(t, p, Frame{Kind: IFrame, ASDU: procedureResponse(t, selected, ActivationCon)})
	var before procedureProgress
	eventually(t, func() bool {
		s.mu.Lock()
		queued := len(s.sends)
		s.mu.Unlock()
		before = currentProcedureProgress(s)
		return queued == 1 && before.tx != nil && before.consumed == 1
	})
	writeFrame(t, p, Frame{Kind: IFrame, Send: 1, ASDU: procedureResponse(t, req, ActivationCon)})
	writeFrame(t, p, Frame{Kind: IFrame, Send: 2, ASDU: procedureResponse(t, req, ActivationTerm)})
	waitView(t, s, func(v sessionView) bool { return v.received == 3 })
	requireSameProcedureProgress(t, s, before)
	select {
	case err := <-done:
		t.Fatalf("queued execute completed on stale responses: %v", err)
	default:
	}
	writeFrame(t, p, Frame{Kind: SFrame, Receive: 1})
	readFrame(t, p)
	writeFrame(t, p, Frame{Kind: IFrame, Send: 3, Receive: 2, ASDU: procedureResponse(t, req, ActivationCon)})
	writeFrame(t, p, Frame{Kind: IFrame, Send: 4, Receive: 2, ASDU: procedureResponse(t, req, ActivationTerm)})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProcedureObserverRecordsOverflow(t *testing.T) {
	opts := DefaultOptions()
	opts.EventQueue = 2
	s := &Session{opts: opts, accepting: true, wake: make(chan struct{}, 1)}
	c := &Client{s}
	req := request(1, 0, C_DC_NA_1, 9, DoubleCommand{Qualifier: 2})
	responses, err := c.procedure(testContext(t), req, func(m Message) bool { return echoed(req, m) }, func(ctx context.Context, tx *transaction) error {
		// Unit-test the observer with a completed writer and a suspended consumer.
		tx.leg.beginWrite()
		tx.leg.endWrite(nil)
		s.observe(procedureResponse(t, req, ActivationCon), tx.leg)
		s.observe(procedureResponse(t, req, ActivationTerm), tx.leg)
		s.observe(procedureResponse(t, req, ActivationTerm), tx.leg)
		return awaitConfirmation(ctx, tx, true, req)
	})
	if !errors.Is(err, ErrQueueFull) || len(responses) != 2 || !s.procedureUncertain || s.procedure != nil {
		t.Fatalf("observer overflow lost: %d responses, %v, fenced=%v", len(responses), err, s.procedureUncertain)
	}
}
