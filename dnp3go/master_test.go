package dnp3go

import (
	"bytes"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestPollValidatesResponse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dest    uint16
		source  uint16
		control byte
		app     []byte
		valid   bool
	}{
		{"valid", 100, 1, 0x44, []byte{0xc0, 0x81, 0x80, 0, 1, 2, 0, 0, 0, 0x81}, true},
		{"wrong-destination", 101, 1, 0x44, []byte{0xc0, 0x81, 0, 0}, false},
		{"wrong-source", 100, 2, 0x44, []byte{0xc0, 0x81, 0, 0}, false},
		{"wrong-link-control", 100, 1, 0x04, []byte{0xc0, 0x81, 0, 0}, false},
		{"wrong-function", 100, 1, 0x44, []byte{0xc0, 1}, false},
		{"wrong-sequence", 100, 1, 0x44, []byte{0xc1, 0x81, 0, 0}, false},
		{"unsolicited", 100, 1, 0x44, []byte{0xd0, 0x81, 0, 0}, false},
		{"application-fragment", 100, 1, 0x44, []byte{0x80, 0x81, 0, 0}, false},
		{"missing-IIN", 100, 1, 0x44, []byte{0xc0, 0x81}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			done := make(chan error, 1)
			go func() {
				c, err := ln.Accept()
				if err != nil {
					done <- err
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(time.Second))
				f, err := ReadLinkFrame(c)
				if err != nil {
					done <- err
					return
				}
				// The FC01 G60V1 request bytes must not change (DPI contract).
				if f.Control != 0xc4 || f.Dest != 1 || f.Source != 100 ||
					!bytes.Equal(f.Payload, []byte{0xc0, 0xc0, 1, 60, 1, 6}) {
					done <- errors.New("integrity request changed")
					return
				}
				done <- WriteLinkFrame(c, &LinkFrame{
					Control: tc.control, Dest: tc.dest, Source: tc.source,
					Payload: append([]byte{0xc0}, tc.app...),
				})
			}()
			r := Poll(&MasterConfig{MasterAddr: 100, OutstationAddr: 1, Endpoint: ln.Addr().String(), Timeout: time.Second})
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if (r.Error == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, r.Error)
			}
			if tc.valid && (!r.BinaryInputs[0] || r.IIN != 0x8000) {
				t.Fatalf("lost point/restart indication: %+v", r)
			}
			if !tc.valid && (len(r.BinaryInputs) != 0 || r.IIN != 0) {
				t.Fatalf("accepted data from invalid response: %+v", r)
			}
		})
	}
}

func TestPollTimeoutAbortsConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	serverReadErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverReadErr <- err
			return
		}
		defer conn.Close()

		if _, err := ReadLinkFrame(conn); err != nil {
			serverReadErr <- err
			return
		}

		var b [1]byte
		_, err = conn.Read(b[:])
		serverReadErr <- err
	}()

	result := Poll(&MasterConfig{
		MasterAddr:     100,
		OutstationAddr: 1,
		Endpoint:       listener.Addr().String(),
		Timeout:        150 * time.Millisecond,
	})
	if result.Error == nil {
		t.Fatal("Poll succeeded despite the server not answering")
	}
	var netErr net.Error
	if !errors.As(result.Error, &netErr) || !netErr.Timeout() {
		t.Fatalf("Poll error = %v, want timeout", result.Error)
	}

	select {
	case err := <-serverReadErr:
		if err == nil {
			t.Fatal("server read unexpectedly succeeded after the request")
		}
		if errors.Is(err, io.EOF) {
			t.Fatalf("server saw graceful EOF, want connection reset: %v", err)
		}
		if !errors.Is(err, syscall.ECONNRESET) {
			t.Fatalf("server read error = %v, want connection reset", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the server-side reset")
	}
}

func TestWithMasterConnectionClosesGracefullyOnSuccess(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	const request = "master request"
	serverReadErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverReadErr <- err
			return
		}
		defer conn.Close()

		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			serverReadErr <- err
			return
		}
		got := make([]byte, len(request))
		if _, err := io.ReadFull(conn, got); err != nil {
			serverReadErr <- err
			return
		}
		var b [1]byte
		_, err = conn.Read(b[:])
		serverReadErr <- err
	}()

	err = WithMasterConnection(listener.Addr().String(), time.Second, func(conn net.Conn) error {
		_, err := conn.Write([]byte(request))
		return err
	})
	if err != nil {
		t.Fatalf("WithMasterConnection: %v", err)
	}

	select {
	case err := <-serverReadErr:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("server read error = %v, want graceful EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for graceful close")
	}
}
