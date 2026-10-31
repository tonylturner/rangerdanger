// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"net"
)

// Client is one controlling-station session. Dial does not implicitly STARTDT.
// Methods on the embedded Session are safe concurrently; procedure helpers
// serialize transactions per connection rather than guessing response ownership.
type Client struct{ *Session }

// Dial establishes TCP with t0 and an optional local source address. The address
// must include its port (normally 2404). No background reconnect is performed.
func Dial(ctx context.Context, address string, opts Options, handler Handler) (*Client, error) {
	opts, err := opts.normalized()
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: opts.APCI.T0, LocalAddr: opts.LocalAddress}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	s, err := NewSession(ctx, conn, ClientRole, opts, handler)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &Client{s}, nil
}
