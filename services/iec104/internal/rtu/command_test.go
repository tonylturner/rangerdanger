// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"testing"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/modbus"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func commandModel(t *testing.T, kind pointmap.Kind) (*RTU, *int64, *captureConn) {
	t.Helper()
	r, now, c := newModel(t, 1)
	r.pm.Commands = []pointmap.Command{{IOA: 3001, Kind: kind, SBO: true, Address: 7}}
	switch kind {
	case pointmap.DoubleCommand:
		r.pm.Points[0].Kind = pointmap.DoublePoint
	case pointmap.SingleCommand:
		r.pm.Points[0].Kind = pointmap.SinglePoint
	case pointmap.RegulatingStep:
		r.pm.Points[0].Kind = pointmap.StepPosition
	}
	r.points[0] = pointState{dp: 1, repDP: 1, step: 5, repStep: 5}
	r.devices[0].pollSeq = 1
	r.devices[0].pollStart = *now - 1
	return r, now, c
}
func request(kind pointmap.Kind, value int, selecting bool) *iec.ASDU {
	q := iec.CommandQualifier(value)
	if selecting {
		q |= 128
	}
	var e iec.Element
	var typ iec.TypeID
	switch kind {
	case pointmap.DoubleCommand:
		typ, e = iec.C_DC_NA_1, iec.DoubleCommand{Qualifier: q}
	case pointmap.SingleCommand:
		typ, e = iec.C_SC_NA_1, iec.SingleCommand{Qualifier: q}
	case pointmap.RegulatingStep:
		typ, e = iec.C_RC_NA_1, iec.RegulatingCommand{Qualifier: q}
	}
	return &iec.ASDU{Header: iec.Header{Type: typ, Cause: iec.Activation, Originator: 17, CommonAddress: 1}, Objects: []iec.Object{{Address: 3001, Element: e}}}
}
func response(t *testing.T, c *captureConn, cot iec.COT, negative bool) {
	t.Helper()
	if len(c.messages) == 0 {
		t.Fatal("no response")
	}
	h := c.messages[len(c.messages)-1].GetHeader()
	if h.Cause != cot || h.Negative != negative || h.Originator != 17 {
		t.Fatalf("response %+v", h)
	}
}
func queueExecute(t *testing.T, r *RTU, c *captureConn, kind pointmap.Kind, value int) {
	t.Helper()
	r.receive(c, request(kind, value, true))
	response(t, c, iec.ActivationCon, false)
	c.messages = nil
	r.receive(c, request(kind, value, false))
	if !r.pending.active || r.sel.active || len(c.messages) != 0 {
		t.Fatal("execute acknowledged before write")
	}
}
func TestCommandCompletion(t *testing.T) {
	for _, kind := range []pointmap.Kind{pointmap.DoubleCommand, pointmap.SingleCommand, pointmap.RegulatingStep} {
		t.Run(string(kind), func(t *testing.T) {
			for _, value := range []int{1, 2} {
				if kind == pointmap.SingleCommand {
					value--
				}
				r, now, c := commandModel(t, kind)
				if kind == pointmap.DoubleCommand && value == 1 {
					r.points[0].dp, r.points[0].repDP = 2, 2
				}
				if kind == pointmap.SingleCommand && value == 0 {
					r.points[0].sp, r.points[0].repSP = true, true
				}
				queueExecute(t, r, c, kind, value)
				other := &captureConn{id: 2, addr: testAddr("192.0.2.2:10000")}
				r.connectionEvent(other, iec.Started)
				// Equal poll-start time is not fresh, even with a newer sequence.
				r.devices[0].pollSeq++
				r.devices[0].pollStart = *now
				r.advanceCommand(*now)
				if r.pending.phase != commandStart {
					t.Fatal("accepted overlapping poll")
				}
				*now++
				r.devices[0].pollStart = *now
				r.devices[0].lastOK = *now
				r.advanceCommand(*now)
				if r.pending.phase != commandWrite {
					t.Fatal("fresh poll did not unlock write")
				}
				m := &fakeMaster{result: modbus.Result{Status: modbus.OK}}
				r.writeCommand(0, m)
				r.writeCommand(0, m)
				if m.writes != 1 || m.writeAddress != 7 {
					t.Fatal("write duplicated/misdirected")
				}
				want := value
				if kind == pointmap.SingleCommand {
					want = value + 1
				}
				if int(m.writeValue) != want {
					t.Fatalf("FC6=%d want %d", m.writeValue, want)
				}
				response(t, c, iec.ActivationCon, false)
				if r.pending.phase != commandFeedback {
					t.Fatal("not waiting feedback")
				}
				budget := int64(1000)
				if kind == pointmap.RegulatingStep {
					budget = 5000
				}
				if r.pending.deadline != *now+budget {
					t.Fatal("wrong feedback timeout")
				}
				*now++
				r.devices[0].lastOK = *now
				r.devices[0].pollStart = *now
				r.devices[0].pollSeq++
				switch kind {
				case pointmap.DoubleCommand:
					r.points[0].dp = uint8(value)
				case pointmap.SingleCommand:
					r.points[0].sp = value != 0
				case pointmap.RegulatingStep:
					r.points[0].step = r.pending.expected
				}
				c.messages = nil
				r.tick()
				if r.pending.active {
					t.Fatal("matching feedback incomplete")
				}
				if len(c.messages) != 3 || c.messages[0].GetHeader().Cause != iec.Spontaneous || c.messages[1].GetHeader().Cause != iec.ReturnRemote {
					t.Fatalf("order=%v", c.messages)
				}
				response(t, c, iec.ActivationTerm, false)
				if len(other.messages) != 2 || other.messages[1].GetHeader().Cause != iec.ReturnRemote {
					t.Fatal("feedback was not broadcast before requester TERM")
				}
			}
		})
	}
}
func TestCommandWriteOutcomes(t *testing.T) {
	for _, st := range []modbus.Status{modbus.Exception, modbus.Timeout, modbus.IOError} {
		t.Run(string(rune('0'+st)), func(t *testing.T) {
			r, _, c := commandModel(t, pointmap.DoubleCommand)
			queueExecute(t, r, c, pointmap.DoubleCommand, 2)
			r.pending.phase = commandWrite
			m := &fakeMaster{result: modbus.Result{Status: st}}
			r.writeCommand(0, m)
			r.writeCommand(0, m)
			if r.pending.active || m.writes != 1 {
				t.Fatal("FC6 retried/slot retained")
			}
			if st == modbus.Exception {
				response(t, c, iec.ActivationCon, true)
				if m.closes != 0 {
					t.Fatal("exception closed transport")
				}
			} else if len(c.messages) != 0 || m.closes != 1 {
				t.Fatal("ambiguous write acknowledged or left connected")
			}
		})
	}
}
func TestSelectionRejections(t *testing.T) {
	r, now, c := commandModel(t, pointmap.DoubleCommand)
	other := &captureConn{id: 2, addr: c.addr}
	r.receive(c, request(pointmap.DoubleCommand, 1, false))
	response(t, c, iec.ActivationCon, true)
	r.receive(c, request(pointmap.DoubleCommand, 1, true))
	r.receive(other, request(pointmap.DoubleCommand, 1, false))
	response(t, other, iec.ActivationCon, true)
	if !r.sel.active || r.sel.id != c.id {
		t.Fatal("other identity stole selection")
	}
	invalid := request(pointmap.DoubleCommand, 1, true)
	invalid.Objects[0].Element = iec.DoubleCommand{Qualifier: 133}
	r.receive(c, invalid)
	response(t, c, iec.ActivationCon, true)
	if !r.sel.active {
		t.Fatal("QU rejection cleared selection")
	}
	r.receive(c, request(pointmap.DoubleCommand, 2, false))
	response(t, c, iec.ActivationCon, true)
	if r.sel.active {
		t.Fatal("mismatch kept selection")
	}
	r.receive(c, request(pointmap.DoubleCommand, 1, true))
	*now = r.sel.deadline
	r.receive(c, request(pointmap.DoubleCommand, 1, false))
	response(t, c, iec.ActivationCon, true)
	r.receive(c, request(pointmap.DoubleCommand, 1, true))
	deact := request(pointmap.DoubleCommand, 1, true)
	deact.Header.Cause = iec.Deactivation
	r.receive(other, deact)
	response(t, other, iec.DeactivationCon, true)
	if !r.sel.active {
		t.Fatal("nonowner deactivated")
	}
	r.receive(c, deact)
	response(t, c, iec.DeactivationCon, false)
	if r.sel.active {
		t.Fatal("deactivation failed")
	}
}
func TestCommandFreshnessAndDeadline(t *testing.T) {
	for _, phase := range []commandPhase{commandStart, commandWrite, commandFeedback} {
		t.Run(string(rune('0'+phase)), func(t *testing.T) {
			r, now, c := commandModel(t, pointmap.DoubleCommand)
			queueExecute(t, r, c, pointmap.DoubleCommand, 2)
			r.pending.phase = phase
			*now = r.pending.deadline
			r.devices[0].pollSeq++
			r.devices[0].lastOK = *now
			r.devices[0].pollStart = *now
			r.points[0].dp = 2
			r.advanceCommand(*now)
			cot := iec.ActivationCon
			if phase == commandFeedback {
				cot = iec.ActivationTerm
			}
			response(t, c, cot, true)
			if r.pending.active {
				t.Fatal("deadline did not win")
			}
		})
	}
	r, now, c := commandModel(t, pointmap.RegulatingStep)
	queueExecute(t, r, c, pointmap.RegulatingStep, 2)
	*now++
	r.devices[0].pollSeq++
	r.devices[0].pollStart = *now
	r.devices[0].lastOK = *now
	r.points[0].valueInvalid = true
	r.advanceCommand(*now)
	if r.pending.phase != commandStart {
		t.Fatal("bad quality passed fresh guard")
	}
	r.points[0].valueInvalid = false
	r.points[0].transient = true
	r.advanceCommand(*now)
	response(t, c, iec.ActivationCon, true)
	if r.pending.active {
		t.Fatal("transient start accepted")
	}
}
func TestRetirementDuringWrite(t *testing.T) {
	r, now, c := commandModel(t, pointmap.DoubleCommand)
	queueExecute(t, r, c, pointmap.DoubleCommand, 2)
	r.pending.phase = commandWrite
	r.pending.expected = 2
	replacement := &captureConn{id: 2, addr: c.addr}
	m := &fakeMaster{onWrite: func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.connectionEvent(c, iec.Closed)
		r.connectionEvent(replacement, iec.Started)
	}}
	r.writeCommand(0, m)
	if len(c.messages) != 0 || len(replacement.messages) != 0 || r.pending.phase != commandFeedback {
		t.Fatal("retired response rerouted")
	}
	*now++
	r.devices[0].pollSeq++
	r.devices[0].lastOK = *now
	r.devices[0].pollStart = *now
	r.points[0].dp = 2
	r.advanceCommand(*now)
	if len(replacement.messages) != 1 || replacement.messages[0].GetHeader().Cause != iec.ReturnRemote || r.pending.active {
		t.Fatal("field completion lost or TERM rerouted")
	}
}
