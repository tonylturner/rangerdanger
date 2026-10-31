// SPDX-License-Identifier: Apache-2.0

package modbus

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"net"
	"slices"
	"testing"
	"time"
)

func TestTransactions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fc     byte
		body   []byte
		mutate func([]byte)
		want   Status
		code   byte
	}{
		{"input", 4, []byte{4, 4, 0x12, 0x34, 0xab, 0xcd}, nil, OK, 0},
		{"holding", 3, []byte{3, 4, 0x12, 0x34, 0xab, 0xcd}, nil, OK, 0},
		{"write", 6, []byte{6, 0, 17, 0, 42}, nil, OK, 0},
		{"exception-read", 4, []byte{0x84, 2}, nil, Exception, 2},
		{"exception-write", 6, []byte{0x86, 3}, nil, Exception, 3},
		{"bad-echo-address", 6, []byte{6, 0, 18, 0, 42}, nil, IOError, 0},
		{"bad-echo-value", 6, []byte{6, 0, 17, 0, 43}, nil, IOError, 0},
		{"bad-echo-fc", 6, []byte{5, 0, 17, 0, 42}, nil, IOError, 0},
		{"bad-count", 4, []byte{4, 2, 0x12, 0x34}, nil, IOError, 0},
		{"bad-fc", 4, []byte{3, 4, 0x12, 0x34, 0xab, 0xcd}, nil, IOError, 0},
		{"short", 4, []byte{4}, nil, IOError, 0},
		{"txid", 4, nil, func(b []byte) { b[1]++ }, IOError, 0},
		{"protocol", 4, nil, func(b []byte) { b[3]++ }, IOError, 0},
		{"unit", 4, nil, func(b []byte) { b[6]++ }, IOError, 0},
		{"length-low", 4, nil, func(b []byte) { b[5] = 1 }, IOError, 0},
		{"length-high", 4, nil, func(b []byte) { b[4] = 1 }, IOError, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			m := New(7)
			m.conn = a
			t.Cleanup(m.Close)
			done := make(chan error, 1)
			go func() {
				defer b.Close()
				if err := b.SetDeadline(time.Now().Add(time.Second)); err != nil {
					done <- err
					return
				}
				req := make([]byte, 12)
				_, err := io.ReadFull(b, req)
				if err != nil {
					done <- err
					return
				}
				want := []byte{0, 1, 0, 0, 0, 6, 7, tc.fc, 0, 17, 0, 2}
				if tc.fc == 6 {
					want[11] = 42
				}
				if !slices.Equal(req, want) {
					done <- &requestError{req}
					return
				}
				body := tc.body
				if body == nil {
					body = []byte{4, 4, 0x12, 0x34, 0xab, 0xcd}
				}
				resp := append([]byte(nil), req[:7]...)
				binary.BigEndian.PutUint16(resp[4:], uint16(len(body)+1))
				if tc.mutate != nil {
					tc.mutate(resp)
				}
				// Separate header and body writes exercise stream reassembly.
				if _, err = b.Write(resp); err == nil && tc.mutate == nil {
					_, err = b.Write(body)
				}
				done <- err
			}()
			var result Result
			var registers []uint16
			switch tc.fc {
			case 3:
				registers, result = m.ReadHolding(17, 2, time.Second)
			case 4:
				registers, result = m.ReadInput(17, 2, time.Second)
			case 6:
				result = m.WriteSingle(17, 42, time.Second)
			}
			if result != (Result{tc.want, tc.code}) {
				t.Fatalf("got %+v", result)
			}
			if tc.want == OK && tc.fc != 6 && !slices.Equal(registers, []uint16{0x1234, 0xabcd}) {
				t.Fatal(registers)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

type requestError struct{ b []byte }

func (e *requestError) Error() string { return "unexpected request bytes" }

func TestAmbiguousWriteNoRetry(t *testing.T) {
	a, b := net.Pipe()
	m := New(1)
	m.conn = a
	t.Cleanup(m.Close)
	done := make(chan error, 1)
	go func() {
		defer b.Close()
		if err := b.SetDeadline(time.Now().Add(time.Second)); err != nil {
			done <- err
			return
		}
		req := make([]byte, 12)
		_, err := io.ReadFull(b, req)
		if err != nil {
			done <- err
			return
		}
		// Drop the echo after receiving the complete, possibly applied write.
		done <- nil
	}()
	if got := m.WriteSingle(0, 2, 100*time.Millisecond); got.Status != Timeout {
		t.Fatal(got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConnectAndRollover(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	addr := l.Addr().(*net.TCPAddr)
	m := New(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if m.Resolve(ctx, "127.0.0.1", addr.Port) != OK || m.Connect(time.Second) != OK {
		t.Fatal("connect")
	}
	t.Cleanup(m.Close)
	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	done := make(chan error, 1)
	go func() {
		if err := c.SetDeadline(time.Now().Add(time.Second)); err != nil {
			done <- err
			return
		}
		req := make([]byte, 12)
		if _, err := io.ReadFull(c, req); err != nil {
			done <- err
			return
		}
		if req[0] != 0 || req[1] != 0 {
			done <- &requestError{req}
			return
		}
		_, err := c.Write(req)
		done <- err
	}()
	m.txid = 65535
	if got := m.WriteSingle(0, 2, time.Second); got.Status != OK {
		t.Fatal(got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	m.Close()
	if m.IsConnected() {
		t.Fatal("still connected")
	}
	if _, got := m.ReadInput(0, 1, time.Second); got.Status != IOError {
		t.Fatal(got)
	}
	if _, got := m.ReadInput(0, 126, time.Second); got.Status != IOError {
		t.Fatal(got)
	}
	for _, v := range []uint32{0x3f800000, 0xc0200000, 0x7fc00000} {
		f := RegistersToFloat(uint16(v>>16), uint16(v))
		if math.Float32bits(f) != v {
			t.Fatal(f)
		}
	}
}
