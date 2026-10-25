// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"fmt"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func (c *Centre) wait(ctx context.Context, deadline time.Time, done func(*tracker) bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		if done(&c.tracker) {
			return "done"
		}
		if c.link.state != "started" || c.link.seq != c.tracker.seq || c.link.shutdown {
			return "lost"
		}
		left := deadline.Sub(c.opts.Now())
		if left <= 0 {
			return "timeout"
		}
		changed := c.changed
		c.mu.Unlock()
		timerC, stopTimer := c.waitTimer(left)
		select {
		case <-ctx.Done():
		case <-changed:
		case <-timerC:
		}
		stopTimer()
		c.mu.Lock()
		if ctx.Err() != nil {
			return "lost"
		}
	}
}

func (c *Centre) waitTimer(after time.Duration) (<-chan time.Time, func()) {
	if c.opts.Clock != nil {
		timer := c.opts.Clock.NewTimer(c.opts.Clock.Now().Add(after))
		return timer.C(), func() { timer.Stop() }
	}
	timer := time.NewTimer(after)
	return timer.C, func() { timer.Stop() }
}

func (c *Centre) sendCommand(value int, selecting bool) bool {
	c.mu.Lock()
	t := c.tracker
	cmd := c.pm.Commands[t.index]
	s := c.session
	ok := s != nil && c.link.state == "started" && c.link.seq == t.seq && c.opts.Now().Before(t.deadline)
	c.mu.Unlock()
	if !ok {
		return false
	}
	qual := byte(value)
	if selecting {
		qual |= 0x80
	}
	var element iec.Element
	var typ iec.TypeID
	switch cmd.Kind {
	case pointmap.DoubleCommand:
		typ = iec.C_DC_NA_1
		element = iec.DoubleCommand{Qualifier: iec.CommandQualifier(qual)}
	case pointmap.RegulatingStep:
		typ = iec.C_RC_NA_1
		element = iec.RegulatingCommand{Qualifier: iec.CommandQualifier(qual)}
	case pointmap.SingleCommand:
		typ = iec.C_SC_NA_1
		element = iec.SingleCommand{Qualifier: iec.CommandQualifier(qual)}
	}
	err := s.Send(iec.ASDU{Header: iec.Header{Type: typ, Cause: 6, CommonAddress: uint16(c.pm.CA)}, Objects: []iec.Object{{Address: uint32(cmd.IOA), Element: element}}})
	if err != nil {
		c.sendFailures.Add(1)
		c.printf("send error: %v", err)
	}
	return err == nil
}
func (c *Centre) phases(ctx context.Context, value int, deadline time.Time) (string, string) {
	if !c.sendCommand(value, true) {
		return "unknown", "link lost before the select was sent"
	}
	selectDeadline := c.opts.Now().Add(time.Duration(c.pm.Timers.T1) * time.Second)
	if deadline.Before(selectDeadline) {
		selectDeadline = deadline
	}
	w := c.wait(ctx, selectDeadline, func(t *tracker) bool { return t.selectCon != 0 })
	if w == "lost" {
		return "unknown", "link lost before the execute was sent"
	}
	if w == "timeout" {
		return "unknown", "no select confirmation; execute not sent"
	}
	c.mu.Lock()
	ok := c.tracker.selectCon == 1
	cause := c.tracker.selectCause
	if ok {
		c.tracker.phase = "execute"
		c.tracker.executed = c.stamp()
	}
	c.mu.Unlock()
	if !ok {
		return "rejected", rejectionReason("select", cause)
	}
	if !c.sendCommand(value, false) {
		return "unknown", "link lost before the execute was sent"
	}
	w = c.wait(ctx, deadline, func(t *tracker) bool { return t.executeCon != 0 })
	if w == "lost" {
		return "unknown", "link lost after the execute was sent"
	}
	if w == "timeout" {
		return "unknown", "no execute confirmation within the command budget"
	}
	c.mu.Lock()
	ok = c.tracker.executeCon == 1
	cause = c.tracker.executeCause
	c.mu.Unlock()
	if !ok {
		return "rejected", rejectionReason("execute", cause)
	}
	w = c.wait(ctx, deadline, func(t *tracker) bool { return t.terminated })
	if w == "lost" {
		return "unknown", "link lost before ACT-TERM"
	}
	if w == "timeout" {
		return "unknown", "no ACT-TERM within the command budget"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tracker
	if t.negative {
		return "failed", "negative ACT-TERM"
	}
	if !t.feedback {
		return "unknown", fmt.Sprintf("no good matching COT 11 feedback for %s before ACT-TERM", c.pm.Points[c.pm.Commands[t.index].FeedbackIndex].ID)
	}
	return "completed", ""
}

func rejectionReason(phase string, cause iec.COT) string {
	if cause == iec.ActivationCon {
		return phase + " rejected (negative ACT-CON)"
	}
	name := map[iec.COT]string{
		iec.UnknownType:  "unknown type",
		iec.UnknownCause: "unknown cause",
		iec.UnknownCA:    "unknown common address",
		iec.UnknownIOA:   "unknown IOA",
	}[cause]
	if name == "" {
		return fmt.Sprintf("%s rejected (COT %d)", phase, cause)
	}
	return fmt.Sprintf("%s rejected (%s, COT %d)", phase, name, cause)
}

func (c *Centre) command(ctx context.Context, index, value int, operation string) response {
	c.mu.Lock()
	cmd := c.pm.Commands[index]
	fb := c.points[cmd.FeedbackIndex]
	if c.tracker.active || c.link.reconnect {
		text := "a command is in flight"
		if !c.tracker.active {
			text = "an operator reconnect is pending"
		}
		c.mu.Unlock()
		return failure(409, text)
	}
	if c.link.state != "started" {
		c.mu.Unlock()
		return failure(503, "link not started")
	}
	if c.link.completed == "" || !fb.has || fb.q&(iec.Invalid|iec.NotTopical) != 0 {
		c.mu.Unlock()
		return failure(409, "feedback is stale or invalid")
	}
	expected := value
	if cmd.Kind == pointmap.RegulatingStep {
		expected = fb.actual + 1
		if value == 1 {
			expected = fb.actual - 1
		}
	}
	deadline := c.opts.Now().Add(10 * time.Second)
	c.tracker = tracker{active: true, index: index, seq: c.link.seq, phase: "select", deadline: deadline, expected: expected, value: value, selected: c.stamp()}
	c.commandSeq++
	id := fmt.Sprintf("cmd-%d", c.commandSeq)
	c.mu.Unlock()
	status, reason := c.phases(ctx, value, deadline)
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tracker
	var feedback any
	if t.feedback {
		feedback = ordered("ioa", c.pm.Points[cmd.FeedbackIndex].IOA, "value", t.feedbackValue, "at", nullable(t.feedbackAt))
	}
	result := ordered("id", id, "point", cmd.ID, "operation", operation, "result", status, "reason", nullable(reason), "selected_at", nullable(t.selected), "executed_at", nullable(t.executed), "con_at", nullable(t.con), "term_at", nullable(t.term), "feedback", feedback)
	c.add("command", result)
	c.tracker = tracker{}
	c.notify()
	return response{status: 200, body: result}
}
