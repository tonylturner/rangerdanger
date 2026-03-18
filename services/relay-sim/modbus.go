package main

// Modbus TCP server for the relay simulator. Exposes breaker state
// as standard Modbus registers and supports write operations — this
// is the primary attack surface for ICS penetration testing.
//
// Register Map:
//
//   Coils (FC1 read / FC5 write, 0-based):
//     0: breaker_closed   (write 0x0000 = trip/open, 0xFF00 = close)
//     1: remote_control_enabled (read-only)
//     2: lockout           (read-only)
//     3: fault_seen        (read-only)
//
//   Holding Registers (FC3 read / FC6 write, 0-based):
//     0: breaker_closed    (write 0 = trip, 1 = close)
//     1: remote_control_enabled (read-only)
//     2: lockout           (read-only)
//
//   Input Registers (FC4 read-only, 0-based):
//     0: measured_current_a * 10
//     1: measured_voltage_kv * 100

import (
	"encoding/binary"
	"io"
	"log"
	"net"

	"github.com/tturner/rangerdanger/services/shared"
)

const (
	modbusPort = ":502"

	fcReadCoils            = 0x01
	fcReadHoldingRegisters = 0x03
	fcReadInputRegisters   = 0x04
	fcWriteSingleCoil      = 0x05
	fcWriteSingleRegister  = 0x06
)

func startModbusServer() {
	ln, err := net.Listen("tcp", modbusPort)
	if err != nil {
		log.Printf("Modbus TCP server failed to start: %v", err)
		return
	}
	log.Printf("Modbus TCP server listening on %s", modbusPort)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Modbus accept error: %v", err)
			continue
		}
		go handleModbusConn(conn)
	}
}

func handleModbusConn(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 260)

	for {
		_, err := io.ReadFull(conn, buf[:7])
		if err != nil {
			return
		}

		transID := binary.BigEndian.Uint16(buf[0:2])
		protoID := binary.BigEndian.Uint16(buf[2:4])
		length := binary.BigEndian.Uint16(buf[4:6])

		if protoID != 0 || length < 2 || length > 253 {
			return
		}

		pduLen := int(length) - 1
		_, err = io.ReadFull(conn, buf[7:7+pduLen])
		if err != nil {
			return
		}

		fc := buf[7]
		var resp []byte

		switch fc {
		case fcReadCoils:
			resp = handleReadCoils(buf[8 : 7+pduLen])
		case fcReadHoldingRegisters:
			resp = handleReadHoldingRegisters(buf[8 : 7+pduLen])
		case fcReadInputRegisters:
			resp = handleReadInputRegisters(buf[8 : 7+pduLen])
		case fcWriteSingleCoil:
			resp = handleWriteSingleCoil(buf[8 : 7+pduLen])
		case fcWriteSingleRegister:
			resp = handleWriteSingleRegister(buf[8 : 7+pduLen])
		default:
			resp = []byte{fc | 0x80, 0x01}
		}

		respBuf := make([]byte, 7+len(resp))
		binary.BigEndian.PutUint16(respBuf[0:2], transID)
		binary.BigEndian.PutUint16(respBuf[2:4], 0)
		binary.BigEndian.PutUint16(respBuf[4:6], uint16(1+len(resp)))
		respBuf[6] = 1
		copy(respBuf[7:], resp)

		conn.Write(respBuf)
	}
}

// ── Read helpers ──────────────────────────────────────────────────

func getCoils() []bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return []bool{
		state.BreakerClosed,        // coil 0
		state.RemoteControlEnabled, // coil 1
		state.Lockout,              // coil 2
		state.FaultSeen,            // coil 3
	}
}

func getHoldingRegisters() []uint16 {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return []uint16{
		boolToUint16(state.BreakerClosed),        // reg 0
		boolToUint16(state.RemoteControlEnabled),  // reg 1
		boolToUint16(state.Lockout),               // reg 2
	}
}

func getInputRegisters() []uint16 {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return []uint16{
		uint16(state.MeasuredCurrent * 10),  // reg 0
		uint16(state.MeasuredVoltage * 100), // reg 1
	}
}

func boolToUint16(b bool) uint16 {
	if b {
		return 1
	}
	return 0
}

// ── FC1: Read Coils ───────────────────────────────────────────────

func handleReadCoils(data []byte) []byte {
	if len(data) < 4 {
		return []byte{fcReadCoils | 0x80, 0x03}
	}
	startAddr := binary.BigEndian.Uint16(data[0:2])
	quantity := binary.BigEndian.Uint16(data[2:4])

	coils := getCoils()
	if int(startAddr+quantity) > len(coils) {
		return []byte{fcReadCoils | 0x80, 0x02}
	}

	byteCount := (quantity + 7) / 8
	resp := make([]byte, 2+byteCount)
	resp[0] = fcReadCoils
	resp[1] = byte(byteCount)

	for i := uint16(0); i < quantity; i++ {
		if coils[startAddr+i] {
			resp[2+i/8] |= 1 << (i % 8)
		}
	}
	return resp
}

// ── FC3: Read Holding Registers ───────────────────────────────────

func handleReadHoldingRegisters(data []byte) []byte {
	if len(data) < 4 {
		return []byte{fcReadHoldingRegisters | 0x80, 0x03}
	}
	startAddr := binary.BigEndian.Uint16(data[0:2])
	quantity := binary.BigEndian.Uint16(data[2:4])

	regs := getHoldingRegisters()
	if int(startAddr+quantity) > len(regs) {
		return []byte{fcReadHoldingRegisters | 0x80, 0x02}
	}

	resp := make([]byte, 2+quantity*2)
	resp[0] = fcReadHoldingRegisters
	resp[1] = byte(quantity * 2)

	for i := uint16(0); i < quantity; i++ {
		binary.BigEndian.PutUint16(resp[2+i*2:], regs[startAddr+i])
	}
	return resp
}

// ── FC4: Read Input Registers ─────────────────────────────────────

func handleReadInputRegisters(data []byte) []byte {
	if len(data) < 4 {
		return []byte{fcReadInputRegisters | 0x80, 0x03}
	}
	startAddr := binary.BigEndian.Uint16(data[0:2])
	quantity := binary.BigEndian.Uint16(data[2:4])

	regs := getInputRegisters()
	if int(startAddr+quantity) > len(regs) {
		return []byte{fcReadInputRegisters | 0x80, 0x02}
	}

	resp := make([]byte, 2+quantity*2)
	resp[0] = fcReadInputRegisters
	resp[1] = byte(quantity * 2)

	for i := uint16(0); i < quantity; i++ {
		binary.BigEndian.PutUint16(resp[2+i*2:], regs[startAddr+i])
	}
	return resp
}

// ── FC5: Write Single Coil ────────────────────────────────────────

func handleWriteSingleCoil(data []byte) []byte {
	if len(data) < 4 {
		return []byte{fcWriteSingleCoil | 0x80, 0x03}
	}
	coilAddr := binary.BigEndian.Uint16(data[0:2])
	value := binary.BigEndian.Uint16(data[2:4])

	// Only coil 0 (breaker_closed) is writable
	if coilAddr != 0 {
		return []byte{fcWriteSingleCoil | 0x80, 0x02}
	}

	if value != 0xFF00 && value != 0x0000 {
		return []byte{fcWriteSingleCoil | 0x80, 0x03}
	}

	closedVal := value == 0xFF00

	state.mu.Lock()
	if !state.RemoteControlEnabled {
		state.mu.Unlock()
		log.Printf("MODBUS FC5 write coil 0 REJECTED: remote control disabled")
		audit.Add(shared.AuditEntry{
			Source:  "modbus-tcp",
			Target:  "relay-sim",
			Command: "write_coil_breaker",
			Result:  "rejected",
			Detail:  "remote control disabled",
		})
		return []byte{fcWriteSingleCoil | 0x80, 0x04} // server device failure
	}
	if state.Lockout {
		state.mu.Unlock()
		log.Printf("MODBUS FC5 write coil 0 REJECTED: lockout active")
		audit.Add(shared.AuditEntry{
			Source:  "modbus-tcp",
			Target:  "relay-sim",
			Command: "write_coil_breaker",
			Result:  "rejected",
			Detail:  "lockout active",
		})
		return []byte{fcWriteSingleCoil | 0x80, 0x04}
	}

	state.BreakerClosed = closedVal
	state.LastCommandSource = "modbus-tcp"
	state.mu.Unlock()

	action := "TRIP"
	detail := "breaker OPENED via Modbus FC5 Write Coil"
	if closedVal {
		action = "CLOSE"
		detail = "breaker CLOSED via Modbus FC5 Write Coil"
	}
	log.Printf("MODBUS FC5 %s — breaker_closed=%v", action, closedVal)

	audit.Add(shared.AuditEntry{
		Source:  "modbus-tcp",
		Target:  "relay-sim",
		Command: "write_coil_breaker",
		Result:  "executed",
		Detail:  detail,
	})

	resp := make([]byte, 5)
	resp[0] = fcWriteSingleCoil
	binary.BigEndian.PutUint16(resp[1:3], coilAddr)
	binary.BigEndian.PutUint16(resp[3:5], value)
	return resp
}

// ── FC6: Write Single Register ────────────────────────────────────

func handleWriteSingleRegister(data []byte) []byte {
	if len(data) < 4 {
		return []byte{fcWriteSingleRegister | 0x80, 0x03}
	}
	regAddr := binary.BigEndian.Uint16(data[0:2])
	value := binary.BigEndian.Uint16(data[2:4])

	// Only reg 0 (breaker_closed) is writable
	if regAddr != 0 {
		return []byte{fcWriteSingleRegister | 0x80, 0x02}
	}

	closedVal := value != 0

	state.mu.Lock()
	if !state.RemoteControlEnabled {
		state.mu.Unlock()
		log.Printf("MODBUS FC6 write reg 0 REJECTED: remote control disabled")
		audit.Add(shared.AuditEntry{
			Source:  "modbus-tcp",
			Target:  "relay-sim",
			Command: "write_reg_breaker",
			Result:  "rejected",
			Detail:  "remote control disabled",
		})
		return []byte{fcWriteSingleRegister | 0x80, 0x04}
	}
	if state.Lockout {
		state.mu.Unlock()
		log.Printf("MODBUS FC6 write reg 0 REJECTED: lockout active")
		audit.Add(shared.AuditEntry{
			Source:  "modbus-tcp",
			Target:  "relay-sim",
			Command: "write_reg_breaker",
			Result:  "rejected",
			Detail:  "lockout active",
		})
		return []byte{fcWriteSingleRegister | 0x80, 0x04}
	}

	state.BreakerClosed = closedVal
	state.LastCommandSource = "modbus-tcp"
	state.mu.Unlock()

	action := "TRIP"
	detail := "breaker OPENED via Modbus FC6 Write Register"
	if closedVal {
		action = "CLOSE"
		detail = "breaker CLOSED via Modbus FC6 Write Register"
	}
	log.Printf("MODBUS FC6 %s — breaker_closed=%v", action, closedVal)

	audit.Add(shared.AuditEntry{
		Source:  "modbus-tcp",
		Target:  "relay-sim",
		Command: "write_reg_breaker",
		Result:  "executed",
		Detail:  detail,
	})

	resp := make([]byte, 5)
	resp[0] = fcWriteSingleRegister
	binary.BigEndian.PutUint16(resp[1:3], regAddr)
	binary.BigEndian.PutUint16(resp[3:5], value)
	return resp
}
