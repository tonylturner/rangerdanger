// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
)

type zeroLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *zeroLog) Printf(f string, a ...any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(f, a...))
	return nil
}
func (l *zeroLog) text() string { l.mu.Lock(); defer l.mu.Unlock(); return strings.Join(l.lines, "\n") }

func TestZeroObjectMonitorPolicy(t *testing.T) {
	c, _ := model(t)
	log := &zeroLog{}
	c.opts.Logger = log
	started(c)
	receive(c, 3, 20, 1001, iec.DoublePoint{DIQ: 2})
	before := append([]cached(nil), c.points...)
	c.handle(context.Background(), iec.Event{Kind: iec.ASDUReceived, ASDU: &iec.ASDU{Header: iec.Header{Type: 3, Cause: 3, CommonAddress: 1}}})
	if !reflect.DeepEqual(before, c.points) || c.link.state != "started" {
		t.Fatal("empty monitor changed point/link state")
	}
	if !strings.Contains(log.text(), "ignored zero-object ASDU type=3 ca=1") {
		t.Fatal(log.text())
	}
}

func TestZeroObjectMonitorWire(t *testing.T) {
	c, _ := model(t)
	log := &zeroLog{}
	c.opts.Logger = log
	c.pm.Timers.W = 1
	local, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := iec.NewSession(ctx, local, iec.ClientRole, c.protocolOptions("127.0.0.1"), c.handle)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		peer.Close()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err := peer.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// STARTDT is initiated by the client, not by this scripted station.
	started := make(chan error, 1)
	go func() { started <- s.StartDT(ctx) }()
	got := make([]byte, 6)
	if _, err := io.ReadFull(peer, got); err != nil || !bytes.Equal(got, []byte{0x68, 4, 7, 0, 0, 0}) {
		t.Fatalf("start=%x %v", got, err)
	}
	if _, err := peer.Write([]byte{0x68, 4, 11, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := <-started; err != nil {
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
	exchange([]byte{0x68, 10, 0, 0, 0, 0, 3, 0, 3, 0, 1, 0}, []byte{0x68, 4, 1, 0, 2, 0})
	exchange([]byte{0x68, 4, 0x43, 0, 0, 0}, []byte{0x68, 4, 0x83, 0, 0, 0})
	// Joining dispatch makes the log assertion independent of writer timing.
	peer.Close()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.text(), "ignored zero-object ASDU") {
		t.Fatal(log.text())
	}
}
