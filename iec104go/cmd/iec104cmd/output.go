// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	iec "github.com/tonylturner/iec104go"
)

const timestampLayout = "2006-01-02T15:04:05.000Z"

type output struct {
	mu       sync.Mutex
	out, log io.Writer
	now      func() time.Time
}

func (o *output) stamp() string { return o.now().UTC().Format(timestampLayout) }
func (o *output) logf(format string, args ...any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.log, "%s iec104cmd %s\n", o.stamp(), fmt.Sprintf(format, args...))
}
func (o *output) line(format string, args ...any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.out, format+"\n", args...)
}
func (o *output) event(name string) {
	o.line(`{"event":"%s","at":"%s"}`, name, o.stamp())
}
func (o *output) result(op string, ok bool) {
	status := "failed"
	if ok {
		status = "ok"
	}
	o.line(`{"event":"result","at":"%s","op":"%s","status":"%s"}`, o.stamp(), op, status)
}

func quality(q iec.Quality) string {
	if q == 0 {
		return "GOOD"
	}
	var words []string
	for _, b := range []struct {
		bit  iec.Quality
		name string
	}{{iec.Invalid, "IV"}, {iec.NotTopical, "NT"}, {iec.Substituted, "SB"}, {iec.Blocked, "BL"}, {iec.Overflow, "OV"}} {
		if q&b.bit != 0 {
			words = append(words, b.name)
		}
	}
	return strings.Join(words, "|")
}

func dpWord(n uint8) string { return []string{"intermediate", "open", "closed", "indeterminate"}[n&3] }
func dpLog(n uint8) string  { return []string{"INTERMEDIATE", "OFF", "ON", "INDETERMINATE"}[n&3] }
func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}
func bit(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cp56(t iec.CP56Time2a) string {
	// Calendar interpretation follows the stack's public UTC profile.
	at, valid := t.Time()
	if !valid {
		return "" // No invented calendar normalization for malformed peer tags.
	}
	s := at.Format(timestampLayout)
	if t.Invalid {
		s += "(IV)"
	}
	return s
}

func floatText(f float32) string {
	return fmt.Sprintf("%.3f", f)
}

func describe(typ iec.TypeID, e iec.Element) string {
	switch typ {
	case iec.M_SP_NA_1, iec.M_DP_NA_1, iec.M_ST_NA_1, iec.M_ME_NC_1,
		iec.M_SP_TB_1, iec.M_DP_TB_1, iec.M_ST_TB_1,
		iec.C_SC_NA_1, iec.C_DC_NA_1, iec.C_RC_NA_1, iec.C_IC_NA_1:
	default:
		return "(no decoder for this type)"
	}
	var timed *iec.CP56Time2a
	if t, ok := e.(iec.Timed); ok {
		e, timed = t.Value, &t.Time
	}
	text := "(no decoder for this type)"
	switch v := e.(type) {
	case iec.SinglePoint:
		text = fmt.Sprintf("value=%s q=%s", onOff(v.SIQ.State()), quality(v.SIQ.Quality()))
	case iec.DoublePoint:
		text = fmt.Sprintf("value=%s q=%s", dpLog(v.DIQ.State()), quality(v.DIQ.Quality()))
	case iec.StepPosition:
		// common.c intentionally omits the time on timed step log lines.
		return fmt.Sprintf("value=%d transient=%d q=%s", v.Value.Position(), bit(v.Value.Transient()), quality(v.Quality))
	case iec.Float:
		if timed == nil {
			text = fmt.Sprintf("value=%s q=%s", floatText(v.Value), quality(v.Quality))
		}
	case iec.SingleCommand:
		return fmt.Sprintf("scs=%s qu=%d se=%s", onOff(v.Qualifier.State()&1 != 0), v.Qualifier.QU(), selectWord(v.Qualifier))
	case iec.DoubleCommand:
		return fmt.Sprintf("dcs=%d(%s) qu=%d se=%s", v.Qualifier.State(), dpLog(v.Qualifier.State()), v.Qualifier.QU(), selectWord(v.Qualifier))
	case iec.RegulatingCommand:
		return fmt.Sprintf("rcs=%d qu=%d se=%s", v.Qualifier.State(), v.Qualifier.QU(), selectWord(v.Qualifier))
	case iec.Interrogation:
		return fmt.Sprintf("qoi=%d", v.QOI)
	}
	if timed != nil {
		switch e.(type) {
		case iec.SinglePoint, iec.DoublePoint:
			text += " time=" + cp56(*timed)
		}
	}
	return text
}

func selectWord(q iec.CommandQualifier) string {
	if q.Select() {
		return "SELECT"
	}
	return "EXECUTE"
}

func (o *output) asdu(m iec.Message, monitor bool) {
	h := m.GetHeader()
	o.logf("rx ASDU type=%s(%d) cot=%s(%d) neg=%d test=%d oa=%d ca=%d objects=%d",
		h.Type, h.Type, h.Cause, h.Cause, bit(h.Negative), bit(h.Test), h.Originator, h.CommonAddress, h.Count)
	a, ok := m.(*iec.ASDU)
	if !ok {
		for i := 0; i < int(h.Count); i++ {
			o.logf("rx   object %d does not decode", i)
		}
		return
	}
	for _, obj := range a.Objects {
		o.logf("rx   ioa=%d %s", obj.Address, describe(h.Type, obj.Element))
	}
	if !monitor {
		return
	}
	at := o.stamp()
	for _, obj := range a.Objects {
		e := obj.Element
		timestamp := "null"
		if t, ok := e.(iec.Timed); ok {
			e = t.Value
			timestamp = fmt.Sprintf(`"%s"`, cp56(t.Time))
		}
		var value string
		var q iec.Quality
		switch h.Type {
		case iec.M_SP_NA_1, iec.M_SP_TB_1:
			v := e.(iec.SinglePoint)
			value, q = fmt.Sprintf("%t", v.SIQ.State()), v.SIQ.Quality()
		case iec.M_DP_NA_1, iec.M_DP_TB_1:
			v := e.(iec.DoublePoint)
			value, q = fmt.Sprintf(`"%s"`, dpWord(v.DIQ.State())), v.DIQ.Quality()
		case iec.M_ST_NA_1, iec.M_ST_TB_1:
			v := e.(iec.StepPosition)
			value, q = fmt.Sprintf(`{"value":%d,"transient":%t}`, v.Value.Position(), v.Value.Transient()), v.Quality
		case iec.M_ME_NC_1:
			v := e.(iec.Float)
			value, q = floatText(v.Value), v.Quality
		default:
			continue
		}
		o.line(`{"event":"asdu","at":"%s","type":%d,"cot":%d,"ioa":%d,"value":%s,"quality":"%s","time":%s}`,
			at, h.Type, h.Cause, obj.Address, value, quality(q), timestamp)
	}
}
