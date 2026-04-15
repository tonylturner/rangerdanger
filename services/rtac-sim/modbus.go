package main

// Modbus TCP server that exposes the aggregated RTAC state as standard
// Modbus registers. This allows FUXA HMI (and any other Modbus client)
// to poll substation status via the standard OT protocol.
//
// Register Map:
//
//   Coils (FC1, 0-based):
//     0: breaker_closed
//     1: recloser_closed
//     2: general_load_energized
//     3: critical_load_energized
//     4: reclose_enabled
//     5: remote_control_enabled
//     6: regulator_auto_mode (inverted manual_mode)
//     7: relay_comms_ok
//     8: recloser_comms_ok
//     9: regulator_comms_ok
//    10: capbank_switched_in
//    11: capbank_auto_mode
//    12: capbank_comms_ok
//
//   Discrete Inputs (FC2, 0-based) - Alarms:
//     0: comm_loss
//     1: breaker_open_unexpected
//     2: reclose_disabled
//     3: low_voltage_critical
//     4: relay_fault
//     5: relay_lockout
//     6: recloser_fault
//     7: recloser_lockout
//     8: regulator_alarm
//     9: capbank_lockout
//    10: capbank_alarm
//
//   Holding Registers (FC3, 0-based):
//     0: breaker_closed (0/1)
//     1: recloser_closed (0/1)
//     2: reclose_enabled (0/1)
//     3: regulator_tap_position (signed int16)
//     4: regulator_manual_mode (0/1)
//     5: recloser_shot_count
//     6: relay_lockout (0/1)
//     7: recloser_lockout (0/1)
//     8: capbank_switched_in (0/1)
//     9: capbank_auto_mode (0/1)
//    10: capbank_switch_count
//    11: capbank_lockout (0/1)
//
//   Input Registers (FC4, 0-based) - Analog measurements (x10 scaling):
//     0: substation_bus_voltage_v * 10
//     1: downstream_voltage_v * 10
//     2: critical_load_voltage_v * 10
//     3: feeder_current_a * 10
//     4: general_load_kw
//     5: critical_load_kw
//     6: relay_measured_current_a * 10
//     7: relay_measured_voltage_kv * 100
//     8: capbank_kvar_rating * 10

import (
	"encoding/binary"
	"io"
	"log"
	"net"
)

const (
	modbusPort = ":502"

	// Function codes
	fcReadCoils            = 0x01
	fcReadDiscreteInputs   = 0x02
	fcReadHoldingRegisters = 0x03
	fcReadInputRegisters   = 0x04
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
			resp = handleReadCoils(buf[8:7+pduLen])
		case fcReadDiscreteInputs:
			resp = handleReadDiscreteInputs(buf[8:7+pduLen])
		case fcReadHoldingRegisters:
			resp = handleReadHoldingRegisters(buf[8:7+pduLen])
		case fcReadInputRegisters:
			resp = handleReadInputRegisters(buf[8:7+pduLen])
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

// getCoils returns current coil values from aggregated state.
func getCoils() []bool {
	agg.mu.RLock()
	defer agg.mu.RUnlock()

	coils := make([]bool, 13)
	if relay, ok := agg.Devices["relay"]; ok {
		coils[0], _ = relay["breaker_closed"].(bool)
		coils[5], _ = relay["remote_control_enabled"].(bool)
	}
	if recloser, ok := agg.Devices["recloser"]; ok {
		coils[1], _ = recloser["closed"].(bool)
		coils[4], _ = recloser["reclose_enabled"].(bool)
	}
	if regulator, ok := agg.Devices["regulator"]; ok {
		manual, _ := regulator["manual_mode"].(bool)
		coils[6] = !manual // auto mode = not manual
	}
	if capbank, ok := agg.Devices["capbank"]; ok {
		coils[10], _ = capbank["switched_in"].(bool)
		coils[11], _ = capbank["auto_mode"].(bool)
	}
	if elec := agg.Electrical; elec != nil {
		coils[2], _ = elec["general_load_energized"].(bool)
		coils[3], _ = elec["critical_load_energized"].(bool)
	}
	coils[7] = agg.DeviceComms["relay"]
	coils[8] = agg.DeviceComms["recloser"]
	coils[9] = agg.DeviceComms["regulator"]
	coils[12] = agg.DeviceComms["capbank"]
	return coils
}

// getDiscreteInputs returns alarm states.
func getDiscreteInputs() []bool {
	agg.mu.RLock()
	defer agg.mu.RUnlock()

	alarms := make([]bool, 11)
	alarms[0] = hasCommLoss()
	alarms[1] = isBreakerOpenUnexpected()
	alarms[2] = isRecloseDisabled()
	alarms[3] = isLowVoltageCritical()

	if relay, ok := agg.Devices["relay"]; ok {
		alarms[4], _ = relay["fault_seen"].(bool)
		alarms[5], _ = relay["lockout"].(bool)
	}
	if recloser, ok := agg.Devices["recloser"]; ok {
		alarms[6], _ = recloser["fault_seen"].(bool)
		alarms[7], _ = recloser["lockout"].(bool)
	}
	if regulator, ok := agg.Devices["regulator"]; ok {
		alarms[8], _ = regulator["alarm"].(bool)
	}
	if capbank, ok := agg.Devices["capbank"]; ok {
		alarms[9], _ = capbank["lockout"].(bool)
		alarms[10], _ = capbank["alarm"].(bool)
	}
	return alarms
}

// getHoldingRegisters returns device state as 16-bit registers.
func getHoldingRegisters() []uint16 {
	agg.mu.RLock()
	defer agg.mu.RUnlock()

	regs := make([]uint16, 12)
	if relay, ok := agg.Devices["relay"]; ok {
		regs[0] = boolToUint16(relay["breaker_closed"])
		regs[6] = boolToUint16(relay["lockout"])
	}
	if recloser, ok := agg.Devices["recloser"]; ok {
		regs[1] = boolToUint16(recloser["closed"])
		regs[2] = boolToUint16(recloser["reclose_enabled"])
		if sc, ok := recloser["shot_count"].(float64); ok {
			regs[5] = uint16(sc)
		}
		regs[7] = boolToUint16(recloser["lockout"])
	}
	if regulator, ok := agg.Devices["regulator"]; ok {
		if tp, ok := regulator["tap_position"].(float64); ok {
			regs[3] = uint16(int16(tp)) // signed
		}
		regs[4] = boolToUint16(regulator["manual_mode"])
	}
	if capbank, ok := agg.Devices["capbank"]; ok {
		regs[8] = boolToUint16(capbank["switched_in"])
		regs[9] = boolToUint16(capbank["auto_mode"])
		if sc, ok := capbank["switch_count"].(float64); ok {
			regs[10] = uint16(sc)
		}
		regs[11] = boolToUint16(capbank["lockout"])
	}
	return regs
}

// getInputRegisters returns analog measurements scaled x10.
func getInputRegisters() []uint16 {
	agg.mu.RLock()
	defer agg.mu.RUnlock()

	regs := make([]uint16, 9)
	if elec := agg.Electrical; elec != nil {
		regs[0] = floatToScaled(elec["substation_bus_voltage_v"], 10)
		regs[1] = floatToScaled(elec["downstream_voltage_v"], 10)
		regs[2] = floatToScaled(elec["critical_load_voltage_v"], 10)
		regs[3] = floatToScaled(elec["feeder_current_a"], 10)
		regs[4] = floatToScaled(elec["general_load_kw"], 1)
		regs[5] = floatToScaled(elec["critical_load_kw"], 1)
	}
	if relay, ok := agg.Devices["relay"]; ok {
		regs[6] = floatToScaled(relay["measured_current_a"], 10)
		regs[7] = floatToScaled(relay["measured_voltage_kv"], 100)
	}
	if capbank, ok := agg.Devices["capbank"]; ok {
		regs[8] = floatToScaled(capbank["kvar_rating"], 10)
	}
	return regs
}

func boolToUint16(v any) uint16 {
	if b, ok := v.(bool); ok && b {
		return 1
	}
	return 0
}

func floatToScaled(v any, scale float64) uint16 {
	if f, ok := v.(float64); ok {
		return uint16(f * scale)
	}
	return 0
}

// Modbus function handlers

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

func handleReadDiscreteInputs(data []byte) []byte {
	if len(data) < 4 {
		return []byte{fcReadDiscreteInputs | 0x80, 0x03}
	}
	startAddr := binary.BigEndian.Uint16(data[0:2])
	quantity := binary.BigEndian.Uint16(data[2:4])

	inputs := getDiscreteInputs()
	if int(startAddr+quantity) > len(inputs) {
		return []byte{fcReadDiscreteInputs | 0x80, 0x02}
	}

	byteCount := (quantity + 7) / 8
	resp := make([]byte, 2+byteCount)
	resp[0] = fcReadDiscreteInputs
	resp[1] = byte(byteCount)

	for i := uint16(0); i < quantity; i++ {
		if inputs[startAddr+i] {
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
