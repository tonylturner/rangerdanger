// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/modbus"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func (r *RTU) clearSelection(why string) {
	if r.sel.active {
		r.printf("command ioa=%d peer=%s phase=select result=cleared reason=\"%s\"",
			r.pm.Commands[r.sel.index].IOA, r.sel.peer, why)
	}
	r.sel = selection{}
}

func (r *RTU) handleCommand(conn connection, request iec.Message, index, value, qu int, selectCommand bool) {
	c, now := r.pm.Commands[index], r.opts.Now()
	_, peer := endpointText(conn.RemoteAddr())
	if r.sel.active && now >= r.sel.deadline {
		r.clearSelection("select timeout elapsed")
	}
	owns := r.sel.active && r.sel.id == conn.ID() && r.sel.index == index
	if request.GetHeader().Cause == iec.Deactivation {
		if owns {
			r.clearSelection("deactivated by the selecting client")
			r.reply(conn, request, iec.DeactivationCon, request.GetHeader().Negative)
		} else {
			r.reply(conn, request, iec.DeactivationCon, true)
		}
		return
	}
	phase := "execute"
	if selectCommand {
		phase = "select"
	}
	reject := func(reason string) {
		r.printf("command ioa=%d peer=%s phase=%s result=rejected reason=\"%s\"", c.IOA, peer, phase, reason)
		r.reply(conn, request, iec.ActivationCon, true)
	}
	if qu != 0 || (c.Kind != pointmap.SingleCommand && value != 1 && value != 2) {
		reject("invalid state or qualifier")
		return
	}
	if selectCommand {
		if !c.SBO {
			reject("not an SBO command")
			return
		}
		if r.pending.active {
			r.reply(conn, request, iec.ActivationCon, true)
			return
		}
		if r.sel.active && !owns {
			reject("selected by another peer")
			return
		}
		r.sel = selection{active: true, index: index, value: value, qu: qu, id: conn.ID(), peer: peer,
			deadline: now + int64(r.pm.SelectTimeoutMS), pollSeq: r.devices[c.DeviceIndex].pollSeq, selectedAt: now}
		r.printf("command ioa=%d peer=%s phase=select result=accepted dcs=%d", c.IOA, peer, value)
		r.reply(conn, request, iec.ActivationCon, false)
		return
	}
	if c.SBO && !owns {
		reason := "execute without a valid selection"
		if r.sel.active {
			reason = "selected by another peer"
		}
		reject(reason)
		return
	}
	if c.SBO && (value != r.sel.value || qu != r.sel.qu) {
		r.clearSelection("execute does not match the selection")
		reject("value differs from selection")
		return
	}
	if r.pending.active {
		r.reply(conn, request, iec.ActivationCon, true)
		return
	}
	owned, err := iec.Mirror(request, request.GetHeader().Cause, request.GetHeader().Negative)
	if err != nil {
		r.buildError(err)
		return
	}
	afterPoll, afterTime := r.sel.pollSeq, r.sel.selectedAt
	r.clearSelection("executed")
	r.pending = pendingCommand{active: true, index: index, value: value, id: conn.ID(), peer: peer,
		request: owned, phase: commandStart, afterPoll: afterPoll, afterTime: afterTime,
		deadline: now + int64(r.pm.Devices[c.DeviceIndex].TimeoutMS)}
	r.printf("command ioa=%d peer=%s phase=execute result=accepted dcs=%d", c.IOA, peer, value)
}

func (r *RTU) finishCommand(result string) {
	p := r.pending
	r.printf("command ioa=%d peer=%s phase=execute result=%s", r.pm.Commands[p.index].IOA, p.peer, result)
	r.pending = pendingCommand{}
}

func (r *RTU) pendingReply(cot iec.COT, negative bool) {
	if conn := r.connection(r.pending.id); conn != nil {
		r.reply(conn, r.pending.request, cot, negative)
	}
}

func (r *RTU) advanceCommand(now int64) {
	p := &r.pending
	if !p.active {
		return
	}
	c := r.pm.Commands[p.index]
	fb, dev := r.pm.Points[c.FeedbackIndex], r.pm.Devices[c.DeviceIndex]
	d, value := r.devices[c.DeviceIndex], r.points[c.FeedbackIndex]
	fresh := d.pollSeq > p.afterPoll && d.pollStart > p.afterTime && d.lastOK != 0 &&
		now-d.lastOK < int64(dev.StaleAfterMS) && pointQuality(value, fb) == 0
	if (p.phase == commandStart || p.phase == commandWrite) && now >= p.deadline {
		r.pendingReply(iec.ActivationCon, true)
		r.finishCommand("rejected")
	} else if p.phase == commandStart && fresh {
		p.expected = p.value
		if c.Kind == pointmap.RegulatingStep {
			p.expected = value.step + 1
			if p.value == 1 {
				p.expected = value.step - 1
			}
			if value.transient {
				r.pendingReply(iec.ActivationCon, true)
				r.finishCommand("rejected")
				return
			}
		}
		p.phase = commandWrite
	} else if p.phase == commandFeedback {
		if now >= p.deadline {
			r.pendingReply(iec.ActivationTerm, true)
			r.finishCommand("failed")
			return
		}
		matches := (c.Kind == pointmap.DoubleCommand && int(value.dp) == p.expected) ||
			(c.Kind == pointmap.SingleCommand && boolInt(value.sp) == p.expected) ||
			(c.Kind == pointmap.RegulatingStep && value.step == p.expected && !value.transient)
		if fresh && matches {
			r.reportPoint(c.FeedbackIndex, iec.ReturnRemote)
			r.pendingReply(iec.ActivationTerm, false)
			r.finishCommand("completed")
		}
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Only the owning device worker calls this; the model lock is released for
// the sole FC6 attempt. commandWriting cannot expire or be replaced meanwhile.
func (r *RTU) writeCommand(d int, master Master) {
	r.mu.Lock()
	p := &r.pending
	if !p.active || p.phase != commandWrite || r.pm.Commands[p.index].DeviceIndex != d {
		r.mu.Unlock()
		return
	}
	if r.opts.Now() >= p.deadline {
		r.pendingReply(iec.ActivationCon, true)
		r.finishCommand("rejected")
		r.mu.Unlock()
		return
	}
	p.phase = commandWriting
	c, dev := r.pm.Commands[p.index], r.pm.Devices[d]
	value := p.value
	if c.Kind == pointmap.SingleCommand {
		value = 1
		if p.value != 0 {
			value = 2
		}
	}
	r.mu.Unlock()
	result := master.WriteSingle(c.Address, uint16(value), millis(dev.TimeoutMS))
	if result.Status == modbus.Timeout || result.Status == modbus.IOError {
		master.Close()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if result.Status == modbus.OK {
		r.pendingReply(iec.ActivationCon, false)
		p.phase, p.afterPoll, p.afterTime = commandFeedback, r.devices[d].pollSeq, r.opts.Now()
		budget := dev.TimeoutMS
		if c.Kind == pointmap.RegulatingStep {
			budget = 5000
		}
		p.deadline = r.opts.Now() + int64(budget)
	} else {
		outcome := "unknown"
		if result.Status == modbus.Exception {
			r.pendingReply(iec.ActivationCon, true)
			outcome = "rejected"
		}
		r.finishCommand(outcome)
	}
}
