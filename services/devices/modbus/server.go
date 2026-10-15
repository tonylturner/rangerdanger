// SPDX-License-Identifier: Apache-2.0
package modbus

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"github.com/tturner/rangerdanger/services/devices/fielddevice"
)

const (
	UnitID = 1

	functionReadHolding   = 0x03
	functionReadInput     = 0x04
	functionWriteSingle   = 0x06
	functionWriteMultiple = 0x10

	exceptionIllegalFunction = 0x01
	exceptionIllegalAddress  = 0x02
	exceptionIllegalValue    = 0x03
	exceptionDeviceFailure   = 0x04
)

type device interface {
	Snapshot() fielddevice.Snapshot
	WriteHolding(context.Context, uint16, uint16, fielddevice.AuditContext) fielddevice.CommandResult
}

type Server struct {
	model device
	zones *fielddevice.ZoneMap
}

func New(model device, zones *fielddevice.ZoneMap) *Server {
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
	case functionWriteMultiple:
		return s.writeMultiple(pdu, peer)
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
	snapshot := s.model.Snapshot()
	position, _ := positionRegister(snapshot.Position)
	registers := []uint16{position}
	if snapshot.Kind == fielddevice.OLTC {
		mode := uint16(1)
		if snapshot.Auto {
			mode = 2
		}
		registers = []uint16{0, snapshot.LastRequest, mode}
	}
	end := uint32(address) + uint32(quantity)
	if quantity == 0 || quantity > 125 || end > uint32(len(registers)) {
		return exception(functionReadHolding, exceptionIllegalAddress)
	}
	return registerResponse(functionReadHolding, registers[int(address):int(end)])
}
func (s *Server) readInput(pdu []byte) []byte {
	if len(pdu) != 5 {
		return exception(functionReadInput, exceptionIllegalValue)
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	quantity := binary.BigEndian.Uint16(pdu[3:5])
	registers := inputRegisters(s.model.Snapshot())
	end := uint32(address) + uint32(quantity)
	if quantity == 0 || quantity > 125 || end > uint32(len(registers)) {
		return exception(functionReadInput, exceptionIllegalAddress)
	}
	return registerResponse(functionReadInput, registers[int(address):int(end)])
}
func (s *Server) writeSingle(pdu []byte, peer string) []byte {
	if len(pdu) != 5 {
		return exception(functionWriteSingle, exceptionIllegalValue)
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	value := binary.BigEndian.Uint16(pdu[3:5])
	if code := s.write(address, value, peer); code != 0 {
		return exception(functionWriteSingle, code)
	}
	return append([]byte(nil), pdu...)
}
func (s *Server) writeMultiple(pdu []byte, peer string) []byte {
	// Only the OLTC PLC request wire permits FC16, and only one HR1.
	if len(pdu) < 6 {
		return exception(functionWriteMultiple, exceptionIllegalValue)
	}
	quantity := binary.BigEndian.Uint16(pdu[3:5])
	if quantity != 1 || pdu[5] != 2 || len(pdu) != 8 {
		return exception(functionWriteMultiple, exceptionIllegalValue)
	}
	address := binary.BigEndian.Uint16(pdu[1:3])
	if address != 1 || s.model.Snapshot().Kind != fielddevice.OLTC {
		return exception(functionWriteMultiple, exceptionIllegalAddress)
	}
	value := binary.BigEndian.Uint16(pdu[6:8])
	if code := s.write(address, value, peer); code != 0 {
		return exception(functionWriteMultiple, code)
	}
	return append([]byte(nil), pdu[:5]...)
}
func (s *Server) write(address, value uint16, peer string) byte {
	result := s.model.WriteHolding(context.Background(), address, value, s.zones.Context(peer, "modbus"))
	if result.Exception != 0 {
		return result.Exception
	}
	if !result.Accepted {
		return exceptionDeviceFailure
	}
	return 0
}

func positionRegister(position fielddevice.Position) (uint16, bool) {
	switch position {
	case fielddevice.PositionOpen:
		return 1, true
	case fielddevice.PositionClosed:
		return 2, true
	default:
		return 3, false
	}
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
