// SPDX-License-Identifier: Apache-2.0

package iec104go

import "net"

// endpointAddr retains values rather than a transport-owned mutable address.
// Its concrete type deliberately exposes no mutable fields to callers.
type endpointAddr struct {
	network string
	address string
}

func (a endpointAddr) Network() string { return a.network }
func (a endpointAddr) String() string  { return a.address }

func snapshotAddr(a net.Addr) net.Addr {
	if a == nil {
		return nil
	}
	return endpointAddr{network: a.Network(), address: a.String()}
}

// RemoteAddr returns an immutable snapshot of the peer's network and address,
// captured before Opened. It is safe in handlers and after Close, and returns
// nil only when the transport supplies no address. Its concrete type is not
// the transport's address type; use Network and String.
func (s *Session) RemoteAddr() net.Addr { return s.remoteAddr }

// LocalAddr returns an immutable snapshot of the local network and address,
// with the same lifetime and concrete-type contract as RemoteAddr.
func (s *Session) LocalAddr() net.Addr { return s.localAddr }
