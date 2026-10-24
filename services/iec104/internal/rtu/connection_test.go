// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"fmt"
	"strings"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

// Ports all four groups of check/rtu_connection_checks.c. A synthetic address
// implements only net.Addr, like the stack's immutable endpoint snapshots.
func TestConnectionLifecycle(t *testing.T) {
	r, _, c := newModel(t, 1)
	r.started = nil
	log := &captureLog{}
	r.opts.Logger = log
	r.pm.Commands = []pointmap.Command{{IOA: 3001}}
	for generation := 0; generation < 32; generation++ {
		c.id = uint64(generation + 1)
		c.addr = testAddr(fmt.Sprintf("192.0.2.1:%d", 10000+generation))
		r.connectionEvent(c, iec.Opened)
		r.connectionEvent(c, iec.Started)
		r.connectionEvent(c, iec.Started)
		if len(r.started) != 1 || r.connection(c.id) != c {
			t.Fatal("duplicate START")
		}
		c.messages = nil
		r.reportPoint(0, iec.Spontaneous)
		if len(c.messages) != 1 {
			t.Fatal("report not delivered exactly once")
		}
		r.sel = selection{active: true, id: c.id, peer: c.addr.String()}
		r.pending = pendingCommand{active: true, id: c.id, peer: c.addr.String()}
		peer := c.addr.String()
		c.addr = nil
		r.connectionEvent(c, iec.Closed)
		if len(r.started) != 0 || r.sel.active || r.pending.id != 0 || r.pending.peer != "" || !r.pending.active || r.connection(c.id) != nil {
			t.Fatal("retirement failed")
		}
		if log.lines[len(log.lines)-2] != "connection closed peer="+peer {
			t.Fatalf("lost cached address: %v", log.lines)
		}
		c.messages = nil
		r.reportPoint(0, iec.Spontaneous)
		if len(c.messages) != 0 {
			t.Fatal("sent after CLOSED")
		}
	}
	c.addr = testAddr("192.0.2.1:10100")
	r.connectionEvent(c, iec.Started)
	c.addr = nil
	r.connectionEvent(c, iec.Stopped)
	r.connectionEvent(c, iec.Closed)
	if len(r.started) != 0 {
		t.Fatal("STOP/CLOSED not idempotent")
	}
	c.addr = testAddr("192.0.2.1:10100")
	r.connectionEvent(c, iec.Started)
	r.mu.Lock()
	closing := make(chan struct{})
	closed := make(chan struct{})
	go func() { close(closing); r.mu.Lock(); r.connectionEvent(c, iec.Closed); r.mu.Unlock(); close(closed) }()
	<-closing
	select {
	case <-closed:
		t.Fatal("CLOSED returned while sender owns lock")
	default:
	}
	c.messages = nil
	r.reportPoint(0, iec.Spontaneous)
	if len(c.messages) != 1 {
		t.Fatal("locked send lost")
	}
	r.mu.Unlock()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("CLOSED blocked after unlock")
	}
	if len(r.started) != 0 {
		t.Fatal("CLOSED did not retire")
	}
	var clients []*captureConn
	for i := 0; i < 9; i++ {
		p := &captureConn{id: uint64(100 + i), addr: testAddr(fmt.Sprintf("192.0.2.1:%d", 11000+i))}
		clients = append(clients, p)
		r.connectionEvent(p, iec.Started)
	}
	if len(r.started) != 8 || log.lines[len(log.lines)-1] != "started connection list full (8); peer=192.0.2.1:11008 not added" {
		t.Fatal("full list not logged/bounded")
	}
	for _, p := range clients {
		r.connectionEvent(p, iec.Closed)
	}
	if len(r.started) != 0 {
		t.Fatal("full list did not retire")
	}
}

func TestEndpointLogs(t *testing.T) {
	r, _, c := newModel(t, 1)
	r.started = nil
	log := &captureLog{}
	r.opts.Logger = log
	for _, kind := range []iec.EventKind{iec.Opened, iec.Started, iec.Stopped, iec.Closed} {
		r.connectionEvent(c, kind)
	}
	want := "connection request from 192.0.2.1\nconnection opened peer=192.0.2.1:10000\nSTARTDT peer=192.0.2.1:10000\nSTOPDT peer=192.0.2.1:10000\nconnection closed peer=192.0.2.1:10000"
	if got := strings.Join(log.lines, "\n"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	ip, peer := endpointText(testAddr("[2001:db8::1]:2404"))
	if ip != "2001:db8::1" || peer != "[2001:db8::1]:2404" {
		t.Fatal(ip, peer)
	}
	ip, peer = endpointText(nil)
	if ip != "?" || peer != "?" {
		t.Fatal(ip, peer)
	}
}
