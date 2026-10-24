// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
)

type transportStub struct {
	send func(iec.Message) error
	done chan struct{}
}

func (s *transportStub) Send(m iec.Message) error      { return s.send(m) }
func (s *transportStub) StartDT(context.Context) error { return nil }
func (s *transportStub) Close(context.Context) error   { return nil }
func (s *transportStub) Done() <-chan struct{}         { return s.done }
func TestCommandGoldens(t *testing.T) {
	for _, tc := range []struct{ name, result, reason string }{
		{"ok", "completed", ""}, {"select-negative", "rejected", "select rejected (negative ACT-CON)"}, {"execute-negative", "rejected", "execute rejected (negative ACT-CON)"}, {"term-negative", "failed", "negative ACT-TERM"}, {"missing-feedback", "unknown", "no good matching COT 11 feedback for feeder-breaker-position before ACT-TERM"},
		{"select-send-error", "unknown", "link lost before the select was sent"}, {"execute-send-error", "unknown", "link lost before the execute was sent"}, {"select-timeout", "unknown", "no select confirmation; execute not sent"}, {"execute-timeout", "unknown", "no execute confirmation within the command budget"}, {"term-timeout", "unknown", "no ACT-TERM within the command budget"}, {"select-lost", "unknown", "link lost before the execute was sent"}, {"execute-lost", "unknown", "link lost after the execute was sent"}, {"term-lost", "unknown", "link lost before ACT-TERM"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, now := model(t)
			started(c)
			c.tickGI(*now)
			c.giResponse(10, false)
			receive(c, 3, 20, 1001, iec.DoublePoint{DIQ: 2})
			c.session = &transportStub{send: func(m iec.Message) error {
				a := m.(iec.ASDU)
				v := a.Objects[0].Element.(iec.DoubleCommand)
				sel := v.Qualifier.Select()
				phase := "execute"
				if sel {
					phase = "select"
				}
				if tc.name == phase+"-send-error" {
					return errors.New("injected send failure")
				}
				if tc.name == phase+"-lost" {
					c.mu.Lock()
					c.ended()
					c.mu.Unlock()
					return nil
				}
				if tc.name == phase+"-timeout" {
					*now = now.Add(11 * time.Second)
					return nil
				}
				h := a.Header
				h.Cause = 7
				h.Negative = tc.name == phase+"-negative"
				c.mu.Lock()
				c.commandResponse(h, 3001, sel)
				c.mu.Unlock()
				if sel || h.Negative {
					return nil
				}
				if tc.name != "missing-feedback" {
					receive(c, 3, 11, 1001, iec.DoublePoint{DIQ: 2})
				}
				if tc.name == "term-timeout" {
					*now = now.Add(11 * time.Second)
					return nil
				}
				if tc.name == "term-lost" {
					c.mu.Lock()
					c.ended()
					c.mu.Unlock()
					return nil
				}
				h.Cause = 10
				h.Negative = tc.name == "term-negative"
				c.mu.Lock()
				c.commandResponse(h, 3001, false)
				c.mu.Unlock()
				return nil
			}}
			r := c.command(context.Background(), 0, 2, "close")
			if r.status != 200 {
				t.Fatal(r.status)
			}
			selected := `"` + at + `"`
			executed, con, term, feedback := "null", "null", "null", "null"
			if !strings.HasPrefix(tc.name, "select-") {
				executed = selected
			}
			if tc.name == "ok" || tc.name == "execute-negative" || strings.HasPrefix(tc.name, "term-") || tc.name == "missing-feedback" {
				con = selected
			}
			if tc.name == "ok" || tc.name == "term-negative" || tc.name == "missing-feedback" {
				term = selected
			}
			if tc.name == "ok" || strings.HasPrefix(tc.name, "term-") {
				feedback = `{"ioa":1001,"value":"closed","at":"` + at + `"}`
			}
			reason := "null"
			if tc.reason != "" {
				reason = `"` + tc.reason + `"`
			}
			want := fmt.Sprintf(`{"id":"cmd-1","point":"feeder-breaker-command","operation":"close","result":"%s","reason":%s,"selected_at":%s,"executed_at":%s,"con_at":%s,"term_at":%s,"feedback":%s}`, tc.result, reason, selected, executed, con, term, feedback)
			equal(t, r.body, want)
			equal(t, c.journal[len(c.journal)-1].detail, want)
			if c.tracker.active {
				t.Fatal("tracker retained")
			}
			sends, _ := c.Counters()
			if strings.HasSuffix(tc.name, "send-error") && sends != 1 {
				t.Fatal("send failure uncounted")
			}
		})
	}
}
func TestResponseMatcherAndFeedback(t *testing.T) {
	c, now := model(t)
	started(c)
	receive(c, 3, 20, 1001, iec.DoublePoint{DIQ: 2})
	c.tracker = tracker{active: true, index: 0, seq: 1, phase: "execute", expected: 2, deadline: now.Add(time.Second)}
	for _, h := range []iec.Header{{Type: iec.C_SC_NA_1, Cause: 7, CommonAddress: 1}, {Type: iec.C_DC_NA_1, Cause: 3, CommonAddress: 1}, {Type: iec.C_DC_NA_1, Cause: 7, CommonAddress: 2}} {
		c.handle(context.Background(), iec.Event{Kind: iec.ASDUReceived, ASDU: iec.ASDU{Header: h, Objects: []iec.Object{{Address: 3001, Element: iec.DoubleCommand{Qualifier: 2}}}}})
	}
	if c.tracker.executeCon != 0 {
		t.Fatal("mismatched confirmation")
	}
	c.commandResponse(iec.Header{Type: 46, Cause: 7}, 9999, false)
	if c.tracker.executeCon != 0 {
		t.Fatal("IOA matcher")
	}
	receive(c, 3, 11, 1001, iec.DoublePoint{DIQ: 2})
	if c.tracker.feedback {
		t.Fatal("feedback before execute con")
	}
	c.commandResponse(iec.Header{Type: 46, Cause: 7}, 3001, false)
	for _, r := range []struct {
		cot iec.COT
		ioa int
		v   iec.DIQ
	}{{3, 1001, 2}, {11, 1001, 1}, {11, 1001, 0x82}, {11, 1001, 0x42}, {11, 1001, 0x22}, {11, 1001, 0x12}, {11, 1101, 2}} {
		receive(c, 3, r.cot, r.ioa, iec.DoublePoint{DIQ: r.v})
		if c.tracker.feedback {
			t.Fatalf("bad feedback %+v", r)
		}
	}
	receive(c, 3, 11, 1001, iec.DoublePoint{DIQ: 2})
	if !c.tracker.feedback {
		t.Fatal("good feedback not matched")
	}
	c.tracker.feedback = false
	*now = now.Add(time.Second)
	receive(c, 3, 11, 1001, iec.DoublePoint{DIQ: 2})
	if c.tracker.feedback {
		t.Fatal("deadline accepted")
	}
}
func TestTypedCommandsAndOverlap(t *testing.T) {
	for _, tc := range []struct {
		index int
		op    string
		value int
		typ   iec.TypeID
		ioa   int
		e     iec.Element
	}{{0, "open", 1, 3, 1001, iec.DoublePoint{DIQ: 1}}, {3, "raise", 2, 5, 1301, iec.StepPosition{Value: 4}}, {4, "auto", 1, 1, 1303, iec.SinglePoint{SIQ: 1}}} {
		c, now := model(t)
		started(c)
		c.tickGI(*now)
		c.giResponse(10, false)
		receive(c, tc.typ, 20, tc.ioa, tc.e)
		c.session = &transportStub{send: func(m iec.Message) error {
			if r := c.command(context.Background(), tc.index, tc.value, tc.op); r.status != 409 || string(r.body) != `{"error":"a command is in flight"}` {
				t.Fatal("overlap")
			}
			return errors.New("sent once")
		}}
		if r := c.command(context.Background(), tc.index, tc.value, tc.op); r.status != 200 {
			t.Fatal("typed admission")
		}
	}
}
