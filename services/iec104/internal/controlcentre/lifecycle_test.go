// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func element(p pointmap.Point) (iec.TypeID, iec.Element) {
	switch p.Kind {
	case pointmap.DoublePoint:
		return 3, iec.DoublePoint{DIQ: 2}
	case pointmap.SinglePoint:
		return 1, iec.SinglePoint{SIQ: 1}
	case pointmap.StepPosition:
		return 5, iec.StepPosition{Value: 3}
	default:
		return 13, iec.Float{Value: 28.9}
	}
}
func TestTCPReconnectAndHTTP(t *testing.T) {
	pm, err := pointmap.Load("../../check/points.test.json")
	if err != nil {
		t.Fatal(err)
	}
	pm.ReconnectIntervalMS = 10
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	peer, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sessions := make(chan *iec.Session, 32)
	var receiveCount atomic.Int64
	server, err := iec.NewServer(iec.DefaultOptions(), func(_ context.Context, e iec.Event) {
		if e.Kind == iec.Started {
			sessions <- e.Session
		}
		if e.Kind != iec.ASDUReceived {
			return
		}
		receiveCount.Add(1)
		a := e.ASDU.(*iec.ASDU)
		h := a.Header
		send := func(m iec.Message) {
			if err := e.Session.Send(m); err != nil {
				t.Errorf("peer send: %v", err)
			}
		}
		if h.Type == 100 {
			h.Cause = 7
			send(iec.ASDU{Header: h, Objects: a.Objects})
			for _, p := range pm.Points {
				typ, v := element(p)
				send(iec.ASDU{Header: iec.Header{Type: typ, Cause: 20, CommonAddress: uint16(pm.CA)}, Objects: []iec.Object{{Address: uint32(p.IOA), Element: v}}})
			}
			h.Cause = 10
			send(iec.ASDU{Header: h, Objects: a.Objects})
			return
		}
		h.Cause = 7
		send(iec.ASDU{Header: h, Objects: a.Objects})
		if a.Objects[0].Element.(iec.DoubleCommand).Qualifier.Select() {
			return
		}
		value := a.Objects[0].Element.(iec.DoubleCommand).Qualifier.State()
		send(iec.ASDU{Header: iec.Header{Type: 3, Cause: 11, CommonAddress: uint16(pm.CA)}, Objects: []iec.Object{{Address: 1001, Element: iec.DoublePoint{DIQ: iec.DIQ(value)}}}})
		h.Cause = 10
		send(iec.ASDU{Header: h, Objects: a.Objects})
	})
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(ctx, peer) }()
	defer func() {
		cancel()
		shutdown, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		server.Shutdown(shutdown)
		<-served
	}()
	c, err := New(pm, peer.Addr().String(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	api, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Serve(ctx, api, "127.0.0.1", nil) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	wait := func(predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for !predicate() {
			if time.Now().After(deadline) {
				t.Fatal("state did not converge")
			}
			time.Sleep(time.Millisecond)
		}
	}
	integrity := func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.link.gi != "complete" {
			return false
		}
		for _, p := range c.points {
			if p.stale != "" || p.q != 0 {
				return false
			}
		}
		return true
	}
	wait(integrity)
	epoch := c.Epoch()
	client := http.Client{Timeout: 2 * time.Second}
	base := "http://" + api.Addr().String()
	for i := 0; i < 12; i++ {
		var session *iec.Session
		select {
		case session = <-sessions:
		case <-ctx.Done():
			t.Fatal("missing session")
		}
		c.mu.Lock()
		oldSeq := c.link.seq
		c.mu.Unlock()
		session.RequestClose()
		wait(func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.link.seq > oldSeq })
		wait(integrity)
	}
	if c.Epoch() != epoch {
		t.Fatal("epoch changed")
	}
	for _, op := range []string{"open", "close", "close"} {
		r, err := client.Post(base+"/api/commands", "application/json", strings.NewReader(`{"point":"feeder-breaker-command","operation":"`+op+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil || r.StatusCode != 200 || !strings.Contains(string(b), `"result":"completed"`) {
			t.Fatal(string(b), err)
		}
	}
	// One GI per session, two sends per command. No duplicate callback delivery.
	if got := receiveCount.Load(); got != 13+6 {
		t.Fatalf("received %d ASDUs, want 19", got)
	}

	for _, path := range []string{"/api/gi", "/api/reconnect"} {
		c.mu.Lock()
		beforeSeq, beforeJournal := c.link.seq, c.journalSeq
		c.mu.Unlock()
		r, err := client.Post(base+path, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 202 || !strings.HasPrefix(string(body), `{"trigger":"operator","requested_at":"`) {
			t.Fatalf("%d %s", r.StatusCode, body)
		}
		if path == "/api/reconnect" {
			wait(func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.link.seq > beforeSeq })
		}
		wait(func() bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.link.gi == "complete" && c.journalSeq > beforeJournal
		})
		wait(integrity)
	}
	if c.Epoch() != epoch {
		t.Fatal("operator reconnect changed boot identity")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.link.seq != 14 {
		t.Fatal(c.link.seq)
	}
	down, up, complete := 0, 0, 0
	for i, e := range c.journal {
		switch e.kind {
		case "link_down":
			down++
			if i+1 >= len(c.journal) || c.journal[i+1].kind != "point_stale" {
				t.Fatal("link_down must precede stale points")
			}
		case "link_up":
			up++
		case "gi_complete":
			complete++
		}
	}
	if down < 1 || up < 1 || complete != up+1 || len(c.journal) != 500 || c.journal[0].seq != c.journalSeq-499 {
		t.Fatal(down, up, complete)
	}
}

type failingListener struct {
	net.Listener
	err   error
	after func()
}

func (l failingListener) Accept() (net.Conn, error) {
	if l.after != nil {
		l.after()
	}
	return nil, l.err
}
func TestShutdownOrdering(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		for _, failure := range []error{net.ErrClosed, errors.New("injected accept failure")} {
			t.Run(fmt.Sprintf("stopped=%v/%v", stopped, failure), func(t *testing.T) {
				c, _ := model(t)
				c.opts.Dial = func(ctx context.Context, _ string, _ iec.Options, _ iec.Handler) (Transport, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				l, err := net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if stopped {
					cancel()
				}
				err = c.Serve(ctx, failingListener{Listener: l, err: failure}, "127.0.0.1", nil)
				clean := stopped && errors.Is(failure, net.ErrClosed)
				if clean && err != nil || !clean && !errors.Is(err, failure) {
					t.Fatalf("Serve=%v", err)
				}
			})
		}
	}
}
func TestShutdownBeforeDialAndHTTPStart(t *testing.T) {
	c, _ := model(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Serve(ctx, l, "127.0.0.1", nil); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.DialTimeout("tcp4", l.Addr().String(), time.Second); err == nil {
		conn.Close()
		t.Fatal("listener leaked")
	}
}
