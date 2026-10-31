// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"reflect"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/logfmt"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func dpName(v uint8) string {
	switch v {
	case 0:
		return "intermediate"
	case 1:
		return "open"
	case 2:
		return "closed"
	default:
		return "indeterminate"
	}
}
func (c *Centre) handle(_ context.Context, e iec.Event) {
	if e.Kind == iec.ASDUReceived {
		lines, err := logfmt.ASDULines("rx", e.ASDU)
		if err != nil {
			c.printf("ASDU log error: %v", err)
		} else {
			for _, line := range lines {
				c.printf("%s", line)
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch e.Kind {
	case iec.Opened:
		c.link.seq++
		c.printf("link opened")
	case iec.Started:
		c.printf("link startdt-con")
		c.link.state = "started"
		c.link.since = c.stamp()
		c.link.down = false
		c.link.pending = "connect"
		c.add("link_up", c.linkDetail())
		c.notify()
	case iec.Stopped:
		c.printf("link stopdt-con")
	case iec.Closed:
		c.printf("link closed")
		c.ended()
	case iec.ASDUReceived:
		h := e.ASDU.GetHeader()
		if h.Count == 0 {
			c.printf("ignored zero-object ASDU type=%d ca=%d", h.Type, h.CommonAddress)
			return
		}
		if int(h.CommonAddress) != c.pm.CA {
			c.printf("ignored ASDU for ca=%d (configured ca=%d)", h.CommonAddress, c.pm.CA)
			return
		}
		if h.Test {
			c.printf("ignored test ASDU type=%d ca=%d", h.Type, h.CommonAddress)
			return
		}
		a, ok := e.ASDU.(*iec.ASDU)
		if !ok {
			if v, yes := e.ASDU.(iec.ASDU); yes {
				a = &v
			} else {
				return
			}
		}
		if h.Type == iec.C_IC_NA_1 {
			if len(a.Objects) == 1 && a.Objects[0].Address == 0 {
				if qoi, ok := a.Objects[0].Element.(iec.Interrogation); ok {
					c.giResponse(h.Cause, h.Negative, qoi.QOI)
				}
			}
			return
		}
		for _, o := range a.Objects {
			switch v := o.Element.(type) {
			case iec.DoubleCommand:
				c.commandResponse(h, int(o.Address), v.Qualifier)
			case iec.SingleCommand:
				c.commandResponse(h, int(o.Address), v.Qualifier)
			case iec.RegulatingCommand:
				c.commandResponse(h, int(o.Address), v.Qualifier)
			default:
				if h.Negative {
					c.printf("ignored negative report type=%d ioa=%d", h.Type, o.Address)
					continue
				}
				c.report(h, o)
			}
		}
	}
}
func (c *Centre) report(h iec.Header, o iec.Object) {
	pp := c.pm.PointByIOA(int(o.Address))
	if pp == nil {
		return
	}
	i := 0
	for j := range c.pm.Points {
		if &c.pm.Points[j] == pp {
			i = j
			break
		}
	}
	element := o.Element
	var source string
	var invalid bool
	if t, ok := element.(iec.Timed); ok {
		utc, valid := t.Time.Time()
		if !valid {
			c.printf("ignored report ioa=%d: invalid CP56 calendar", o.Address)
			return
		}
		source = stamp(utc)
		invalid = t.Time.Invalid
		element = t.Value
	}
	var value any
	var q iec.Quality
	var transient bool
	var actual int
	switch v := element.(type) {
	case iec.DoublePoint:
		if pp.Kind != pointmap.DoublePoint {
			return
		}
		actual = int(v.DIQ.State())
		value = dpName(v.DIQ.State())
		q = v.DIQ.Quality()
	case iec.SinglePoint:
		if pp.Kind != pointmap.SinglePoint {
			return
		}
		value = v.SIQ.State()
		if v.SIQ.State() {
			actual = 1
		}
		q = v.SIQ.Quality()
	case iec.StepPosition:
		if pp.Kind != pointmap.StepPosition {
			return
		}
		actual = int(v.Value.Position())
		transient = v.Value.Transient()
		value = ordered("value", actual, "transient", transient)
		q = v.Quality
	case iec.Float:
		if pp.Kind != pointmap.Float || h.Type != iec.M_ME_NC_1 {
			return
		}
		value = floatValue(v.Value)
		q = v.Quality
		if value == nil {
			q |= iec.Invalid
		}
	default:
		return
	}
	p := &c.points[i]
	before := p.stale
	changed := !p.has || !reflect.DeepEqual(p.value, value)
	stale := ""
	if q&iec.NotTopical != 0 {
		stale = "device_unreachable"
	} else if q&iec.Invalid != 0 {
		stale = "measurement_invalid"
		if c.pm.Devices[pp.DeviceIndex].HasHeartbeat {
			stale = "heartbeat"
		}
	}
	*p = cached{has: true, value: value, actual: actual, q: q, transient: transient, stale: stale, source: source, invalid: invalid, received: c.stamp(), at: c.opts.Now(), typ: h.Type, cot: h.Cause}
	if before != stale {
		c.pointEvent(i)
	}
	if pp.Kind != pointmap.Float && changed && (h.Cause == 3 || h.Cause == 11) {
		var iv any
		if source != "" {
			iv = invalid
		}
		c.add("status_event", ordered("id", pp.ID, "ioa", pp.IOA, "asdu", h.Type.String(), "cot", h.Cause, "value", value, "source_time", nullable(source), "source_time_invalid", iv))
	}
	t := &c.tracker
	if t.active && c.opts.Now().Before(t.deadline) && t.phase == "execute" && t.executeCon == 1 && !t.terminated && !t.feedback && i == c.pm.Commands[t.index].FeedbackIndex && h.Cause == 11 && q == 0 && !transient && actual == t.expected {
		t.feedback = true
		t.feedbackValue = value
		t.feedbackAt = p.received
	}
	if source != "" {
		c.enqueue()
	}
	c.notify()
}
func (c *Centre) commandResponse(h iec.Header, ioa int, qualifier iec.CommandQualifier) {
	t := &c.tracker
	if !t.active || !c.opts.Now().Before(t.deadline) {
		return
	}
	cmd := c.pm.Commands[t.index]
	if h.Test || h.CommonAddress != uint16(c.pm.CA) || h.Originator != 0 || ioa != cmd.IOA || h.Type.String() != cmd.ASDU {
		return
	}
	unknownCause := h.Cause >= iec.UnknownType && h.Cause <= iec.UnknownIOA
	if h.Cause != iec.ActivationCon && h.Cause != iec.ActivationTerm && !unknownCause {
		return
	}
	if int(qualifier&0x7f) != t.value || qualifier.Select() != (t.phase == "select") {
		return
	}
	if h.Cause == iec.ActivationTerm {
		if !qualifier.Select() && t.phase == "execute" && t.executeCon == 1 && !t.terminated {
			t.terminated = true
			t.negative = h.Negative
			t.term = c.stamp()
		}
	} else {
		con := 1
		if h.Negative || unknownCause {
			con = -1
		}
		if qualifier.Select() && t.phase == "select" && t.selectCon == 0 {
			t.selectCon = con
			t.selectCause = h.Cause
		} else if !qualifier.Select() && t.phase == "execute" && t.executeCon == 0 {
			t.executeCon = con
			t.executeCause = h.Cause
			t.con = c.stamp()
		}
	}
	c.notify()
}
