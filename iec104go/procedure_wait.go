// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrProcedureBusy prevents ambiguous overlapping transactions on one link.
	ErrProcedureBusy = errors.New("iec104go: procedure already in progress")
	// ErrProcedureUncertain requires a fresh connection after an abandoned or
	// unordered exchange: IEC 104 has no transaction ID to distinguish late echoes.
	ErrProcedureUncertain = errors.New("iec104go: procedure outcome uncertain; reconnect before another helper")
	// ErrNegativeResponse means the peer returned negative or COT44..47.
	ErrNegativeResponse = errors.New("iec104go: negative procedure response")
	// ErrProcedureOrder means a procedure response violated confirmation ordering.
	ErrProcedureOrder = errors.New("iec104go: unexpected procedure response order")
)

type transaction struct {
	mu        sync.Mutex
	match     func(Message) bool
	responses []Message
	consumed  int
	wake      chan struct{}
	err       error
	limit     int
	streaming bool
	leg       *procedureLeg
}

func (t *transaction) fail(err error) {
	t.mu.Lock()
	if t.err == nil {
		t.err = err
	}
	t.mu.Unlock()
	signal(t.wake)
}
func (s *Session) observe(payload []byte, leg *procedureLeg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.procedure
	if t == nil {
		return
	}
	m, err := DecodeASDU(payload, s.opts.Params)
	if err != nil {
		t.fail(err)
		return
	}
	t.mu.Lock()
	if leg == nil || leg != t.leg || !t.match(m) {
		t.mu.Unlock()
		return
	}
	if len(t.responses) >= t.limit {
		t.err = ErrQueueFull
	} else {
		t.responses = append(t.responses, m)
	}
	t.mu.Unlock()
	signal(t.wake)
}
func (s *Session) begin(match func(Message) bool) (*transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gone {
		return nil, ErrConnectionGone
	}
	if s.procedure != nil {
		return nil, ErrProcedureBusy
	}
	if s.procedureUncertain {
		return nil, ErrProcedureUncertain
	}
	t := &transaction{match: match, wake: make(chan struct{}, 1), limit: s.opts.EventQueue}
	s.procedure = t
	return t, nil
}
func (s *Session) complete(t *transaction, err error, admitted bool) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()
	// Observation and finalization use the same lock order: an overflow recorded
	// before detach must dominate a buffered success, with no unfenced gap.
	if t.err != nil {
		err = t.err
	}
	if t.streaming && err == nil && len(t.responses) != 0 {
		err = ErrProcedureOrder
	}
	if s.procedure == t {
		s.procedure = nil
	}
	if admitted && (t.streaming && err != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrQueueFull) || errors.Is(err, ErrProcedureOrder)) {
		s.procedureUncertain = true
	}
	return append([]Message(nil), t.responses...), err
}
func (t *transaction) next(ctx context.Context) (Message, error) {
	for {
		t.mu.Lock()
		err := t.err
		if err != nil {
			t.mu.Unlock()
			return nil, err
		}
		if t.leg != nil && t.leg.written && t.consumed < len(t.responses) {
			m := t.responses[t.consumed]
			if t.streaming {
				t.responses[0] = nil
				t.responses = t.responses[1:]
			} else {
				t.consumed++
			}
			t.mu.Unlock()
			return m, nil
		}
		t.mu.Unlock()
		select {
		case <-t.wake:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
func envelopeMatch(request ASDU, m Message) bool {
	h := m.GetHeader()
	want := request.GetHeader()
	return h.CommonAddress == want.CommonAddress && h.Originator == want.Originator && h.Test == want.Test
}
func echoed(request ASDU, m Message) bool {
	if !envelopeMatch(request, m) || m.GetHeader().Type != request.Header.Type {
		return false
	}
	a, ok := m.(*ASDU)
	if !ok || len(a.Objects) != len(request.Objects) {
		return false
	}
	for i, o := range a.Objects {
		if o.Address != request.Objects[i].Address {
			return false
		}
		_, got, err := encodeElement(o.Element)
		if err != nil {
			return false
		}
		_, want, err := encodeElement(request.Objects[i].Element)
		if err != nil || !bytes.Equal(got, want) {
			return false
		}
	}
	return true
}
func responseError(m Message) error {
	h := m.GetHeader()
	if h.Negative || h.Cause >= UnknownType && h.Cause <= UnknownIOA {
		return fmt.Errorf("%w: %s", ErrNegativeResponse, h.Cause)
	}
	return nil
}
func awaitConfirmation(ctx context.Context, t *transaction, termination bool, expected ASDU) error {
	confirmed := false
	for {
		m, err := t.next(ctx)
		if err != nil {
			return err
		}
		if err := responseError(m); err != nil {
			return err
		}
		switch m.GetHeader().Cause {
		case ActivationCon:
			if !echoed(expected, m) {
				return ErrProcedureOrder
			}
			if confirmed {
				return ErrProcedureOrder
			}
			confirmed = true
			if !termination {
				return nil
			}
		case ActivationTerm:
			if !confirmed || !echoed(expected, m) {
				return ErrProcedureOrder
			}
			return nil
		default:
			if !confirmed || m.GetHeader().Type == expected.Header.Type {
				return ErrProcedureOrder
			}
		}
	}
}
func (c *Client) procedure(ctx context.Context, request ASDU, match func(Message) bool, wait func(context.Context, *transaction) error) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, fmt.Errorf("%w: procedure context requires a deadline", ErrMalformed)
	}
	t, err := c.begin(match)
	if err != nil {
		return nil, err
	}
	if err := c.sendProcedure(t, request, match); err != nil {
		return c.complete(t, err, false)
	}
	err = wait(ctx, t)
	return c.complete(t, err, true)
}
func request(ca uint16, oa uint8, id TypeID, address uint32, e Element) ASDU {
	return ASDU{Header: Header{Type: id, Cause: Activation, Originator: oa, CommonAddress: ca}, Objects: []Object{{Address: address, Element: e}}}
}
