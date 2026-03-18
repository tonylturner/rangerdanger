package main

// Modbus TCP server for the GPS time server simulator.
//
// Register Map:
//
//   Coils (FC1 read / FC5 write, 0-based):
//     0: ntp_enabled
//     1: ptp_enabled
//     2: irig_b_output (read-only)
//     3: alarm (read-only)
//
//   Holding Registers (FC3 read / FC6 write, 0-based):
//     0: time_offset_sec * 1000 (signed int16, milliseconds)
//     1: ntp_enabled (0/1)
//     2: ptp_enabled (0/1)
//
//   Input Registers (FC4 read-only, 0-based):
//     0: sync_status (0=locked, 1=holdover, 2=freerun)
//     1: satellite_count
//     2: time_offset_ms (absolute, unsigned)
//     3: holdover_drift_ppm * 100

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
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

func getCoils() []bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return []bool{
		state.NTPEnabled,
		state.PTPEnabled,
		state.IRIGB,
		state.Alarm,
	}
}

func getHoldingRegisters() []uint16 {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return []uint16{
		uint16(int16(state.TimeOffsetSec * 1000)), // ms, signed
		boolToUint16(state.NTPEnabled),
		boolToUint16(state.PTPEnabled),
	}
}

func getInputRegisters() []uint16 {
	state.mu.RLock()
	defer state.mu.RUnlock()

	var syncCode uint16
	switch state.SyncStatus {
	case "locked":
		syncCode = 0
	case "holdover":
		syncCode = 1
	case "freerun":
		syncCode = 2
	}

	return []uint16{
		syncCode,
		uint16(state.SatelliteCount),
		uint16(math.Abs(state.TimeOffsetSec) * 1000),
		uint16(state.HoldoverDriftPPM * 100),
	}
}

func boolToUint16(b bool) uint16 {
	if b {
		return 1
	}
	return 0
}

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

func handleWriteSingleCoil(data []byte) []byte {
	if len(data) < 4 {
		return []byte{fcWriteSingleCoil | 0x80, 0x03}
	}
	coilAddr := binary.BigEndian.Uint16(data[0:2])
	value := binary.BigEndian.Uint16(data[2:4])

	if value != 0xFF00 && value != 0x0000 {
		return []byte{fcWriteSingleCoil | 0x80, 0x03}
	}

	on := value == 0xFF00

	switch coilAddr {
	case 0: // ntp_enabled
		state.mu.Lock()
		state.NTPEnabled = on
		state.LastCommandSource = "modbus"
		state.mu.Unlock()
		log.Printf("MODBUS FC5: ntp_enabled=%v", on)
		audit.Add(shared.AuditEntry{
			Source: "modbus", Target: "gps-sim",
			Command: "write_coil_ntp", Result: "executed",
		})

	case 1: // ptp_enabled
		state.mu.Lock()
		state.PTPEnabled = on
		state.LastCommandSource = "modbus"
		state.mu.Unlock()
		log.Printf("MODBUS FC5: ptp_enabled=%v", on)
		audit.Add(shared.AuditEntry{
			Source: "modbus", Target: "gps-sim",
			Command: "write_coil_ptp", Result: "executed",
		})

	default:
		return []byte{fcWriteSingleCoil | 0x80, 0x02}
	}

	resp := make([]byte, 5)
	resp[0] = fcWriteSingleCoil
	binary.BigEndian.PutUint16(resp[1:3], coilAddr)
	binary.BigEndian.PutUint16(resp[3:5], value)
	return resp
}

func handleWriteSingleRegister(data []byte) []byte {
	if len(data) < 4 {
		return []byte{fcWriteSingleRegister | 0x80, 0x03}
	}
	regAddr := binary.BigEndian.Uint16(data[0:2])
	value := binary.BigEndian.Uint16(data[2:4])

	switch regAddr {
	case 0: // time_offset_ms (signed)
		offsetMs := int16(value)
		offsetSec := float64(offsetMs) / 1000.0

		state.mu.Lock()
		state.TimeOffsetSec = offsetSec
		state.Alarm = math.Abs(offsetSec) > 1.0
		state.LastCommandSource = "modbus"
		state.mu.Unlock()

		log.Printf("MODBUS FC6: time_offset=%.3fs", offsetSec)
		impact := ""
		if math.Abs(offsetSec) > 1.0 {
			impact = "SOE timestamps corrupted via Modbus time spoofing"
		}
		audit.Add(shared.AuditEntry{
			Source: "modbus", Target: "gps-sim",
			Command: "write_reg_time_offset", Result: "executed",
			Detail:        fmt.Sprintf("offset set to %.3fs via Modbus", offsetSec),
			ProcessImpact: impact,
		})

	default:
		return []byte{fcWriteSingleRegister | 0x80, 0x02}
	}

	resp := make([]byte, 5)
	resp[0] = fcWriteSingleRegister
	binary.BigEndian.PutUint16(resp[1:3], regAddr)
	binary.BigEndian.PutUint16(resp[3:5], value)
	return resp
}
