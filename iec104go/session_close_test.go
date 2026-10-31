// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionCloseFinalACK(t *testing.T) {
	for _, count := range []int{4, 5} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			peer, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			conn, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			s, err := NewSession(context.Background(), conn, ServerRole, DefaultOptions(), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = peer.Close(); closeSession(t, s) })
			startServerSession(t, s, peer)
			for n := 0; n < count; n++ {
				writeFrame(t, peer, messageFrame(t, uint16(n), 0))
			}
			waitView(t, s, func(v sessionView) bool { return int(v.received) == count })
			closeSession(t, s)
			assertCloseWire(t, peer, []byte{0x68, 4, 1, 0, byte(count * 2), 0})
			waitClosed(t, s)
			if !errors.Is(s.Err(), ErrClosed) {
				t.Fatal(s.Err())
			}
		})
	}
}

// Read through FIN, not just one APDU: this also rejects an extra STOPDT or ACK.
func assertCloseWire(t *testing.T, peer net.Conn, want []byte) {
	t.Helper()
	// net.Pipe rejects deadline updates after the other end closes. Reading
	// still returns EOF, which is exactly what this assertion must verify.
	if err := peer.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	got, err := io.ReadAll(peer)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("last bytes before EOF=%x, error=%v; want %x", got, err, want)
	}
}

func TestSessionCloseNothingPending(t *testing.T) {
	for _, name := range []string{"no-receives", "S-ack", "piggyback"} {
		t.Run(name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.APCI.W = 1
			if name == "piggyback" {
				opts.APCI.W = 8
			}
			s, peer, _ := manualSession(t, ServerRole, opts, nil)
			startServerSession(t, s, peer)
			if name != "no-receives" {
				writeFrame(t, peer, messageFrame(t, 0, 0))
				waitView(t, s, func(v sessionView) bool { return v.received == 1 })
				if name == "piggyback" {
					if err := s.Send(sampleMessage()); err != nil {
						t.Fatal(err)
					}
				}
				if f := readFrame(t, peer); f.Receive != 1 {
					t.Fatal(f)
				}
			}
			s.RequestClose()
			assertCloseWire(t, peer, nil)
			waitClosed(t, s)
		})
	}
}

func TestSessionCloseInflightACK(t *testing.T) {
	for _, name := range []string{"S-ack", "piggyback"} {
		t.Run(name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.APCI.W = 1
			if name == "piggyback" {
				opts.APCI.W = 8
			}
			attempt := make(chan struct{}, 1)
			opts.Tap = func(dir Direction, _ uint64, _ time.Time, b []byte) {
				if dir == Transmit && (b[2] == 1 || b[2]&1 == 0) {
					attempt <- struct{}{}
				}
			}
			s, peer, _ := manualSession(t, ServerRole, opts, nil)
			startServerSession(t, s, peer)
			writeFrame(t, peer, messageFrame(t, 0, 0))
			waitView(t, s, func(v sessionView) bool { return v.received == 1 })
			if name == "piggyback" {
				if err := s.Send(sampleMessage()); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-attempt: // The ACK is claimed, but net.Pipe.Write is blocked.
			case <-time.After(3 * time.Second):
				t.Fatal("no Write attempt")
			}
			waitView(t, s, func(v sessionView) bool { return v.pending == 0 })
			s.RequestClose()
			eventually(t, func() bool { return !s.IsStarted() })
			if f := readFrame(t, peer); f.Receive != 1 {
				t.Fatal(f)
			}
			assertCloseWire(t, peer, nil)
			waitClosed(t, s)
		})
	}
}

func TestSessionCloseQueuedACK(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.W = 1
	attempt := make(chan struct{}, 1)
	opts.Tap = func(dir Direction, _ uint64, _ time.Time, b []byte) {
		if dir == Transmit && b[2]&1 == 0 {
			attempt <- struct{}{}
		}
	}
	s, peer, _ := manualSession(t, ServerRole, opts, nil)
	startServerSession(t, s, peer)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-attempt: // Block an older I-frame, N(R)=0, ahead of the queued S.
	case <-time.After(3 * time.Second):
		t.Fatal("no I-frame Write attempt")
	}
	writeFrame(t, peer, messageFrame(t, 0, 0))
	waitView(t, s, func(v sessionView) bool { return v.received == 1 && v.pending == 0 })
	s.RequestClose()
	eventually(t, func() bool { return !s.IsStarted() })
	if f := readFrame(t, peer); f.Kind != IFrame || f.Receive != 0 {
		t.Fatalf("in-flight I-frame=%+v", f)
	}
	assertCloseWire(t, peer, []byte{0x68, 4, 1, 0, 2, 0})
	waitClosed(t, s)
}

func TestSessionCloseFailedTransport(t *testing.T) {
	var transmissions atomic.Int32
	opts := DefaultOptions()
	opts.Tap = func(dir Direction, _ uint64, _ time.Time, _ []byte) {
		if dir == Transmit {
			transmissions.Add(1)
		}
	}
	s, peer, _ := manualSession(t, ServerRole, opts, nil)
	startServerSession(t, s, peer)
	writeFrame(t, peer, messageFrame(t, 0, 0))
	waitView(t, s, func(v sessionView) bool { return v.received == 1 })
	before := transmissions.Load()
	_ = peer.Close()
	waitClosed(t, s)
	closeSession(t, s)
	if transmissions.Load() != before || !errors.Is(s.Err(), io.EOF) {
		t.Fatalf("attempted final ACK on failed transport: sends=%d, reason=%v", transmissions.Load()-before, s.Err())
	}
}

type closeFailConn struct {
	net.Conn
	fail atomic.Bool
}

func (c *closeFailConn) Write(b []byte) (int, error) {
	if c.fail.Load() {
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(b)
}

func TestSessionCloseFailedWrite(t *testing.T) {
	opts := DefaultOptions()
	var transmissions atomic.Int32
	opts.Tap = func(dir Direction, _ uint64, _ time.Time, _ []byte) {
		if dir == Transmit {
			transmissions.Add(1)
		}
	}
	a, peer := net.Pipe()
	conn := &closeFailConn{Conn: a}
	s, err := NewSession(context.Background(), conn, ServerRole, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close(); closeSession(t, s) })
	startServerSession(t, s, peer)
	writeFrame(t, peer, messageFrame(t, 0, 0))
	waitView(t, s, func(v sessionView) bool { return v.received == 1 })
	conn.fail.Store(true)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, s)
	before := transmissions.Load()
	closeSession(t, s)
	if transmissions.Load() != before || before != 2 || !errors.Is(s.Err(), io.ErrClosedPipe) {
		t.Fatalf("retried ACK after failed Write: sends=%d, reason=%v", transmissions.Load(), s.Err())
	}
	assertCloseWire(t, peer, nil)
}

func TestSessionCloseACKDeadline(t *testing.T) {
	for _, t2 := range []time.Duration{10 * time.Second, 25 * time.Millisecond} {
		t.Run(t2.String(), func(t *testing.T) {
			opts := DefaultOptions()
			opts.APCI.T2 = t2
			attempt := make(chan []byte, 1)
			opts.Tap = func(dir Direction, _ uint64, _ time.Time, b []byte) {
				if dir == Transmit && b[2] == 1 {
					attempt <- b
				}
			}
			s, peer, clock := manualSession(t, ServerRole, opts, nil)
			startServerSession(t, s, peer)
			writeFrame(t, peer, messageFrame(t, 0, 0))
			waitView(t, s, func(v sessionView) bool { return v.received == 1 })
			s.RequestClose()
			select {
			case b := <-attempt:
				if !bytes.Equal(b, []byte{0x68, 4, 1, 0, 2, 0}) {
					t.Fatalf("attempt=%x", b)
				}
			case <-s.Done():
				t.Fatal("closed without attempting pending ACK")
			case <-time.After(3 * time.Second):
				t.Fatal("no flush attempt")
			}
			bound := min(100*time.Millisecond, t2)
			clock.Advance(bound - time.Nanosecond)
			select {
			case <-s.Done():
				t.Fatal("closed before flush deadline")
			default:
			}
			clock.Advance(time.Nanosecond)
			waitClosed(t, s) // No peer Read: Close must interrupt the blocked Write.
			if !errors.Is(s.Err(), ErrClosed) {
				t.Fatal(s.Err())
			}
		})
	}
}
