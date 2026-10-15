package dnp3go

import (
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
	"time"
)

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
