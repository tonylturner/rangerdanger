// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"context"
	"net"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func endpointText(addr net.Addr) (ip, peer string) {
	if addr == nil {
		return "?", "?"
	}
	switch addr.Network() {
	case "tcp", "tcp4", "tcp6":
		ip, _, err := net.SplitHostPort(addr.String())
		if err == nil {
			return ip, addr.String()
		}
	}
	return "?", "?"
}

func (r *RTU) handle(_ context.Context, e iec.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch e.Kind {
	case iec.Opened, iec.Started, iec.Stopped, iec.Closed:
		r.connectionEvent(e.Session, e.Kind)
	case iec.ASDUReceived:
		r.receive(e.Session, e.ASDU)
	}
}

func (r *RTU) connectionEvent(conn connection, kind iec.EventKind) {
	ip, peer := endpointText(conn.RemoteAddr())
	index := -1
	for i, p := range r.started {
		if p.conn.ID() == conn.ID() {
			index, peer = i, p.peer
			break
		}
	}
	switch kind {
	case iec.Opened:
		r.printf("connection request from %s", ip)
		r.printf("connection opened peer=%s", peer)
	case iec.Started:
		r.printf("STARTDT peer=%s", peer)
		if index >= 0 {
			return
		}
		if len(r.started) == 8 {
			r.printf("started connection list full (%d); peer=%s not added", 8, peer)
			return
		}
		r.started = append(r.started, startedPeer{conn: conn, peer: peer})
	case iec.Stopped, iec.Closed:
		name := "STOPDT"
		if kind == iec.Closed {
			name = "connection closed"
		}
		r.printf("%s peer=%s", name, peer)
		if index >= 0 {
			if r.sel.active && r.sel.id == conn.ID() {
				r.clearSelection("selecting connection stopped")
			}
			if r.pending.active && r.pending.id == conn.ID() {
				// Field execution still completes, but never reroutes its reply
				// to a new session, even if its address tuple is identical.
				r.pending.id, r.pending.peer = 0, ""
			}
			last := len(r.started) - 1
			r.started[index] = r.started[last]
			r.started[last] = startedPeer{}
			r.started = r.started[:last]
		}
	}
}

func (r *RTU) connection(id uint64) connection {
	for _, p := range r.started {
		if p.conn.ID() == id {
			return p.conn
		}
	}
	return nil
}

func (r *RTU) reply(conn connection, request iec.Message, cot iec.COT, negative bool) {
	m, err := iec.Mirror(request, cot, negative)
	if err != nil {
		r.buildError(err)
		return
	}
	r.send(conn, m)
}

func (r *RTU) receive(conn connection, m iec.Message) {
	h := m.GetHeader()
	switch h.Type {
	case iec.C_IC_NA_1:
		r.logASDU("rx", m)
		if int(h.CommonAddress) != r.pm.CA {
			r.reply(conn, m, iec.UnknownCA, true)
			return
		}
		if h.Cause == iec.Deactivation {
			r.reply(conn, m, iec.DeactivationCon, true)
			return
		}
		a, ok := m.(*iec.ASDU)
		if !ok || len(a.Objects) == 0 {
			r.reply(conn, m, iec.UnknownIOA, true)
			return
		}
		q, ok := a.Objects[0].Element.(iec.Interrogation)
		if !ok || q.QOI != 20 {
			r.reply(conn, m, iec.ActivationCon, true)
			return
		}
		r.reply(conn, m, iec.ActivationCon, false)
		r.buildError(r.reportImage(conn, h.Originator, false, 0))
		r.reply(conn, m, iec.ActivationTerm, false)
	case iec.C_DC_NA_1, iec.C_RC_NA_1, iec.C_SC_NA_1:
		r.logASDU("rx", m)
		if int(h.CommonAddress) != r.pm.CA {
			r.reply(conn, m, iec.UnknownCA, true)
			return
		}
		if h.Cause != iec.Activation && h.Cause != iec.Deactivation {
			r.reply(conn, m, iec.UnknownCause, true)
			return
		}
		a, ok := m.(*iec.ASDU)
		if !ok || len(a.Objects) != 1 {
			r.reply(conn, m, iec.UnknownIOA, true)
			return
		}
		c := r.pm.CommandByIOA(int(a.Objects[0].Address))
		if c == nil {
			r.reply(conn, m, iec.UnknownIOA, true)
			return
		}
		expected := map[pointmap.Kind]iec.TypeID{pointmap.DoubleCommand: iec.C_DC_NA_1,
			pointmap.RegulatingStep: iec.C_RC_NA_1, pointmap.SingleCommand: iec.C_SC_NA_1}[c.Kind]
		if h.Type != expected || h.Test {
			r.reply(conn, m, iec.ActivationCon, true)
			return
		}
		var q iec.CommandQualifier
		switch v := a.Objects[0].Element.(type) {
		case iec.DoubleCommand:
			q = v.Qualifier
		case iec.RegulatingCommand:
			q = v.Qualifier
		case iec.SingleCommand:
			q = v.Qualifier
		}
		value := int(q.State())
		if c.Kind == pointmap.SingleCommand {
			value &= 1
		}
		index := -1
		for i := range r.pm.Commands {
			if c == &r.pm.Commands[i] {
				index = i
				break
			}
		}
		r.handleCommand(conn, m, index, value, int(q.QU()), q.Select())
	default:
		r.reply(conn, m, iec.UnknownType, true)
	}
}
