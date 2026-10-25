// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"time"

	iec "github.com/tonylturner/iec104go"
)

// Raw Send plus application-owned cursors intentionally leaves a timed-out
// monitor GI retryable on the same link. No library procedure helper is used.
type procedure struct {
	c       link
	r       *responses
	o       *output
	timeout time.Duration
	a       arguments
	clock   iec.Clock
}

func (p *procedure) transfer(ctx context.Context, start bool) bool {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	var err error
	ch := p.r.stopped
	if start {
		ch, err = p.r.started, p.c.StartDT(ctx)
	} else {
		err = p.c.StopDT(ctx)
	}
	if err != nil {
		return false
	}
	// Drain the ordered lifecycle callback before printing the caller's log.
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}

func (p *procedure) send(request iec.ASDU) bool {
	err := p.c.Send(&request)
	if err != nil {
		p.o.logf("send failed: %v", err)
	}
	return err == nil
}

func (p *procedure) wait(ctx context.Context, cursor *uint64, expected iec.ASDU, term bool) (response, bool) {
	return p.r.wait(ctx, cursor, expected, term, p.timeout, p.clock)
}

func (p *procedure) gi(ctx context.Context, monitor bool) bool {
	request := iec.ASDU{
		Header:  iec.Header{Type: iec.C_IC_NA_1, Cause: iec.Activation, Originator: uint8(p.a.oa), CommonAddress: uint16(p.a.ca)},
		Objects: []iec.Object{{Address: 0, Element: iec.Interrogation{QOI: 20}}},
	}
	cursor := p.r.cursor()
	if !p.send(request) {
		return false
	}
	if !monitor {
		r, ok := p.wait(ctx, &cursor, request, false)
		if !ok {
			return false
		}
		if r.header.Cause != iec.ActivationCon || r.header.Negative {
			p.o.logf("GI confirmation rejected (cot=%s(%d) negative=%d)", r.header.Cause, r.header.Cause, bit(r.header.Negative))
			return false
		}
	}
	// The legacy CLI checks the presence of GI ACT-TERM, not its PN flag.
	// Monitor also deliberately does not require ACT-CON.
	_, ok := p.wait(ctx, &cursor, request, true)
	return ok
}

func (p *procedure) command(ctx context.Context, ioa uint32, value int) bool {
	typ := map[string]iec.TypeID{"dc": iec.C_DC_NA_1, "rc": iec.C_RC_NA_1, "sc": iec.C_SC_NA_1}[p.a.words[0]]
	request := func(value int, selectCommand bool) iec.ASDU {
		q := iec.CommandQualifier(value)
		if selectCommand {
			q |= 128
		}
		var e iec.Element
		switch typ {
		case iec.C_DC_NA_1:
			e = iec.DoubleCommand{Qualifier: q}
		case iec.C_RC_NA_1:
			e = iec.RegulatingCommand{Qualifier: q}
		case iec.C_SC_NA_1:
			e = iec.SingleCommand{Qualifier: q}
		}
		return iec.ASDU{
			Header:  iec.Header{Type: typ, Cause: iec.Activation, Originator: uint8(p.a.oa), CommonAddress: uint16(p.a.ca)},
			Objects: []iec.Object{{Address: ioa, Element: e}},
		}
	}
	confirm := func(cursor *uint64, expected iec.ASDU, leg string) bool {
		r, ok := p.wait(ctx, cursor, expected, false)
		if !ok {
			p.o.logf("no %s confirmation", leg)
			return false
		}
		if r.header.Cause == iec.ActivationCon && r.header.Negative {
			// Legacy CLI wording; services/iec104/check/run.sh matches it.
			p.o.logf("%s REJECTED (negative)", leg)
			return false
		}
		if r.header.Cause != iec.ActivationCon {
			p.o.logf("%s REJECTED (cot=%s(%d) negative=%d)", leg, r.header.Cause, r.header.Cause, bit(r.header.Negative))
			return false
		}
		p.o.logf("%s confirmed", leg)
		return true
	}
	if p.a.sbo {
		cursor := p.r.cursor()
		selected := request(value, true)
		p.o.logf("SELECT ioa=%d val=%d", ioa, value)
		if !p.send(selected) || !confirm(&cursor, selected, "select") {
			return false
		}
	}
	if p.a.delayMS > 0 {
		p.o.logf("waiting %dms before execute", p.a.delayMS)
		if !sleep(ctx, p.clock, time.Duration(p.a.delayMS)*time.Millisecond) {
			return false
		}
	}
	if p.a.mismatch {
		if typ == iec.C_SC_NA_1 {
			value = 1 - value
		} else {
			value = 3 - value
		}
	}
	cursor := p.r.cursor()
	executed := request(value, false)
	p.o.logf("EXECUTE ioa=%d val=%d", ioa, value)
	if !p.send(executed) || !confirm(&cursor, executed, "execute") {
		return false
	}
	r, ok := p.wait(ctx, &cursor, executed, true)
	if !ok {
		p.o.logf("no execute termination (ACT-TERM)")
		return false
	}
	p.o.logf("execute terminated (ACT-TERM) negative=%d", bit(r.header.Negative))
	return !r.header.Negative
}

func sleep(ctx context.Context, clock iec.Clock, d time.Duration) bool {
	timer := clock.NewTimer(clock.Now().Add(d))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C():
		return true
	}
}

func (p *procedure) monitor(ctx context.Context) int {
	if p.a.json {
		p.o.line(`{"event":"connected","at":"%s","local":null,"peer":"%s:%d"}`, p.o.stamp(), p.a.host, p.a.port)
	}
	p.o.logf("send station GI")
	if p.gi(ctx, true) && p.a.json {
		p.o.event("gi_complete")
	}
	// The C hold begins AFTER the initial GI wait. Periodic waits also count
	// toward the hold, including a final wait that may overrun its deadline.
	start := p.clock.Now()
	lastGI := start
	for p.clock.Now().Sub(start) < time.Duration(p.a.hold)*time.Second {
		if p.r.isClosed() || ctx.Err() != nil {
			if p.a.json {
				p.o.line(`{"event":"link_down","at":"%s","reason":"peer_closed"}`, p.o.stamp())
			}
			p.o.logf("link down during monitor hold")
			return 3
		}
		now := p.clock.Now()
		if p.a.giEvery > 0 && now.Sub(lastGI) >= time.Duration(p.a.giEvery)*time.Second {
			lastGI = now
			if p.gi(ctx, true) && p.a.json {
				p.o.event("gi_complete")
			}
		}
		if !sleep(ctx, p.clock, 50*time.Millisecond) {
			continue
		}
	}
	if p.a.json {
		p.o.event("held")
	}
	p.o.logf("hold complete")
	return 0
}
