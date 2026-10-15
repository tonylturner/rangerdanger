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
	"time"

	"github.com/tturner/rangerdanger/services/devices/fielddevice"
)

type snapshotDevice struct {
	snapshot fielddevice.Snapshot
	calls    int
}

func (d *snapshotDevice) Snapshot() fielddevice.Snapshot { d.calls++; return d.snapshot }
func (d *snapshotDevice) WriteHolding(context.Context, uint16, uint16, fielddevice.AuditContext) fielddevice.CommandResult {
	panic("snapshot-only fixture cannot accept writes")
}
func newServer(t *testing.T, d device) *Server {
	t.Helper()
	zones, err := fielddevice.ParseZones("field=10.40.40.0/24,station=10.30.30.0/24,process=10.50.50.0/24,mgmt=10.99.99.0/24")
	if err != nil {
		t.Fatal(err)
	}
	return New(d, zones)
}
func newModelServer(t *testing.T, kind fielddevice.Kind) (*fielddevice.Model, *Server) {
	t.Helper()
	id := map[fielddevice.Kind]string{fielddevice.Breaker: "feeder-breaker", fielddevice.RMU: "rmu-1", fielddevice.OLTC: "oltc-1"}[kind]
	m, err := fielddevice.NewModel(fielddevice.Config{Kind: kind, DeviceID: id, RMUNormal: fielddevice.PositionClosed, Generation: 42})
	if err != nil {
		t.Fatal(err)
	}
	return m, newServer(t, m)
}
func pdu(function byte, address, quantityOrValue uint16) []byte {
	result := make([]byte, 5)
	result[0] = function
	binary.BigEndian.PutUint16(result[1:3], address)
	binary.BigEndian.PutUint16(result[3:5], quantityOrValue)
	return result
}
func fc16(address, quantity uint16, values ...uint16) []byte {
	result := pdu(functionWriteMultiple, address, quantity)
	result = append(result, byte(2*len(values)))
	for _, value := range values {
		result = binary.BigEndian.AppendUint16(result, value)
	}
	return result
}
func registers(t *testing.T, response []byte) []uint16 {
	t.Helper()
	if len(response) < 2 || response[0]&0x80 != 0 || len(response) != int(response[1])+2 {
		t.Fatalf("bad read response=%v", response)
	}
	values := make([]uint16, int(response[1])/2)
	for i := range values {
		values[i] = binary.BigEndian.Uint16(response[2+2*i:])
	}
	return values
}
func assertRegisters(t *testing.T, got, want []uint16) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("register count=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("IR%d=%#04x want %#04x", i, got[i], want[i])
		}
	}
}
func apply(t *testing.T, m *fielddevice.Model, fault bool) {
	t.Helper()
	truth := m.Truth()
	input := fielddevice.MeasurementInput{Schema: 2, DeviceID: truth.DeviceID, Generation: truth.Generation, Revision: truth.Revision,
		SolverEpoch: "solver", SolveSeq: uint64(time.Now().UnixNano()), SolvedAt: "2026-10-15T10:00:00Z", CurrentA: 1, VoltageKV: 20, SourceKV: 110, SectionLive: true, FaultCurrent: fault}
	if _, err := m.ApplyMeasurements(context.Background(), input, fielddevice.AuditContext{Protocol: "process"}); err != nil {
		t.Fatal(err)
	}
}

func TestInputMapsOneSnapshotPerRead(t *testing.T) {
	current, voltage, source := 1.0, 20.0, 110.0
	yes := true
	for _, tt := range []struct {
		kind fielddevice.Kind
		want []uint16
	}{
		{fielddevice.Breaker, []uint16{2, 15, 1, 0x3f80, 0, 0x41a0, 0, 0x89ab, 0xcdef, 0x1234, 0x5678}},
		{fielddevice.RMU, []uint16{2, 15, 1, 0x3f80, 0, 0x41a0, 0, 0x89ab, 0xcdef, 0x1234, 0x5678, 0x42dc, 0}},
		{fielddevice.OLTC, []uint16{0xfff8, 0x6f, 1, 0x41a0, 0, 0x3f80, 0, 0x89ab, 0xcdef, 0x1234, 0x5678, 0xfff9, 20000, 1, 42}},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			d := &snapshotDevice{snapshot: fielddevice.Snapshot{Truth: fielddevice.Truth{Kind: tt.kind, Position: fielddevice.PositionClosed, RemoteEnabled: true,
				Lockout: true, ProtectionTripped: true, EarthClosed: true, FaultPassage: true, Tap: -8, TargetTap: -7, Moving: true, Auto: true, Blocked: true,
				EventSeq: 0x89abcdef, Generation: 0x12345678}, Measurements: fielddevice.Measurements{CurrentA: &current, VoltageKV: &voltage, SourceKV: &source, FaultCurrent: &yes, SectionLive: &yes, Valid: true}, PLCRequestResult: fielddevice.RequestAccepted, Heartbeat: 42}}
			response := newServer(t, d).HandlePDU(pdu(functionReadInput, 0, uint16(len(tt.want))), "10.30.30.20:43001")
			if d.calls != 1 {
				t.Fatalf("snapshots=%d want 1", d.calls)
			}
			assertRegisters(t, registers(t, response), tt.want)
		})
	}
}

func TestInvalidMeasurementRegistersAndFlags(t *testing.T) {
	yes := true
	voltage := 20.0
	for _, kind := range []fielddevice.Kind{fielddevice.Breaker, fielddevice.RMU, fielddevice.OLTC} {
		t.Run(string(kind), func(t *testing.T) {
			d := &snapshotDevice{snapshot: fielddevice.Snapshot{Truth: fielddevice.Truth{Kind: kind, Position: fielddevice.PositionOpen}, Measurements: fielddevice.Measurements{FaultCurrent: &yes, SectionLive: &yes, VoltageKV: &voltage}}}
			r := inputRegisters(d.snapshot)
			flags := uint16(0)
			if kind == fielddevice.OLTC {
				flags = 1 << 6 // Section-live readback is independent of IR2 validity.
			}
			if r[1] != flags || r[2] != 0 {
				t.Fatal("invalid measurement flags or validity")
			}
			if kind == fielddevice.OLTC && r[12] != 0 {
				t.Fatal("invalid OLTC voltage V not zero")
			}
			d.snapshot.Measurements = fielddevice.Measurements{}
			r = inputRegisters(d.snapshot)
			if r[3] != 0 || r[4] != 0 || r[5] != 0 || r[6] != 0 {
				t.Fatal("unknown measurement encoded nonzero")
			}
		})
	}
}

func TestFC3AndFC6ReadbackAndPeer(t *testing.T) {
	for _, kind := range []fielddevice.Kind{fielddevice.Breaker, fielddevice.RMU} {
		t.Run(string(kind), func(t *testing.T) {
			model, server := newModelServer(t, kind)
			if kind == fielddevice.RMU {
				apply(t, model, false)
			}
			_, latest, _ := model.AuditAfter(0)
			var cursor uint64
			if latest != nil {
				cursor = *latest
			}
			request := pdu(functionWriteSingle, 0, 1)
			if r := server.HandlePDU(request, "10.30.30.20:43001"); !bytes.Equal(r, request) {
				t.Fatalf("write=%v", r)
			}
			assertRegisters(t, registers(t, server.HandlePDU(pdu(functionReadHolding, 0, 1), "")), []uint16{1})
			entries, _, _ := model.AuditAfter(cursor)
			if len(entries) != 1 || entries[0].Context.Protocol != "modbus" || entries[0].Context.Peer != "10.30.30.20:43001" || entries[0].Context.Zone != "station" {
				t.Fatalf("audit=%+v", entries)
			}
		})
	}
	model, server := newModelServer(t, fielddevice.OLTC)
	for _, request := range [][]byte{pdu(6, 2, 1), pdu(6, 1, 101)} {
		if response := server.HandlePDU(request, "10.30.30.20:43001"); !bytes.Equal(response, request) {
			t.Fatalf("request=%v response=%v", request, response)
		}
	}
	assertRegisters(t, registers(t, server.HandlePDU(pdu(3, 0, 3), "")), []uint16{0, 101, 1})
	if model.Snapshot().PLCRequestResult != fielddevice.RequestNotAuto {
		t.Fatal("ignored PLC request was not classified")
	}
	assertRegisters(t, registers(t, server.HandlePDU(pdu(4, 13, 1), "")), []uint16{3})
}

func TestEveryModbusExceptionAndExactLengths(t *testing.T) {
	_, breaker := newModelServer(t, fielddevice.Breaker)
	_, oltc := newModelServer(t, fielddevice.OLTC)
	for _, tt := range []struct {
		name    string
		server  *Server
		request []byte
		code    byte
	}{
		{"unsupported", breaker, []byte{1}, 1},
		{"FC3 address", breaker, pdu(3, 1, 1), 2}, {"FC3 quantity", breaker, pdu(3, 0, 0), 2},
		{"FC4 address", breaker, pdu(4, 10, 2), 2}, {"FC4 quantity", breaker, pdu(4, 0, 0), 2}, {"FC4 overflow", breaker, pdu(4, 65535, 2), 2},
		{"FC6 address", breaker, pdu(6, 1, 1), 2}, {"FC6 value", breaker, pdu(6, 0, 0), 3},
		{"OLTC bad address", oltc, pdu(6, 3, 1), 2}, {"OLTC bad step", oltc, pdu(6, 0, 3), 3}, {"OLTC bad mode", oltc, pdu(6, 2, 0), 3},
		{"OLTC HR1 91", oltc, pdu(6, 1, 91), 3}, {"OLTC HR1 109", oltc, pdu(6, 1, 109), 3}, {"OLTC HR1 1", oltc, pdu(6, 1, 1), 3},
		{"FC16 quantity >1", oltc, fc16(1, 2, 100, 101), 3}, {"FC16 quantity 0", oltc, fc16(1, 0), 3},
		{"FC16 byte count", oltc, []byte{16, 0, 1, 0, 1, 1, 0, 0}, 3},
		{"FC16 wrong address", oltc, fc16(0, 1, 1), 2}, {"FC16 breaker", breaker, fc16(1, 1, 0), 2},
		{"FC16 invalid encoding", oltc, fc16(1, 1, 109), 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.server.HandlePDU(tt.request, "10.30.30.20:43001")
			if !bytes.Equal(r, []byte{tt.request[0] | 0x80, tt.code}) {
				t.Fatalf("response=%v", r)
			}
		})
	}
	for _, function := range []byte{3, 4, 6, 16} {
		length := 5
		if function == 16 {
			length = 8
		}
		for _, badLen := range []int{1, length - 1, length + 1} {
			request := make([]byte, badLen)
			request[0] = function
			if r := oltc.HandlePDU(request, ""); !bytes.Equal(r, []byte{function | 0x80, 3}) {
				t.Fatalf("FC%d length %d response=%v", function, badLen, r)
			}
		}
	}
}

func TestFC16AcknowledgesIgnoredRequestsAndArmsWithZero(t *testing.T) {
	model, server := newModelServer(t, fielddevice.OLTC)
	for _, tt := range []struct {
		value  uint16
		result fielddevice.RequestResult
		armed  bool
	}{
		{101, fielddevice.RequestUnarmed, false}, {0, fielddevice.RequestNone, true},
	} {
		request := fc16(1, 1, tt.value)
		response := server.HandlePDU(request, "10.30.30.30:43001")
		if !bytes.Equal(response, []byte{16, 0, 1, 0, 1}) {
			t.Fatalf("FC16 response=%v", response)
		}
		if model.Truth().Armed != tt.armed || model.Snapshot().PLCRequestResult != tt.result {
			t.Fatal("FC16 model effect wrong")
		}
	}
	apply(t, model, false)
	request := fc16(1, 1, 101)
	if r := server.HandlePDU(request, "10.30.30.30:43001"); !bytes.Equal(r, request[:5]) {
		t.Fatal("FC16 accepted step response")
	}
	if !model.Truth().Moving {
		t.Fatal("FC16 did not start motion")
	}
	server.HandlePDU(pdu(6, 1, 101), "10.30.30.30:43001") // Active target is a well-formed no-op.
	entries, _, _ := model.AuditAfter(0)
	count := 0
	for _, e := range entries {
		if e.Operation == "plc_request" && e.Result == "accepted" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("accepted HR1 audited more than once")
	}
	if _, err := model.Action(context.Background(), "reset", fielddevice.AuditContext{Protocol: "lab"}); err != nil {
		t.Fatal(err)
	}
}

func TestRealModelInterlocksUseException04(t *testing.T) {
	for _, kind := range []fielddevice.Kind{fielddevice.Breaker, fielddevice.RMU, fielddevice.OLTC} {
		t.Run(string(kind), func(t *testing.T) {
			model, server := newModelServer(t, kind)
			request := pdu(6, 0, 2) // No measurement for switch close; auto refuses OLTC manual.
			if r := server.HandlePDU(request, "10.30.30.20:43001"); !bytes.Equal(r, []byte{0x86, 4}) {
				t.Fatalf("interlock response=%v", r)
			}
			if kind == fielddevice.Breaker {
				apply(t, model, true)
				if r := server.HandlePDU(request, ""); !bytes.Equal(r, []byte{0x86, 4}) {
					t.Fatal("lockout accepted")
				}
			}
		})
	}
}

func TestFloatHighWordFirst(t *testing.T) {
	r := make([]uint16, 2)
	putFloat32(r, float64(math.Float32frombits(0x12345678)))
	assertRegisters(t, r, []uint16{0x1234, 0x5678})
}

func TestTCPTransactionCoalescedFramesAndUnit(t *testing.T) {
	model, server := newModelServer(t, fielddevice.Breaker)
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	_ = clientConn.SetDeadline(time.Now().Add(2 * time.Second))
	done := make(chan struct{})
	go func() { server.ServeConn(serverConn); close(done) }()
	wrongUnit := modbusFrame(1, pdu(6, 0, 1))
	wrongUnit[6] = 2
	first := modbusFrame(0x1234, pdu(3, 0, 1))
	second := modbusFrame(0xabcd, pdu(4, 0, 1))
	writeErr := make(chan error, 1)
	go func() { _, err := clientConn.Write(append(append(wrongUnit, first...), second...)); writeErr <- err }()
	for _, transaction := range []uint16{0x1234, 0xabcd} {
		header := make([]byte, 7)
		if _, err := io.ReadFull(clientConn, header); err != nil {
			t.Fatal(err)
		}
		if got := binary.BigEndian.Uint16(header[:2]); got != transaction {
			t.Fatalf("transaction=%#04x", got)
		}
		length := int(binary.BigEndian.Uint16(header[4:6]))
		response := make([]byte, length-1)
		if _, err := io.ReadFull(clientConn, response); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(response, []byte{response[0], 2, 0, 2}) {
			t.Fatalf("response=%v", response)
		}
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
	if model.Truth().Position != fielddevice.PositionClosed {
		t.Fatal("wrong unit executed write")
	}
	_ = clientConn.Close()
	<-done
}
func modbusFrame(transaction uint16, pdu []byte) []byte {
	frame := make([]byte, 7+len(pdu))
	binary.BigEndian.PutUint16(frame[:2], transaction)
	binary.BigEndian.PutUint16(frame[4:6], uint16(1+len(pdu)))
	frame[6] = UnitID
	copy(frame[7:], pdu)
	return frame
}
