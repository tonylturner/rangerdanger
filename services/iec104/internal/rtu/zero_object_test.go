// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func TestZeroObjectPolicy(t *testing.T) {
	for _, typ := range []iec.TypeID{45, 46, 47, 100} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			r, _, conn := newModel(t, 1)
			a := &iec.ASDU{Header: iec.Header{Type: typ, Cause: iec.Activation, CommonAddress: 1, Originator: 17}}
			r.receive(conn, a)
			if typ == 100 {
				if !conn.closeRequested || len(conn.messages) != 0 {
					t.Fatal("empty GI must close without a reply")
				}
			} else {
				if conn.closeRequested {
					t.Fatal("empty command closed session")
				}
				response(t, conn, iec.UnknownIOA, true)
				if len(conn.messages) != 1 || conn.messages[0].GetHeader().Count != 0 || conn.messages[0].GetHeader().Originator != 17 {
					t.Fatal("not an empty mirror")
				}
			}
			if r.sel.active || r.pending.active {
				t.Fatal("empty request mutated command state")
			}
		})
	}
}

func TestZeroObjectWire(t *testing.T) {
	for _, typ := range []byte{45, 46, 47, 100} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			pm, err := pointmap.Load("../../check/points.test.json")
			if err != nil {
				t.Fatal(err)
			}
			pm.Timers.W = 1
			r, err := New(pm, Options{Clock: fixedClock{true}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- r.server.Serve(ctx, listener) }()
			defer func() {
				cancel()
				if err := <-done; err != nil {
					t.Error(err)
				}
			}()
			peer, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			if err := peer.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			exchange := func(send, want []byte) {
				t.Helper()
				if _, err := peer.Write(send); err != nil {
					t.Fatal(err)
				}
				got := make([]byte, len(want))
				if _, err := io.ReadFull(peer, got); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("got=%x want=%x err=%v", got, want, err)
				}
			}
			exchange([]byte{0x68, 4, 7, 0, 0, 0}, []byte{0x68, 4, 11, 0, 0, 0})
			exchange([]byte{0x68, 10, 0, 0, 0, 0, typ, 0, 6, 17, 1, 0}, []byte{0x68, 4, 1, 0, 2, 0})
			if typ == 100 {
				rest, err := io.ReadAll(peer)
				if err != nil || len(rest) != 0 {
					t.Fatalf("GI reply before FIN=%x err=%v", rest, err)
				}
			} else {
				want := []byte{0x68, 10, 0, 0, 2, 0, typ, 0, 0x6f, 17, 1, 0}
				got := make([]byte, len(want))
				if _, err := io.ReadFull(peer, got); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("reply=%x err=%v", got, err)
				}
				exchange([]byte{0x68, 4, 0x43, 0, 0, 0}, []byte{0x68, 4, 0x83, 0, 0, 0})
			}
		})
	}
}
