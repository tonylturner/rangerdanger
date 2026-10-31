// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"net"
	"testing"
)

type endpointEvent struct {
	session       *Session
	local, remote net.Addr
}

func endpointHandler(ch chan<- endpointEvent) Handler {
	return func(_ context.Context, e Event) {
		if e.Kind == Opened || e.Kind == Closed {
			ch <- endpointEvent{e.Session, e.Session.LocalAddr(), e.Session.RemoteAddr()}
		}
	}
}

func receiveEndpoint(t *testing.T, ch <-chan endpointEvent) endpointEvent {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-testContext(t).Done():
		t.Fatal("missing endpoint lifecycle event")
		return endpointEvent{}
	}
}

func equalEndpoint(t *testing.T, got, want net.Addr) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Fatalf("address=%v, want %v", got, want)
		}
		return
	}
	if got.Network() != want.Network() || got.String() != want.String() {
		t.Fatalf("address=%s/%s, want %s/%s", got.Network(), got, want.Network(), want)
	}
}

func TestServerClientEndpointAddresses(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	serverEvents, clientEvents := make(chan endpointEvent, 2), make(chan endpointEvent, 2)
	server, err := NewServer(Options{}, endpointHandler(serverEvents))
	if err != nil {
		t.Fatal(err)
	}
	ctx := testContext(t)
	served := make(chan error, 1)
	go func() { served <- server.Serve(ctx, listener) }()
	t.Cleanup(func() {
		if err := server.Shutdown(testContext(t)); err != nil {
			t.Error(err)
		}
		select {
		case err := <-served:
			if err != nil {
				t.Error(err)
			}
		case <-testContext(t).Done():
			t.Error("Serve did not exit")
		}
	})
	client, err := Dial(ctx, listener.Addr().String(), Options{}, endpointHandler(clientEvents))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSession(t, client.Session) })
	remote, local := receiveEndpoint(t, serverEvents), receiveEndpoint(t, clientEvents)
	equalEndpoint(t, remote.local, listener.Addr())
	equalEndpoint(t, local.remote, listener.Addr())
	equalEndpoint(t, remote.remote, client.LocalAddr())
	equalEndpoint(t, remote.local, client.RemoteAddr())
	equalEndpoint(t, local.local, client.LocalAddr())
	equalEndpoint(t, local.remote, client.RemoteAddr())
	if local.session != client.Session {
		t.Fatal("client callback exposed a different session")
	}
	closeSession(t, client.Session)
	closeSession(t, remote.session)
	for _, pair := range [][2]endpointEvent{
		{local, receiveEndpoint(t, clientEvents)},
		{remote, receiveEndpoint(t, serverEvents)},
	} {
		equalEndpoint(t, pair[1].local, pair[0].local)
		equalEndpoint(t, pair[1].remote, pair[0].remote)
		equalEndpoint(t, pair[0].session.LocalAddr(), pair[0].local)
		equalEndpoint(t, pair[0].session.RemoteAddr(), pair[0].remote)
	}
}

func TestSessionPipeEndpointAddresses(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = b.Close() })
	events := make(chan endpointEvent, 2)
	s, err := NewSession(testContext(t), a, ServerRole, Options{}, endpointHandler(events))
	if err != nil {
		_ = a.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSession(t, s) })
	opened := receiveEndpoint(t, events)
	equalEndpoint(t, opened.local, a.LocalAddr())
	equalEndpoint(t, opened.remote, b.LocalAddr())
	closeSession(t, s)
	closed := receiveEndpoint(t, events)
	equalEndpoint(t, closed.local, opened.local)
	equalEndpoint(t, closed.remote, opened.remote)
	equalEndpoint(t, s.LocalAddr(), opened.local)
	equalEndpoint(t, s.RemoteAddr(), opened.remote)
}

type addressedConn struct {
	net.Conn
	local, remote net.Addr
}

func (c *addressedConn) LocalAddr() net.Addr  { return c.local }
func (c *addressedConn) RemoteAddr() net.Addr { return c.remote }

func TestSessionEndpointSnapshot(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "mutable"
		if missing {
			name = "absent"
		}
		t.Run(name, func(t *testing.T) {
			a, b := net.Pipe()
			t.Cleanup(func() { _ = b.Close() })
			local := &net.TCPAddr{IP: net.IP{127, 0, 0, 1}, Port: 1234}
			remote := &net.TCPAddr{IP: net.IP{127, 0, 0, 2}, Port: 2404}
			conn := &addressedConn{Conn: a}
			if !missing {
				conn.local, conn.remote = local, remote
			}
			s, err := NewSession(testContext(t), conn, ServerRole, Options{}, nil)
			if err != nil {
				_ = a.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() { closeSession(t, s) })
			wantLocal, wantRemote := snapshotAddr(conn.local), snapshotAddr(conn.remote)
			local.IP[0], local.Port = 10, 9
			remote.IP[0], remote.Port = 10, 9
			conn.local, conn.remote = nil, nil
			equalEndpoint(t, s.LocalAddr(), wantLocal)
			equalEndpoint(t, s.RemoteAddr(), wantRemote)
			closeSession(t, s)
			equalEndpoint(t, s.LocalAddr(), wantLocal)
			equalEndpoint(t, s.RemoteAddr(), wantRemote)
		})
	}
}
