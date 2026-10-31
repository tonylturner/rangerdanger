// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"errors"
	"fmt"
	"math"
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
			completeGI(c)
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
				c.commandResponse(h, 3001, v.Qualifier)
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
				c.commandResponse(h, 3001, v.Qualifier)
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

type immediateCommandClock struct{ now time.Time }

type immediateCommandTimer struct {
	clock *immediateCommandClock
	at    time.Time
	ch    chan time.Time
	fired bool
}

func (c *immediateCommandClock) Now() time.Time { return c.now }
func (c *immediateCommandClock) NewTimer(at time.Time) iec.Timer {
	return &immediateCommandTimer{clock: c, at: at, ch: make(chan time.Time, 1)}
}
func (t *immediateCommandTimer) C() <-chan time.Time {
	if !t.fired {
		t.clock.now = t.at
		t.ch <- t.at
		t.fired = true
	}
	return t.ch
}
func (t *immediateCommandTimer) Stop() bool { return !t.fired }
func (t *immediateCommandTimer) Reset(at time.Time) bool {
	wasFired := t.fired
	t.at, t.fired = at, false
	return wasFired
}

func TestMirroredUnknownIOARejectsCommandBeforeT1(t *testing.T) {
	for _, phase := range []string{"select", "execute"} {
		cases := []struct {
			cause    iec.COT
			negative bool
			reason   string
		}{{iec.ActivationCon, true, phase + " rejected (negative ACT-CON)"}}
		for _, unknown := range []struct {
			cause iec.COT
			name  string
		}{{iec.UnknownType, "unknown type"}, {iec.UnknownCause, "unknown cause"}, {iec.UnknownCA, "unknown common address"}, {iec.UnknownIOA, "unknown IOA"}} {
			for _, negative := range []bool{false, true} {
				cases = append(cases, struct {
					cause    iec.COT
					negative bool
					reason   string
				}{unknown.cause, negative, fmt.Sprintf("%s rejected (%s, COT %d)", phase, unknown.name, unknown.cause)})
			}
		}
		for _, tc := range cases {
			name := fmt.Sprintf("%s/COT=%d/PN=%d", phase, tc.cause, map[bool]int{true: 1, false: 0}[tc.negative])
			t.Run(name, func(t *testing.T) {
				c, now := model(t)
				clock := &immediateCommandClock{now: *now}
				c.opts.Now = clock.Now
				c.opts.Clock = clock
				started(c)
				c.tickGI(clock.Now())
				completeGI(c)
				receive(c, 3, 20, 1001, iec.DoublePoint{DIQ: 2})
				c.session = &transportStub{send: func(m iec.Message) error {
					a := m.(iec.ASDU)
					command := a.Objects[0].Element.(iec.DoubleCommand)
					reply := a.Header
					if phase == "select" && command.Qualifier.Select() || phase == "execute" && !command.Qualifier.Select() {
						reply.Cause = tc.cause
						reply.Negative = tc.negative
					} else {
						reply.Cause = iec.ActivationCon
					}
					c.mu.Lock()
					c.commandResponse(reply, int(a.Objects[0].Address), command.Qualifier)
					c.mu.Unlock()
					return nil
				}}

				result := c.command(context.Background(), 0, 2, "close")
				if result.status != 200 || !strings.Contains(string(result.body), `"result":"rejected"`) || !strings.Contains(string(result.body), `"reason":"`+tc.reason+`"`) {
					t.Fatalf("COT %d reason mismatch: %s", tc.cause, result.body)
				}
				elapsed := clock.Now().Sub(*now)
				if elapsed >= time.Duration(c.pm.Timers.T1)*time.Second {
					t.Fatalf("rejection arrived at fake elapsed %s, not before T1", elapsed)
				}
			})
		}
	}
}

func TestResponseMatcherAndFeedback(t *testing.T) {
	c, now := model(t)
	started(c)
	receive(c, 3, 20, 1001, iec.DoublePoint{DIQ: 2})
	c.tracker = tracker{active: true, index: 0, seq: 1, phase: "execute", expected: 2, value: 2, deadline: now.Add(time.Second)}
	for _, h := range []iec.Header{{Type: iec.C_SC_NA_1, Cause: 7, CommonAddress: 1}, {Type: iec.C_DC_NA_1, Cause: 3, CommonAddress: 1}, {Type: iec.C_DC_NA_1, Cause: 7, CommonAddress: 2}} {
		c.handle(context.Background(), iec.Event{Kind: iec.ASDUReceived, ASDU: iec.ASDU{Header: h, Objects: []iec.Object{{Address: 3001, Element: iec.DoubleCommand{Qualifier: 2}}}}})
	}
	if c.tracker.executeCon != 0 {
		t.Fatal("mismatched confirmation")
	}
	c.commandResponse(iec.Header{Type: 46, Cause: 7, CommonAddress: 1}, 9999, 2)
	if c.tracker.executeCon != 0 {
		t.Fatal("IOA matcher")
	}
	receive(c, 3, 11, 1001, iec.DoublePoint{DIQ: 2})
	if c.tracker.feedback {
		t.Fatal("feedback before execute con")
	}
	c.commandResponse(iec.Header{Type: 46, Cause: 7, CommonAddress: 1}, 3001, 2)
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

func TestCommandRepliesRequireExactPhaseAndQualifier(t *testing.T) {
	for _, term := range []bool{false, true} {
		phase := "select"
		cause := iec.ActivationCon
		qualifier := iec.CommandQualifier(0x82)
		if term {
			phase = "execute"
			cause = iec.ActivationTerm
			qualifier = 2
		}
		cases := []struct {
			name string
			h    iec.Header
			ioa  int
			q    iec.CommandQualifier
		}{
			{"wrong type", iec.Header{Type: iec.C_SC_NA_1, Cause: cause, CommonAddress: 1}, 3001, qualifier},
			{"wrong CA", iec.Header{Type: iec.C_DC_NA_1, Cause: cause, CommonAddress: 2}, 3001, qualifier},
			{"wrong OA", iec.Header{Type: iec.C_DC_NA_1, Cause: cause, CommonAddress: 1, Originator: 1}, 3001, qualifier},
			{"wrong IOA", iec.Header{Type: iec.C_DC_NA_1, Cause: cause, CommonAddress: 1}, 3999, qualifier},
			{"wrong command value", iec.Header{Type: iec.C_DC_NA_1, Cause: cause, CommonAddress: 1}, 3001, iec.CommandQualifier(uint8(qualifier) ^ 1)},
			{"wrong QU", iec.Header{Type: iec.C_DC_NA_1, Cause: cause, CommonAddress: 1}, 3001, qualifier | 4},
			{"wrong select bit", iec.Header{Type: iec.C_DC_NA_1, Cause: cause, CommonAddress: 1}, 3001, qualifier ^ 0x80},
			{"deactivation confirmation", iec.Header{Type: iec.C_DC_NA_1, Cause: iec.DeactivationCon, CommonAddress: 1}, 3001, qualifier},
		}
		t.Run(phase, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					c, now := model(t)
					executeCon := 0
					if term && tc.h.Cause == iec.ActivationTerm {
						executeCon = 1
					}
					c.tracker = tracker{active: true, index: 0, value: 2, phase: phase, executeCon: executeCon, deadline: now.Add(time.Second)}
					receiveHeader(c, tc.h, tc.ioa, iec.DoubleCommand{Qualifier: tc.q})
					if c.tracker.selectCon != 0 || c.tracker.executeCon != executeCon || c.tracker.terminated {
						t.Fatalf("mismatched reply accepted: %+v", c.tracker)
					}
				})
			}
			t.Run("test-marked", func(t *testing.T) {
				c, now := model(t)
				executeCon := 0
				if term {
					executeCon = 1
				}
				c.tracker = tracker{active: true, index: 0, value: 2, phase: phase, executeCon: executeCon, deadline: now.Add(time.Second)}
				h := iec.Header{Type: iec.C_DC_NA_1, Cause: cause, CommonAddress: 1, Test: true}
				receiveHeader(c, h, 3001, iec.DoubleCommand{Qualifier: qualifier})
				if c.tracker.selectCon != 0 || c.tracker.executeCon != executeCon || c.tracker.terminated {
					t.Fatalf("test reply accepted: %+v", c.tracker)
				}
			})
			t.Run("matching positive", func(t *testing.T) {
				c, now := model(t)
				executeCon := 0
				if term {
					executeCon = 1
				}
				c.tracker = tracker{active: true, index: 0, value: 2, phase: phase, executeCon: executeCon, deadline: now.Add(time.Second)}
				h := iec.Header{Type: iec.C_DC_NA_1, Cause: cause, CommonAddress: 1}
				receiveHeader(c, h, 3001, iec.DoubleCommand{Qualifier: qualifier})
				if term {
					if !c.tracker.terminated || c.tracker.negative {
						t.Fatalf("matching ACT-TERM not recorded: %+v", c.tracker)
					}
				} else if c.tracker.selectCon != 1 {
					t.Fatalf("matching ACT-CON not recorded: %+v", c.tracker)
				}
			})
			if !term {
				t.Run("negative ACT-CON", func(t *testing.T) {
					c, now := model(t)
					c.tracker = tracker{active: true, index: 0, value: 2, phase: phase, deadline: now.Add(time.Second)}
					h := iec.Header{Type: iec.C_DC_NA_1, Cause: iec.ActivationCon, CommonAddress: 1, Negative: true}
					receiveHeader(c, h, 3001, iec.DoubleCommand{Qualifier: qualifier})
					if c.tracker.selectCon != -1 {
						t.Fatalf("negative ACT-CON not recorded as rejection: %+v", c.tracker)
					}
				})
			}
		})
	}
}

func TestUnknownCauseCommandRepliesAreRejections(t *testing.T) {
	for _, phase := range []string{"select", "execute"} {
		for _, cot := range []iec.COT{iec.UnknownType, iec.UnknownCause, iec.UnknownCA, iec.UnknownIOA} {
			for _, negative := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/cot=%d/PN=%d", phase, cot, map[bool]int{true: 1, false: 0}[negative]), func(t *testing.T) {
					c, now := model(t)
					qualifier := iec.CommandQualifier(2)
					if phase == "select" {
						qualifier |= 0x80
					}
					c.tracker = tracker{active: true, index: 0, value: 2, phase: phase, deadline: now.Add(time.Second)}
					h := iec.Header{Type: iec.C_DC_NA_1, Cause: cot, CommonAddress: 1, Negative: negative}
					receiveHeader(c, h, 3001, iec.DoubleCommand{Qualifier: qualifier})
					if phase == "select" && c.tracker.selectCon != -1 || phase == "execute" && c.tracker.executeCon != -1 {
						t.Fatalf("COT %d did not reject phase %s (P/N=%t): %+v", cot, phase, negative, c.tracker)
					}
				})
			}
		}
	}
}

func TestTestAndNegativeReportsCannotPromoteGoodFeedback(t *testing.T) {
	c, now := model(t)
	receive(c, iec.M_DP_NA_1, iec.InterrogatedStation, 1001, iec.DoublePoint{DIQ: 2})
	c.tracker = tracker{active: true, index: 0, phase: "execute", executeCon: 1, expected: 1, deadline: now.Add(time.Second)}
	for _, h := range []iec.Header{
		{Type: iec.M_DP_NA_1, Cause: iec.Spontaneous, CommonAddress: 1, Test: true},
		{Type: iec.M_DP_NA_1, Cause: iec.Spontaneous, CommonAddress: 1, Negative: true},
		{Type: iec.M_DP_NA_1, Cause: iec.ReturnRemote, CommonAddress: 1, Test: true},
	} {
		receiveHeader(c, h, 1001, iec.DoublePoint{DIQ: 1})
		if c.tracker.feedback {
			t.Fatalf("test report satisfied feedback: %+v", c.tracker)
		}
		p := c.points[0]
		if p.value != "closed" || p.q != 0 || p.stale != "" {
			t.Fatalf("invalid report changed process image: %+v", p)
		}
	}
}

func TestNonFiniteMeasurementsAreInvalid(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value float32
	}{{"NaN", float32(math.NaN())}, {"positive infinity", float32(math.Inf(1))}, {"negative infinity", float32(math.Inf(-1))}} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := model(t)
			receive(c, iec.M_ME_NC_1, iec.Spontaneous, 2001, iec.Float{Value: tc.value})
			p := c.points[2]
			if p.value != nil || p.q&iec.Invalid == 0 || p.stale != "measurement_invalid" {
				t.Fatalf("non-finite value quality: %+v", p)
			}
		})
	}
}

func TestGICompletionRequiresPositiveMatchingStationActivation(t *testing.T) {
	for _, tc := range []struct {
		name string
		qoi  uint8
	}{{"no confirmation", 20}, {"wrong group without confirmation", 21}} {
		t.Run(tc.name, func(t *testing.T) {
			c, now := model(t)
			started(c)
			c.tickGI(*now)
			receiveHeader(c, iec.Header{Type: iec.C_IC_NA_1, Cause: iec.ActivationTerm, CommonAddress: 1}, 0, iec.Interrogation{QOI: tc.qoi})
			if c.link.gi == "complete" || c.link.completed != "" {
				t.Fatalf("GI completed without matching ACT-CON: %+v", c.link)
			}
		})
	}
	t.Run("valid station sequence", func(t *testing.T) {
		c, now := model(t)
		started(c)
		c.tickGI(*now)
		receiveHeader(c, iec.Header{Type: iec.C_IC_NA_1, Cause: iec.ActivationCon, CommonAddress: 1}, 0, iec.Interrogation{QOI: 20})
		receiveHeader(c, iec.Header{Type: iec.C_IC_NA_1, Cause: iec.ActivationTerm, CommonAddress: 1}, 0, iec.Interrogation{QOI: 20})
		if c.link.gi != "complete" || c.link.completed == "" {
			t.Fatalf("valid GI sequence not completed: %+v", c.link)
		}
	})
}

func TestTypedCommandsAndOverlap(t *testing.T) {
	for _, tc := range []struct {
		index        int
		op           string
		value        int
		commandType  iec.TypeID
		commandIOA   int
		feedbackType iec.TypeID
		feedbackIOA  int
		initial      iec.Element
	}{{0, "close", 2, iec.C_DC_NA_1, 3001, iec.M_DP_NA_1, 1001, iec.DoublePoint{DIQ: 1}}, {3, "raise", 2, iec.C_RC_NA_1, 3301, iec.M_ST_NA_1, 1301, iec.StepPosition{Value: 4}}, {4, "auto", 1, iec.C_SC_NA_1, 3302, iec.M_SP_NA_1, 1303, iec.SinglePoint{SIQ: 0}}} {
		c, now := model(t)
		started(c)
		c.tickGI(*now)
		completeGI(c)
		receive(c, tc.feedbackType, iec.InterrogatedStation, tc.feedbackIOA, tc.initial)
		var sent []iec.ASDU
		c.session = &transportStub{send: func(m iec.Message) error {
			a := m.(iec.ASDU)
			sent = append(sent, a)
			if a.Header.Type != tc.commandType || a.Header.Cause != iec.Activation || a.Header.Originator != 0 || a.Header.CommonAddress != 1 || len(a.Objects) != 1 || int(a.Objects[0].Address) != tc.commandIOA {
				t.Fatalf("wrong typed command envelope: %+v", a)
			}
			selecting := len(sent) == 1
			want := iec.CommandQualifier(tc.value)
			if selecting {
				want |= 0x80
			}
			got := commandQualifier(t, a.Objects[0].Element)
			if got != want {
				t.Fatalf("command qualifier=%d want=%d", got, want)
			}
			if r := c.command(context.Background(), tc.index, tc.value, tc.op); r.status != 409 || string(r.body) != `{"error":"a command is in flight"}` {
				t.Fatal("overlap")
			}
			h := a.Header
			h.Cause = iec.ActivationCon
			receiveHeader(c, h, tc.commandIOA, a.Objects[0].Element)
			if !selecting {
				c.mu.Lock()
				expected := c.tracker.expected
				c.mu.Unlock()
				var feedback iec.Element
				switch tc.feedbackType {
				case iec.M_DP_NA_1:
					feedback = iec.DoublePoint{DIQ: iec.DIQ(expected)}
				case iec.M_ST_NA_1:
					feedback = iec.StepPosition{Value: iec.VTI(expected)}
				case iec.M_SP_NA_1:
					feedback = iec.SinglePoint{SIQ: iec.SIQ(expected)}
				}
				receive(c, tc.feedbackType, iec.ReturnRemote, tc.feedbackIOA, feedback)
				h.Cause = iec.ActivationTerm
				receiveHeader(c, h, tc.commandIOA, a.Objects[0].Element)
			}
			return nil
		}}
		r := c.command(context.Background(), tc.index, tc.value, tc.op)
		if r.status != 200 || !strings.Contains(string(r.body), `"result":"completed"`) || !strings.Contains(string(r.body), `"feedback":{`) || len(sent) != 2 {
			t.Fatalf("typed command not completed: %s sends=%d", r.body, len(sent))
		}
	}
}

func commandQualifier(t *testing.T, e iec.Element) iec.CommandQualifier {
	t.Helper()
	switch v := e.(type) {
	case iec.DoubleCommand:
		return v.Qualifier
	case iec.RegulatingCommand:
		return v.Qualifier
	case iec.SingleCommand:
		return v.Qualifier
	default:
		t.Fatalf("not a command element: %T", e)
		return 0
	}
}
