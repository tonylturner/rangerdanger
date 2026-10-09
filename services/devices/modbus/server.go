// SPDX-License-Identifier: Apache-2.0
package modbus

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"time"

	"github.com/tturner/rangerdanger/services/devices/feederbreaker"
)

const (
	UnitID = 1

	functionReadHolding = 0x03
	functionReadInput   = 0x04
	functionWriteSingle = 0x06

	exceptionIllegalFunction = 0x01
	exceptionIllegalAddress  = 0x02
	exceptionIllegalValue    = 0x03
	exceptionDeviceFailure   = 0x04
)

type device interface {
	Snapshot() feederbreaker.Snapshot
	Command(context.Context, feederbreaker.Command, feederbreaker.AuditContext) feederbreaker.CommandResult
}

type Server struct {
	model device
	zones *feederbreaker.ZoneMap
}

func New(model device, zones *feederbreaker.ZoneMap) *Server {
	return &Server{model: model, zones: zones}
}

func (s *Server) Serve(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.ServeConn(conn)
	}
}

func (s *Server) ServeConn(conn net.Conn) {
	defer conn.Close()
	peer := conn.RemoteAddr().String()
	for {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		header := make([]byte, 7)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		transaction := binary.BigEndian.Uint16(header[0:2])
		protocol := binary.BigEndian.Uint16(header[2:4])
		length := binary.BigEndian.Uint16(header[4:6])
		unit := header[6]
		if protocol != 0 || length < 2 || length > 254 {
			return
		}
		pdu := make([]byte, int(length)-1)
		if _, err := io.ReadFull(conn, pdu); err != nil {
			return
		}
		if unit != UnitID {
			continue
		}
		response := s.HandlePDU(pdu, peer)
		frame := make([]byte, 7+len(response))
		binary.BigEndian.PutUint16(frame[0:2], transaction)
		binary.BigEndian.PutUint16(frame[2:4], 0)
		binary.BigEndian.PutUint16(frame[4:6], uint16(1+len(response)))
		frame[6] = unit
		copy(frame[7:], response)
		if err := writeFull(conn, frame); err != nil {
			return
		}
	}
}

func (s *Server) HandlePDU(pdu []byte, peer string) []byte {
	if len(pdu) == 0 {
		return []byte{0x80, exceptionIllegalValue}
	}
	function := pdu[0]
	switch function {
	case functionReadHolding:
		return s.readHolding(pdu)
	case functionReadInput:
		return s.readInput(pdu)
	case functionWriteSingle:
		return s.writeSingle(pdu, peer)
	default:
		return exception(function, exceptionIllegalFunction)
	}
}

func (s *Server) readHolding(pdu []byte) []byte {
	if len(pdu) != 5 {
		return exception(functionReadHolding, exceptionIllegalValue)
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	quantity := binary.BigEndian.Uint16(pdu[3:5])
	if quantity == 0 || quantity > 125 || address != 0 || quantity != 1 {
		return exception(functionReadHolding, exceptionIllegalAddress)
	}
	snapshot := s.model.Snapshot()
	position, ok := positionRegister(snapshot.Position)
	if !ok {
		position = 3
	}
	return registerResponse(functionReadHolding, []uint16{position})
}

func (s *Server) readInput(pdu []byte) []byte {
	if len(pdu) != 5 {
		return exception(functionReadInput, exceptionIllegalValue)
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	quantity := binary.BigEndian.Uint16(pdu[3:5])
	end := uint32(address) + uint32(quantity)
	if quantity == 0 || quantity > 125 || end > 11 {
		return exception(functionReadInput, exceptionIllegalAddress)
	}
	snapshot := s.model.Snapshot()
	registers := inputRegisters(snapshot)
	return registerResponse(functionReadInput, registers[int(address):int(end)])
}

func (s *Server) writeSingle(pdu []byte, peer string) []byte {
	if len(pdu) != 5 {
		return exception(functionWriteSingle, exceptionIllegalValue)
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	value := binary.BigEndian.Uint16(pdu[3:5])
	if address != 0 {
		return exception(functionWriteSingle, exceptionIllegalAddress)
	}
	var command feederbreaker.Command
	switch value {
	case 1:
		command = feederbreaker.CommandOpen
	case 2:
		command = feederbreaker.CommandClose
	default:
		return exception(functionWriteSingle, exceptionIllegalValue)
	}
	result := s.model.Command(context.Background(), command, s.zones.Context(peer, "modbus"))
	if !result.Accepted {
		return exception(functionWriteSingle, exceptionDeviceFailure)
	}
	response := make([]byte, len(pdu))
	copy(response, pdu)
	return response
}

func inputRegisters(snapshot feederbreaker.Snapshot) []uint16 {
	registers := make([]uint16, 11)
	position, ok := positionRegister(snapshot.Position)
	if !ok {
		position = 3
	}
	registers[0] = position
	var flags uint16
	if snapshot.RemoteEnabled {
		flags |= 1 << 0
	}
	if snapshot.Lockout {
		flags |= 1 << 1
	}
	if snapshot.FaultActive {
		flags |= 1 << 2
	}
	if snapshot.ProtectionTripped {
		flags |= 1 << 3
	}
	registers[1] = flags
	if snapshot.Measurements.Valid {
		registers[2] = 1
	}
	if snapshot.Measurements.CurrentA != nil {
		putFloat32(registers[3:5], *snapshot.Measurements.CurrentA)
	}
	if snapshot.Measurements.VoltageKV != nil {
		putFloat32(registers[5:7], *snapshot.Measurements.VoltageKV)
	}
	putUint32(registers[7:9], uint32(snapshot.EventSeq))
	putUint32(registers[9:11], snapshot.Generation)
	return registers
}

func positionRegister(position feederbreaker.Position) (uint16, bool) {
	switch position {
	case feederbreaker.PositionOpen:
		return 1, true
	case feederbreaker.PositionClosed:
		return 2, true
	default:
		return 3, false
	}
}

func putFloat32(registers []uint16, value float64) {
	putUint32(registers, math.Float32bits(float32(value)))
}

func putUint32(registers []uint16, value uint32) {
	registers[0] = uint16(value >> 16)
	registers[1] = uint16(value)
}

func registerResponse(function byte, registers []uint16) []byte {
	response := make([]byte, 2+2*len(registers))
	response[0] = function
	response[1] = byte(2 * len(registers))
	for i, register := range registers {
		binary.BigEndian.PutUint16(response[2+i*2:], register)
	}
	return response
}

func exception(function, code byte) []byte {
	return []byte{function | 0x80, code}
}

func writeFull(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if err != nil {
			return err
		}
		if written == 0 {
			return errors.New("connection write returned zero bytes")
		}
		value = value[written:]
	}
	return nil
}
