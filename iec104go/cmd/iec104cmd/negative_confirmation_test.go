// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
)

type procedureTestClock struct{ now time.Time }

type procedureTestTimer struct {
	clock *procedureTestClock
	at    time.Time
	ch    chan time.Time
	fired bool
}

func newProcedureTestClock() *procedureTestClock {
	return &procedureTestClock{now: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}
}

func (c *procedureTestClock) Now() time.Time { return c.now }
func (c *procedureTestClock) NewTimer(at time.Time) iec.Timer {
	return &procedureTestTimer{clock: c, at: at, ch: make(chan time.Time, 1)}
}
func (t *procedureTestTimer) C() <-chan time.Time {
	if !t.fired {
		t.clock.now = t.at
		t.ch <- t.at
		t.fired = true
	}
	return t.ch
}
func (t *procedureTestTimer) Stop() bool { return !t.fired }
func (t *procedureTestTimer) Reset(at time.Time) bool {
	wasFired := t.fired
	t.at, t.fired = at, false
	return wasFired
}

type scriptedProcedureLink struct {
	handler  iec.Handler
	cause    iec.COT
	negative bool
}

func (l *scriptedProcedureLink) Send(message iec.Message) error {
	request, ok := message.(*iec.ASDU)
	if !ok {
		return errors.New("expected an ASDU")
	}
	reply := *request
	reply.Objects = append([]iec.Object(nil), request.Objects...)
	reply.Header.Cause = l.cause
	reply.Header.Negative = l.negative
	l.handler(context.Background(), iec.Event{Kind: iec.ASDUReceived, ASDU: &reply})

	term := reply
	term.Header.Cause = iec.ActivationTerm
	term.Header.Negative = false
	l.handler(context.Background(), iec.Event{Kind: iec.ASDUReceived, ASDU: &term})
	return nil
}
func (l *scriptedProcedureLink) StartDT(ctx context.Context) error {
	l.handler(ctx, iec.Event{Kind: iec.Opened})
	l.handler(ctx, iec.Event{Kind: iec.Started})
	return nil
}
func (*scriptedProcedureLink) StopDT(context.Context) error { return nil }
func (*scriptedProcedureLink) Close(context.Context) error  { return nil }

func runWithNegativeConfirmation(t *testing.T, operation string, cause iec.COT, negative bool) (int, string, string, time.Duration) {
	t.Helper()
	out, errout := newCapture(), newCapture()
	rt := testRuntime()
	rt.timeout = 5 * time.Second
	clock := newProcedureTestClock()
	start := clock.Now()
	rt.clock = clock
	rt.dial = func(_ context.Context, _ string, _ iec.Options, handler iec.Handler) (link, error) {
		return &scriptedProcedureLink{handler: handler, cause: cause, negative: negative}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	argv := []string{"127.0.0.1:2404", operation, "--json"}
	if operation == "dc" {
		argv = []string{"127.0.0.1:2404", "dc", "3001", "open", "--json"}
	}
	code := run(ctx, argv, out, errout, rt)
	return code, out.String(), errout.String(), clock.Now().Sub(start)
}

func TestUnknownIOACommandConfirmationFailsBeforeTimeout(t *testing.T) {
	for _, negative := range []bool{false, true} {
		name := "PN=0"
		if negative {
			name = "PN=1"
		}
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr, elapsed := runWithNegativeConfirmation(t, "dc", iec.UnknownIOA, negative)
			if code != 1 || stdout != result("dc", "failed") {
				t.Fatalf("exit=%d stdout=%q", code, stdout)
			}
			if !strings.Contains(stderr, "execute REJECTED (cot=unknown_object_address(47)") || strings.Contains(stderr, "execute confirmed") {
				t.Fatalf("COT 47 was not reported as a rejection: %s", stderr)
			}
			if elapsed >= 5*time.Second {
				t.Fatalf("fake elapsed time %s reached the procedure timeout", elapsed)
			}
		})
	}
}

func TestUnknownCauseGIConfirmationFailsBeforeTimeout(t *testing.T) {
	code, stdout, stderr, elapsed := runWithNegativeConfirmation(t, "gi", iec.UnknownCause, false)
	if code != 1 || stdout != result("gi", "failed") {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
	if !strings.Contains(stderr, "GI confirmation rejected (cot=unknown_cause(45) negative=0)") {
		t.Fatalf("GI failure did not name its COT: %s", stderr)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("fake elapsed time %s reached the procedure timeout", elapsed)
	}
}
