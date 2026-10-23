// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"fmt"
)

// Interrogate waits for ACT-CON, station/group image and ACT-TERM. It returns
// every matching response observed, including negative responses on failure.
func (c *Client) Interrogate(ctx context.Context, ca uint16, oa, qoi uint8) ([]Message, error) {
	if qoi < 20 || qoi > 36 {
		return nil, errFieldRange
	}
	req := request(ca, oa, C_IC_NA_1, 0, Interrogation{QOI: qoi})
	match := func(m Message) bool {
		return envelopeMatch(req, m) && (echoed(req, m) || m.GetHeader().Cause == COT(qoi))
	}
	return c.procedure(ctx, req, match, func(ctx context.Context, t *transaction) error { return awaitConfirmation(ctx, t, true, req) })
}

// CounterInterrogate retains QCC's request/freeze bits and collects counter
// responses (COT37..41), preceded by ACT-CON and completed by ACT-TERM.
func (c *Client) CounterInterrogate(ctx context.Context, ca uint16, oa, qcc uint8) ([]Message, error) {
	req := request(ca, oa, C_CI_NA_1, 0, CounterInterrogation{QCC: qcc})
	match := func(m Message) bool {
		h := m.GetHeader()
		return envelopeMatch(req, m) && (echoed(req, m) || h.Cause >= 37 && h.Cause <= 41)
	}
	return c.procedure(ctx, req, match, func(ctx context.Context, t *transaction) error { return awaitConfirmation(ctx, t, true, req) })
}

// Read waits for requested-address data with COT Request, not ACT-CON/ACT-TERM:
// those activation confirmations are not part of the standard read exchange.
func (c *Client) Read(ctx context.Context, ca uint16, oa uint8, address uint32) ([]Message, error) {
	req := request(ca, oa, C_RD_NA_1, address, ReadCommand{})
	req.Header.Cause = Request
	match := func(m Message) bool {
		if !envelopeMatch(req, m) {
			return false
		}
		if echoed(req, m) {
			return true
		}
		a, ok := m.(*ASDU)
		if !ok || a.Header.Cause != Request {
			return false
		}
		for _, o := range a.Objects {
			if o.Address == address {
				return true
			}
		}
		return false
	}
	return c.procedure(ctx, req, match, func(ctx context.Context, t *transaction) error {
		m, err := t.next(ctx)
		if err != nil {
			return err
		}
		if err := responseError(m); err != nil {
			return err
		}
		if m.GetHeader().Cause != Request || m.GetHeader().Type == C_RD_NA_1 {
			return ErrProcedureOrder
		}
		return nil
	})
}

// SyncClock sends a CP56 clock synchronization and waits for ACT-CON only.
func (c *Client) SyncClock(ctx context.Context, ca uint16, oa uint8, tag CP56Time2a) ([]Message, error) {
	return c.confirmOnly(ctx, request(ca, oa, C_CS_NA_1, 0, ClockSync{Time: tag}))
}

// Test sends type107 with the supplied sequence/time and waits for ACT-CON only.
func (c *Client) Test(ctx context.Context, ca uint16, oa uint8, sequence uint16, tag CP56Time2a) ([]Message, error) {
	return c.confirmOnly(ctx, request(ca, oa, C_TS_TA_1, 0, TestCommand{Sequence: sequence, Time: tag}))
}

// Reset sends type105 with raw QRP and waits for ACT-CON only.
func (c *Client) Reset(ctx context.Context, ca uint16, oa, qrp uint8) ([]Message, error) {
	return c.confirmOnly(ctx, request(ca, oa, C_RP_NA_1, 0, ResetProcess{QRP: qrp}))
}
func (c *Client) confirmOnly(ctx context.Context, req ASDU) ([]Message, error) {
	return c.procedure(ctx, req, func(m Message) bool { return echoed(req, m) }, func(ctx context.Context, t *transaction) error { return awaitConfirmation(ctx, t, false, req) })
}

// Command sends a direct execute and waits for ACT-CON then ACT-TERM. This
// profile requires termination even for optional-termination command variants.
func (c *Client) Command(ctx context.Context, req ASDU) ([]Message, error) {
	req, err := commandSelection(req, false)
	if err != nil {
		return nil, err
	}
	return c.procedure(ctx, req, func(m Message) bool { return echoed(req, m) }, func(ctx context.Context, t *transaction) error { return awaitConfirmation(ctx, t, true, req) })
}

// SelectExecute performs select/ACT-CON then execute/ACT-CON/ACT-TERM under
// one transaction and caller deadline. It never retries an ambiguous command.
func (c *Client) SelectExecute(ctx context.Context, req ASDU) ([]Message, error) {
	selectReq, err := commandSelection(req, true)
	if err != nil {
		return nil, err
	}
	executeReq, err := commandSelection(req, false)
	if err != nil {
		return nil, err
	}
	return c.procedure(ctx, selectReq, func(m Message) bool { return echoed(selectReq, m) || echoed(executeReq, m) }, func(ctx context.Context, t *transaction) error {
		if err := awaitConfirmation(ctx, t, false, selectReq); err != nil {
			return err
		}
		if err := c.Send(executeReq); err != nil {
			return err
		}
		return awaitConfirmation(ctx, t, true, executeReq)
	})
}
func commandSelection(req ASDU, selecting bool) (ASDU, error) {
	if len(req.Objects) != 1 {
		return ASDU{}, fmt.Errorf("%w: command requires one object", ErrMalformed)
	}
	e, err := setSelection(req.Objects[0].Element, selecting)
	if err != nil {
		return ASDU{}, err
	}
	req.Objects = append([]Object(nil), req.Objects...)
	req.Objects[0].Element = e
	req.Header.Cause = Activation
	req.Header.Negative = false
	return req, nil
}
func setSelection(e Element, selecting bool) (Element, error) {
	bit := uint8(0)
	if selecting {
		bit = 128
	}
	switch v := e.(type) {
	case SingleCommand:
		v.Qualifier = CommandQualifier(uint8(v.Qualifier)&127 | bit)
		return v, nil
	case DoubleCommand:
		v.Qualifier = CommandQualifier(uint8(v.Qualifier)&127 | bit)
		return v, nil
	case RegulatingCommand:
		v.Qualifier = CommandQualifier(uint8(v.Qualifier)&127 | bit)
		return v, nil
	case SetpointNormalized:
		v.Qualifier = SetpointQualifier(uint8(v.Qualifier)&127 | bit)
		return v, nil
	case SetpointScaled:
		v.Qualifier = SetpointQualifier(uint8(v.Qualifier)&127 | bit)
		return v, nil
	case SetpointFloat:
		v.Qualifier = SetpointQualifier(uint8(v.Qualifier)&127 | bit)
		return v, nil
	case BitstringCommand:
		if !selecting {
			return v, nil
		}
	case Timed:
		if _, nested := v.Value.(Timed); nested {
			break
		}
		inner, err := setSelection(v.Value, selecting)
		if err != nil {
			return nil, err
		}
		v.Value = inner
		return v, nil
	}
	return nil, fmt.Errorf("%w: element has no select/execute qualifier", ErrMalformed)
}
