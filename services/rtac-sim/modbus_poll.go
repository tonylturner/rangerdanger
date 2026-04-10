package main

// Autonomous Modbus master polling — the RTAC continuously reads holding
// registers from each field device via Modbus TCP on port 502. Traffic
// originates from the RTAC's OT Ops interface (10.30.30.20) and transits
// the containd firewall because rtac-harden.sh installs an indirect route
// for 10.40.40.0/24 via the OT Ops firewall.
//
// This is a hand-written Modbus TCP client (no external library). We
// issue a single FC3 Read Holding Registers request per target per
// interval and discard the response. Protocol-wise this produces:
//
//   TCP SYN → SYN-ACK → ACK            (3-way handshake)
//   PSH (request 12 bytes)
//   PSH (response, variable length)
//   FIN/ACK close                       (connection teardown)
//
// That's ~8-10 packets per poll per device, creating a clean, visible
// Modbus TCP conversation in the firewall capture.

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"time"
)

// modbusTarget is a field device the RTAC polls via Modbus TCP.
type modbusTarget struct {
	name string
	host string // "host:port"
}

// modbusReadHolding performs a single Modbus TCP FC3 Read Holding Registers
// request and discards the response. Any network or protocol error is
// returned so the caller can log and move on.
func modbusReadHolding(host string, unitID byte, startAddr, count uint16) error {
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// MBAP header (7 bytes) + PDU (5 bytes for FC3 request)
	frame := make([]byte, 12)
	binary.BigEndian.PutUint16(frame[0:], 1) // transaction id
	binary.BigEndian.PutUint16(frame[2:], 0) // protocol id (Modbus)
	binary.BigEndian.PutUint16(frame[4:], 6) // length of (unit_id + fc + 4 bytes data)
	frame[6] = unitID
	frame[7] = 0x03 // FC3 Read Holding Registers
	binary.BigEndian.PutUint16(frame[8:], startAddr)
	binary.BigEndian.PutUint16(frame[10:], count)

	if _, err := conn.Write(frame); err != nil {
		return err
	}

	// Read response. Minimum is MBAP(7) + unit_id(1) + fc(1) + byte_count(1) = 10,
	// then byte_count bytes of register data.
	header := make([]byte, 9)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	byteCount := int(header[8])
	if byteCount > 0 && byteCount <= 250 {
		data := make([]byte, byteCount)
		if _, err := io.ReadFull(conn, data); err != nil {
			return err
		}
	}
	return nil
}

// pollModbusDevices is the RTAC's autonomous Modbus master loop.
// It runs as a goroutine kicked off from main() and polls every
// field device at a steady interval, producing consistent Modbus
// traffic on the wire without any external trigger.
func pollModbusDevices() {
	// Delay start so field device Modbus servers are up
	time.Sleep(8 * time.Second)

	targets := []modbusTarget{
		{"relay", "10.40.40.20:502"},
		{"recloser", "10.40.40.21:502"},
		{"regulator", "10.40.40.22:502"},
		{"capbank", "10.40.40.23:502"},
	}

	interval := 3 * time.Second
	log.Printf("Modbus master polling started (interval=%s, targets=%d)", interval, len(targets))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		for _, t := range targets {
			// FC3 Read Holding Registers, unit 1, start 0, count 10.
			// Field devices ignore unit_id anyway — their Go-based
			// Modbus servers serve a single register bank.
			if err := modbusReadHolding(t.host, 0x01, 0, 10); err != nil {
				// Silently skip on error; HTTP polling handles comms state
				continue
			}
		}
	}
}
