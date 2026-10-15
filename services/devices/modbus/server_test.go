// SPDX-License-Identifier: Apache-2.0
package modbus

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"net"
	"testing"

	"github.com/tturner/rangerdanger/services/devices/feederbreaker"
)

type testDevice struct {
	snapshot feederbreaker.Snapshot
	calls    int
	commands []feederbreaker.Command
	result   feederbreaker.CommandResult
}

func (d *testDevice) Snapshot() feederbreaker.Snapshot {
	d.calls++
	return d.snapshot
}

func (d *testDevice) Command(_ context.Context, command feederbreaker.Command, _ feederbreaker.AuditContext) feederbreaker.CommandResult {
	d.commands = append(d.commands, command)
	return d.result
}

func testServer(t *testing.T, device *testDevice) *Server {
	t.Helper()
	zones, err := feederbreaker.ParseZones("field=10.40.40.0/24,station=10.30.30.0/24,process=10.50.50.0/24,mgmt=10.99.99.0/24")
	if err != nil {
		t.Fatal(err)
	}
	return New(device, zones)
}

func pdu(function byte, address, quantityOrValue uint16) []byte {
	result := make([]byte, 5)
	result[0] = function
	binary.BigEndian.PutUint16(result[1:3], address)
	binary.BigEndian.PutUint16(result[3:5], quantityOrValue)
	return result
}

func TestFC4UsesOneSnapshotAndMapsRegisters(t *testing.T) {
	current := 1.0
	voltage := 20.0
	device := &testDevice{snapshot: feederbreaker.Snapshot{
		Truth: feederbreaker.Truth{
			Position: feederbreaker.PositionClosed, RemoteEnabled: true,
			Lockout: true, FaultActive: true, ProtectionTripped: true,
			EventSeq: 0x89abcdef, Generation: 0x12345678,
		},
		Measurements: feederbreaker.Measurements{
			CurrentA: &current, VoltageKV: &voltage, Valid: true,
		},
	}}
	server := testServer(t, device)
	response := server.HandlePDU(pdu(functionReadInput, 0, 11), "10.30.30.20:502")
	if device.calls != 1 {
		t.Fatalf("FC4 took %d snapshots, want one", device.calls)
	}
	if len(response) != 24 || response[0] != functionReadInput || response[1] != 22 {
		t.Fatalf("FC4 response header/length = %v, want 24-byte response", response)
	}
	registers := make([]uint16, 11)
	for i := range registers {
		registers[i] = binary.BigEndian.Uint16(response[2+i*2:])
	}
	want := []uint16{
		2, 0x000f, 1, 0x3f80, 0x0000, 0x41a0, 0x0000,
		0x89ab, 0xcdef, 0x1234, 0x5678,
	}
	for i := range want {
		if registers[i] != want[i] {
			t.Errorf("input register %d = %#04x, want %#04x", i, registers[i], want[i])
		}
	}
}

func TestFC4UnknownMeasurementsEncodeZeroWithValidityClear(t *testing.T) {
	device := &testDevice{snapshot: feederbreaker.Snapshot{
		Truth: feederbreaker.Truth{Position: feederbreaker.PositionOpen},
	}}
	response := testServer(t, device).HandlePDU(pdu(functionReadInput, 0, 7), "10.30.30.20:502")
	if device.calls != 1 {
		t.Fatalf("FC4 took %d snapshots, want one", device.calls)
	}
	if binary.BigEndian.Uint16(response[4:6]) != 0 ||
		binary.BigEndian.Uint16(response[6:8]) != 0 ||
		binary.BigEndian.Uint16(response[8:10]) != 0 ||
		binary.BigEndian.Uint16(response[2:4]) != 1 {
		t.Fatalf("invalid measurement registers = %v", response)
	}
}

func TestFC3AndFC6HoldingRegister(t *testing.T) {
	device := &testDevice{
		snapshot: feederbreaker.Snapshot{Truth: feederbreaker.Truth{Position: feederbreaker.PositionOpen}},
		result:   feederbreaker.CommandResult{Accepted: true},
	}
	server := testServer(t, device)
	read := server.HandlePDU(pdu(functionReadHolding, 0, 1), "10.30.30.20:502")
	if !bytes.Equal(read, []byte{functionReadHolding, 2, 0, 1}) {
		t.Fatalf("FC3 response = %v, want position=1", read)
	}
	write := pdu(functionWriteSingle, 0, 2)
	response := server.HandlePDU(write, "10.30.30.20:502")
	if !bytes.Equal(response, write) || len(device.commands) != 1 || device.commands[0] != feederbreaker.CommandClose {
		t.Fatalf("FC6 response=%v commands=%v", response, device.commands)
	}
}

func TestEveryModbusExceptionAndExactPDULengths(t *testing.T) {
	device := &testDevice{result: feederbreaker.CommandResult{Accepted: true}}
	server := testServer(t, device)
	tests := []struct {
		name string
		pdu  []byte
		code byte
	}{
		{name: "unsupported function", pdu: []byte{0x01}, code: exceptionIllegalFunction},
		{name: "FC3 bad address", pdu: pdu(functionReadHolding, 1, 1), code: exceptionIllegalAddress},
		{name: "FC3 bad quantity", pdu: pdu(functionReadHolding, 0, 0), code: exceptionIllegalAddress},
		{name: "FC4 bad address", pdu: pdu(functionReadInput, 10, 2), code: exceptionIllegalAddress},
		{name: "FC4 bad quantity", pdu: pdu(functionReadInput, 0, 0), code: exceptionIllegalAddress},
		{name: "FC6 bad address", pdu: pdu(functionWriteSingle, 1, 1), code: exceptionIllegalAddress},
		{name: "FC6 bad value", pdu: pdu(functionWriteSingle, 0, 0), code: exceptionIllegalValue},
		{name: "FC3 short PDU", pdu: []byte{functionReadHolding, 0, 0, 1}, code: exceptionIllegalValue},
		{name: "FC3 long PDU", pdu: []byte{functionReadHolding, 0, 0, 0, 1, 0}, code: exceptionIllegalValue},
		{name: "FC4 short PDU", pdu: []byte{functionReadInput, 0, 0, 0}, code: exceptionIllegalValue},
		{name: "FC4 long PDU", pdu: []byte{functionReadInput, 0, 0, 0, 1, 0}, code: exceptionIllegalValue},
		{name: "FC6 short PDU", pdu: []byte{functionWriteSingle, 0, 0, 0}, code: exceptionIllegalValue},
		{name: "FC6 long PDU", pdu: []byte{functionWriteSingle, 0, 0, 0, 1, 0}, code: exceptionIllegalValue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := server.HandlePDU(test.pdu, "10.30.30.20:502")
			want := []byte{test.pdu[0] | 0x80, test.code}
			if !bytes.Equal(response, want) {
				t.Fatalf("response=%v, want exception %v", response, want)
			}
		})
	}
}

func TestFC6InterlockUsesException04(t *testing.T) {
	device := &testDevice{result: feederbreaker.CommandResult{Reason: "lockout"}}
	response := testServer(t, device).HandlePDU(pdu(functionWriteSingle, 0, 2), "10.30.30.20:502")
	if !bytes.Equal(response, []byte{functionWriteSingle | 0x80, exceptionDeviceFailure}) {
		t.Fatalf("interlock response = %v", response)
	}
}

func TestFC6ModelInterlockUsesException04(t *testing.T) {
	model, err := feederbreaker.NewModel(feederbreaker.Config{DeviceID: "feeder-breaker", Generation: 9})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.InjectFault(context.Background(), true, feederbreaker.AuditContext{Peer: "lab", Protocol: "lab", Zone: "mgmt"}); err != nil {
		t.Fatal(err)
	}
	zones, err := feederbreaker.ParseZones("field=10.40.40.0/24")
	if err != nil {
		t.Fatal(err)
	}
	response := New(model, zones).HandlePDU(pdu(functionWriteSingle, 0, 2), "10.30.30.20:502")
	if !bytes.Equal(response, []byte{functionWriteSingle | 0x80, exceptionDeviceFailure}) {
		t.Fatalf("model interlock response = %v", response)
	}
}

func TestFloatWordOrderIsHighWordFirst(t *testing.T) {
	registers := make([]uint16, 2)
	putFloat32(registers, float64(math.Float32frombits(0x12345678)))
	if registers[0] != 0x1234 || registers[1] != 0x5678 {
		t.Fatalf("float32 registers = %#04x %#04x, want high word first 0x1234 0x5678", registers[0], registers[1])
	}
}

func TestModbusTCPPreservesTransactionAndHandlesCoalescedFrames(t *testing.T) {
	device := &testDevice{snapshot: feederbreaker.Snapshot{Truth: feederbreaker.Truth{Position: feederbreaker.PositionClosed}}}
	server := testServer(t, device)
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go server.ServeConn(serverConn)

	first := modbusFrame(0x1234, pdu(functionReadHolding, 0, 1))
	second := modbusFrame(0xabcd, pdu(functionReadInput, 0, 1))
	writeErr := make(chan error, 1)
	go func() {
		_, err := clientConn.Write(append(first, second...))
		writeErr <- err
	}()
	for _, transaction := range []uint16{0x1234, 0xabcd} {
		header := make([]byte, 7)
		if _, err := io.ReadFull(clientConn, header); err != nil {
			t.Fatal(err)
		}
		if got := binary.BigEndian.Uint16(header[:2]); got != transaction {
			t.Errorf("transaction = %#04x, want %#04x", got, transaction)
		}
		length := int(binary.BigEndian.Uint16(header[4:6]))
		response := make([]byte, length-1)
		if _, err := io.ReadFull(clientConn, response); err != nil {
			t.Fatal(err)
		}
	}
	if device.calls != 2 {
		t.Fatalf("read requests took %d snapshots, want 2", device.calls)
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
}

func modbusFrame(transaction uint16, pdu []byte) []byte {
	frame := make([]byte, 7+len(pdu))
	binary.BigEndian.PutUint16(frame[:2], transaction)
	binary.BigEndian.PutUint16(frame[4:6], uint16(1+len(pdu)))
	frame[6] = UnitID
	copy(frame[7:], pdu)
	return frame
}
