// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func TestRequestRejections(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*iec.ASDU)
		cot    iec.COT
	}{
		{"COT", func(a *iec.ASDU) { a.Header.Cause = iec.Spontaneous }, iec.UnknownCause},
		{"IOA", func(a *iec.ASDU) { a.Objects[0].Address = 999 }, iec.UnknownIOA},
		{"count", func(a *iec.ASDU) { a.Objects = append(a.Objects, a.Objects[0]) }, iec.UnknownIOA},
		{"test", func(a *iec.ASDU) { a.Header.Test = true }, iec.ActivationCon},
		{"kind", func(a *iec.ASDU) {
			a.Header.Type = iec.C_SC_NA_1
			a.Objects[0].Element = iec.SingleCommand{Qualifier: 129}
		}, iec.ActivationCon},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, c := commandModel(t, pointmap.DoubleCommand)
			a := request(pointmap.DoubleCommand, 1, true)
			tc.change(a)
			r.receive(c, a)
			response(t, c, tc.cot, true)
			if r.sel.active || r.pending.active {
				t.Fatal("invalid request mutated model")
			}
		})
	}
	r, _, c := newModel(t, 1)
	other := &captureConn{id: 2, addr: testAddr("192.0.2.2:20000")}
	r.connectionEvent(other, iec.Started)
	gi := &iec.ASDU{Header: iec.Header{Type: iec.C_IC_NA_1, Cause: iec.Activation, CommonAddress: 1, Originator: 17}, Objects: []iec.Object{{Element: iec.Interrogation{QOI: 20}}}}
	r.receive(c, gi)
	if len(c.messages) != 3 || c.messages[0].GetHeader().Cause != iec.ActivationCon || c.messages[1].GetHeader().Cause != iec.InterrogatedStation {
		t.Fatal("GI ordering")
	}
	response(t, c, iec.ActivationTerm, false)
	if len(other.messages) != 0 || r.points[0].repF != 10 {
		t.Fatal("GI broadcast or moved baseline")
	}
	gi.Objects[0].Element = iec.Interrogation{QOI: 21}
	r.receive(c, gi)
	response(t, c, iec.ActivationCon, true)
	gi.Header.Cause = iec.Deactivation
	r.receive(c, gi)
	response(t, c, iec.DeactivationCon, true)
}

type concurrentLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *concurrentLog) Printf(f string, a ...any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := captureLog{}
	c.Printf(f, a...)
	l.lines = append(l.lines, c.lines...)
	return nil
}
func (l *concurrentLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// Exercise the real sessions, immutable addresses, event delivery and shutdown.
// No real southbound destination is contacted: the injected worker owns a fake.
func TestServeSessions(t *testing.T) {
	r, _, _ := newModel(t, 1)
	r.started = nil
	// Production monotonic clock: the model-test clock is deliberately mutable
	// and must not be shared with live workers.
	origin := time.Now()
	r.opts.Now = func() int64 { return 100000 + time.Since(origin).Milliseconds() }
	r.pm.Points[0].Format = pointmap.U16
	r.pm.Points[0].Scale = 1
	r.opts.NewMaster = func(byte) Master { return &fakeMaster{regs: []uint16{10}} }
	log := &concurrentLog{}
	r.opts.Logger = log
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- r.Serve(ctx, l) }()
	defer func() {
		cancel()
		select {
		case err := <-served:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(6 * time.Second):
			t.Error("RTU workers did not join")
		}
	}()
	frames := make(chan iec.Message, 32)
	client, err := iec.Dial(ctx, l.Addr().String(), iec.DefaultOptions(), func(_ context.Context, e iec.Event) {
		if e.Kind == iec.ASDUReceived {
			frames <- e.ASDU
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err = client.StartDT(ctx); err != nil {
		t.Fatal(err)
	}
	gi := iec.ASDU{Header: iec.Header{Type: iec.C_IC_NA_1, Cause: iec.Activation, CommonAddress: 1, Originator: 17}, Objects: []iec.Object{{Element: iec.Interrogation{QOI: 20}}}}
	if err = client.Send(gi); err != nil {
		t.Fatal(err)
	}
	var cots []iec.COT
	for len(cots) < 3 {
		select {
		case m := <-frames:
			if m.GetHeader().Cause == iec.Spontaneous {
				continue
			}
			cots = append(cots, m.GetHeader().Cause)
			if m.GetHeader().Originator != 17 {
				t.Fatal("lost GI OA")
			}
		case <-ctx.Done():
			t.Fatal("GI timeout")
		}
	}
	if cots[0] != iec.ActivationCon || cots[1] != iec.InterrogatedStation || cots[2] != iec.ActivationTerm {
		t.Fatal(cots)
	}
	// The endpoint filter, not receive(), owns envelope/type rejection. A
	// subsequent GI is a callback barrier for the negative log assertion.
	for _, wrongCA := range []bool{false, true} {
		var rejected iec.Message = iec.RawASDU{Header: iec.Header{Type: 200, Count: 1, Cause: iec.Activation, CommonAddress: 1}, Payload: []byte{1, 2, 3, 4}}
		want := iec.UnknownType
		if wrongCA {
			bad := gi
			bad.Header.CommonAddress = 2
			rejected = bad
			want = iec.UnknownCA
		}
		if err := client.Send(rejected); err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case m := <-frames:
				if m.GetHeader().Cause == iec.Spontaneous {
					continue
				}
				if m.GetHeader().Cause != want || !m.GetHeader().Negative {
					t.Fatal(m.GetHeader())
				}
			case <-ctx.Done():
				t.Fatal("filter response timeout")
			}
			break
		}
	}
	if err := client.Send(gi); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case m := <-frames:
			if m.GetHeader().Cause != iec.ActivationTerm {
				continue
			}
		case <-ctx.Done():
			t.Fatal("callback barrier timeout")
		}
		break
	}
	if text := log.text(); strings.Contains(text, "rx ASDU type=C_IC_NA_1(100) cot=ACTIVATION(6) neg=0 test=0 oa=17 ca=2") || strings.Contains(text, "(200)") {
		t.Fatalf("rejection reached RTU: %s", text)
	}

	local := client.LocalAddr().String()
	text := log.text()
	if !strings.Contains(text, "connection request from 127.0.0.1\nconnection opened peer="+local) || !strings.Contains(text, "STARTDT peer="+local) {
		t.Fatalf("endpoint logs: %s", text)
	}
}
