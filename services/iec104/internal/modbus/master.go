// SPDX-License-Identifier: Apache-2.0

// Package modbus implements the RTU's single-owner FC3/FC4/FC6 TCP master.
// A Master is owned by one device worker and is not safe for concurrent use.
package modbus

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"net"
	"strconv"
	"time"
)

// Status keeps the reference master's distinction between an explicit device
// rejection and an ambiguous write outcome. Never retry a write after Timeout.
type Status uint8

const (
	OK Status = iota
	Timeout
	IOError
	Exception
)

// Result describes one request; ExceptionCode is nonzero only for exceptions.
type Result struct {
	Status        Status
	ExceptionCode byte
}

// Master owns one connection, resolved addresses and wrapping transaction ID.
type Master struct {
	unit      byte
	txid      uint16
	addresses []string
	conn      net.Conn
}

// New creates a disconnected master.
func New(unit byte) *Master { return &Master{unit: unit} }

// Resolve performs DNS on the owning device worker, not on every reconnect.
func (m *Master) Resolve(ctx context.Context, host string, port int) Status {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return IOError
	}
	m.addresses = nil
	for _, a := range addresses {
		h := a.IP.String()
		if a.Zone != "" {
			h += "%" + a.Zone
		}
		m.addresses = append(m.addresses, net.JoinHostPort(h, strconv.Itoa(port)))
	}
	return OK
}

// Connect tries all resolved addresses within one total deadline.
func (m *Master) Connect(timeout time.Duration) Status {
	m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	st := IOError
	for _, address := range m.addresses {
		if ctx.Err() != nil {
			return Timeout
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				st = Timeout
			}
			continue
		}
		m.conn = conn
		return OK
	}
	return st
}

// Close releases the transport without discarding cached addresses or txid.
func (m *Master) Close() {
	if m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
}

// IsConnected reports whether a transport is owned, not peer liveness.
func (m *Master) IsConnected() bool { return m.conn != nil }

func (m *Master) transact(pdu []byte, timeout time.Duration) ([]byte, Result) {
	if m.conn == nil {
		return nil, Result{Status: IOError}
	}
	m.txid++
	req := make([]byte, 7+len(pdu))
	binary.BigEndian.PutUint16(req, m.txid)
	binary.BigEndian.PutUint16(req[4:], uint16(len(pdu)+1))
	req[6] = m.unit
	copy(req[7:], pdu)
	if err := m.conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, Result{Status: IOError}
	}
	for rest := req; len(rest) != 0; {
		n, err := m.conn.Write(rest)
		if err != nil || n == 0 {
			return nil, Result{Status: Timeout}
		}
		rest = rest[n:]
	}
	var hdr [7]byte
	if _, err := io.ReadFull(m.conn, hdr[:]); err != nil {
		return nil, Result{Status: Timeout}
	}
	n := int(binary.BigEndian.Uint16(hdr[4:]))
	if binary.BigEndian.Uint16(hdr[:]) != m.txid || binary.BigEndian.Uint16(hdr[2:]) != 0 ||
		n < 2 || n > 254 || hdr[6] != m.unit {
		return nil, Result{Status: IOError}
	}
	response := make([]byte, n-1)
	if _, err := io.ReadFull(m.conn, response); err != nil {
		return nil, Result{Status: Timeout}
	}
	return response, Result{Status: OK}
}

func (m *Master) read(fc byte, address, count int, timeout time.Duration) ([]uint16, Result) {
	if count < 1 || count > 125 {
		return nil, Result{Status: IOError}
	}
	pdu := []byte{fc, byte(address >> 8), byte(address), byte(count >> 8), byte(count)}
	r, result := m.transact(pdu, timeout)
	if result.Status != OK {
		return nil, result
	}
	if len(r) >= 2 && r[0] == fc|0x80 {
		return nil, Result{Status: Exception, ExceptionCode: r[1]}
	}
	if len(r) != 2+count*2 || r[0] != fc || int(r[1]) != count*2 {
		return nil, Result{Status: IOError}
	}
	out := make([]uint16, count)
	for i := range out {
		out[i] = binary.BigEndian.Uint16(r[2+i*2:])
	}
	return out, Result{Status: OK}
}

// ReadHolding reads FC3 registers.
func (m *Master) ReadHolding(address, count int, timeout time.Duration) ([]uint16, Result) {
	return m.read(3, address, count, timeout)
}

// ReadInput reads FC4 registers.
func (m *Master) ReadInput(address, count int, timeout time.Duration) ([]uint16, Result) {
	return m.read(4, address, count, timeout)
}

// WriteSingle issues FC6 exactly once and requires the complete matching echo.
func (m *Master) WriteSingle(address int, value uint16, timeout time.Duration) Result {
	pdu := []byte{6, byte(address >> 8), byte(address), byte(value >> 8), byte(value)}
	r, result := m.transact(pdu, timeout)
	if result.Status != OK {
		return result
	}
	if len(r) >= 2 && r[0] == 0x86 {
		return Result{Status: Exception, ExceptionCode: r[1]}
	}
	if len(r) != 5 || r[0] != 6 || binary.BigEndian.Uint16(r[1:]) != uint16(address) || binary.BigEndian.Uint16(r[3:]) != value {
		return Result{Status: IOError}
	}
	return Result{Status: OK}
}

// RegistersToFloat combines IEEE-754 registers with the high word first.
func RegistersToFloat(hi, lo uint16) float32 {
	return math.Float32frombits(uint32(hi)<<16 | uint32(lo))
}
