package main

// Modbus TCP server for the regulator simulator. Exposes tap position,
// voltage, and mode as standard Modbus registers and supports write
// operations for remote tap control.
//
// Register Map:
//
//   Coils (FC1 read / FC5 write, 0-based):
//     0: manual_mode
//     1: alarm (read-only)
//
//   Holding Registers (FC3 read / FC6 write, 0-based):
//     0: tap_position (signed int16)
//     1: manual_mode (0/1)
//     2: voltage_setpoint * 10
//
//   Input Registers (FC4 read-only, 0-based):
//     0: tap_position (signed int16)
//     1: voltage_offset * 10  (tap * voltsPerTap * 10)

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/tturner/rangerdanger/services/shared"
)

const (
	modbusPort = ":502"

	// Function codes
	fcReadCoils            = 0x01
	fcReadHoldingRegisters = 0x03
	fcReadInputRegisters   = 0x04
	fcWriteSingleCoil      = 0x05
	fcWriteSingleRegister  = 0x06
)

// startModbusServer launches a Modbus TCP server on port 502.
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
	buf := make([]byte, 260) // max Modbus TCP ADU

	for {
		// Read MBAP header (7 bytes)
		_, err := io.ReadFull(conn, buf[:7])
		if err != nil {
			return
		}

		transID := binary.BigEndian.Uint16(buf[0:2])
		protoID := binary.BigEndian.Uint16(buf[2:4])
		length := binary.BigEndian.Uint16(buf[4:6])
		// unitID := buf[6]

		if protoID != 0 || length < 2 || length > 253 {
			return
		}

		// Read PDU
		pduLen := int(length) - 1 // minus unit ID already read
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
			// Exception: illegal function
			resp = []byte{fc | 0x80, 0x01}
		}

		// Build response MBAP header
		respBuf := make([]byte, 7+len(resp))
		binary.BigEndian.PutUint16(respBuf[0:2], transID)
		binary.BigEndian.PutUint16(respBuf[2:4], 0) // protocol ID
		binary.BigEndian.PutUint16(respBuf[4:6], uint16(1+len(resp)))
		respBuf[6] = 1 // unit ID
		copy(respBuf[7:], resp)

		conn.Write(respBuf)
	}
}

// ── Read helpers ──────────────────────────────────────────────────

func getCoils() []bool {
	state.mu.RLock()
	defer state.mu.RUnlock()

	return []bool{
		state.ManualMode, // coil 0
		state.Alarm,      // coil 1
	}
}

func getHoldingRegisters() []uint16 {
	state.mu.RLock()
	defer state.mu.RUnlock()

	regs := make([]uint16, 3)
	regs[0] = uint16(int16(state.TapPosition))              // signed
	regs[1] = boolToUint16(state.ManualMode)                 // 0 or 1
	regs[2] = uint16(state.VoltageSetpoint * 10)             // scaled x10
	return regs
}

func getInputRegisters() []uint16 {
	state.mu.RLock()
	defer state.mu.RUnlock()

	regs := make([]uint16, 2)
	regs[0] = uint16(int16(state.TapPosition))                          // signed
	regs[1] = uint16(float64(state.TapPosition) * voltsPerTap * 10)     // voltage_offset x10
	return regs
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
		return []byte{fcReadCoils | 0x80, 0x02} // illegal data address
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

	// Only coil 0 (manual_mode) is writable
	if coilAddr != 0 {
		return []byte{fcWriteSingleCoil | 0x80, 0x02} // illegal data address
	}

	// Modbus spec: 0xFF00 = ON, 0x0000 = OFF
	if value != 0xFF00 && value != 0x0000 {
		return []byte{fcWriteSingleCoil | 0x80, 0x03} // illegal data value
	}

	manualOn := value == 0xFF00

	state.mu.Lock()
	state.ManualMode = manualOn
	state.LastCommandSource = "modbus"
	state.mu.Unlock()

	detail := "auto mode enabled via Modbus"
	if manualOn {
		detail = "manual mode enabled via Modbus"
	}
	log.Printf("MODBUS FC5 write coil 0: manual_mode=%v", manualOn)

	audit.Add(shared.AuditEntry{
		Source:  "modbus",
		Target:  "regulator-sim",
		Command: "write_coil_manual_mode",
		Result:  "executed",
		Detail:  detail,
	})

	// Echo request back per Modbus spec
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

	switch regAddr {
	case 0: // tap_position (signed int16)
		tap := int(int16(value))
		if tap < minTap || tap > maxTap {
			log.Printf("MODBUS FC6 write reg 0: tap %d out of range [%d..%d]", tap, minTap, maxTap)
			return []byte{fcWriteSingleRegister | 0x80, 0x03} // illegal data value
		}

		state.mu.Lock()
		state.TapPosition = tap
		state.LastCommandSource = "modbus"
		estimatedV := 117.6 + float64(tap)*voltsPerTap
		state.Alarm = estimatedV < 108.0 || estimatedV > 132.0
		alarm := state.Alarm
		state.mu.Unlock()

		detail := fmt.Sprintf("tap set to %d via Modbus (est. %.1fV)", tap, estimatedV)
		if alarm {
			detail = fmt.Sprintf("tap set to %d via Modbus — VOLTAGE ALARM: est. %.1fV", tap, estimatedV)
		}
		log.Printf("MODBUS FC6 write reg 0: tap=%d est=%.1fV alarm=%v", tap, estimatedV, alarm)

		audit.Add(shared.AuditEntry{
			Source:  "modbus",
			Target:  "regulator-sim",
			Command: "write_reg_tap_position",
			Result:  "executed",
			Detail:  detail,
		})

	case 1: // manual_mode (0 = auto, 1 = manual)
		manualOn := value != 0

		state.mu.Lock()
		state.ManualMode = manualOn
		state.LastCommandSource = "modbus"
		state.mu.Unlock()

		detail := "auto mode enabled via Modbus"
		if manualOn {
			detail = "manual mode enabled via Modbus"
		}
		log.Printf("MODBUS FC6 write reg 1: manual_mode=%v", manualOn)

		audit.Add(shared.AuditEntry{
			Source:  "modbus",
			Target:  "regulator-sim",
			Command: "write_reg_manual_mode",
			Result:  "executed",
			Detail:  detail,
		})

	default:
		return []byte{fcWriteSingleRegister | 0x80, 0x02} // illegal data address
	}

	// Echo request back per Modbus spec
	resp := make([]byte, 5)
	resp[0] = fcWriteSingleRegister
	binary.BigEndian.PutUint16(resp[1:3], regAddr)
	binary.BigEndian.PutUint16(resp[3:5], value)
	return resp
}
