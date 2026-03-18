package main

// Modbus TCP server for the data historian. Exposes latest readings and
// configuration as Modbus registers, matching how real PI/eDNA historians
// expose tags via Modbus gateway interfaces.
//
// Register Map:
//
//   Coils (FC1 read / FC5 write, 0-based):
//     0: recording
//     1: write_back_enabled
//     2: comms_ok (read-only)
//
//   Holding Registers (FC3 read / FC6 write, 0-based):
//     0: recording (0/1)
//     1: write_back_enabled (0/1)
//     2: poll_interval_sec
//
//   Input Registers (FC4 read-only, 0-based):
//     0: substation_voltage_v * 10
//     1: downstream_voltage_v * 10
//     2: feeder_current_a * 10
//     3: general_load_kw * 10
//     4: critical_load_kw * 10
//     5: point_count

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

func getCoils() []bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return []bool{
		state.Recording,
		state.WriteBackEnabled,
		state.CommsOK,
	}
}

func getHoldingRegisters() []uint16 {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return []uint16{
		boolToUint16(state.Recording),
		boolToUint16(state.WriteBackEnabled),
		uint16(state.PollIntervalSec),
	}
}

func getInputRegisters() []uint16 {
	state.mu.RLock()
	defer state.mu.RUnlock()

	var subV, downV, curA, genKw, critKw float64
	if len(state.History) > 0 {
		last := state.History[len(state.History)-1]
		subV = last.SubstationVoltageV
		downV = last.DownstreamVoltageV
		curA = last.FeederCurrentA
		genKw = last.GeneralLoadKw
		critKw = last.CriticalLoadKw
	}

	return []uint16{
		uint16(subV * 10),
		uint16(downV * 10),
		uint16(curA * 10),
		uint16(genKw * 10),
		uint16(critKw * 10),
		uint16(state.PointCount),
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
	case 0: // recording
		state.mu.Lock()
		state.Recording = on
		state.mu.Unlock()
		log.Printf("MODBUS FC5: recording=%v", on)
		audit.Add(shared.AuditEntry{
			Source: "modbus", Target: "historian-sim",
			Command: "write_coil_recording", Result: "executed",
		})

	case 1: // write_back_enabled
		state.mu.Lock()
		state.WriteBackEnabled = on
		state.mu.Unlock()
		log.Printf("MODBUS FC5: write_back_enabled=%v", on)
		audit.Add(shared.AuditEntry{
			Source: "modbus", Target: "historian-sim",
			Command: "write_coil_write_back", Result: "executed",
			Detail:        "write-back toggled via Modbus",
			ProcessImpact: "historian write-back changed remotely",
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
	case 2: // poll_interval_sec
		sec := int(value)
		if sec < 1 || sec > 60 {
			return []byte{fcWriteSingleRegister | 0x80, 0x03}
		}
		state.mu.Lock()
		state.PollIntervalSec = sec
		state.mu.Unlock()

		audit.Add(shared.AuditEntry{
			Source: "modbus", Target: "historian-sim",
			Command: "write_reg_poll_interval", Result: "executed",
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
