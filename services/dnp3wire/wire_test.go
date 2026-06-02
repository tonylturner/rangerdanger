// Package dnp3wire holds wire-level integration tests for the DNP3 outstations
// the field-device simulators expose (relay, recloser, regulator, capbank,
// RTAC).
//
// These tests boot a real dnp3go outstation on an ephemeral TCP port using
// point maps that mirror the sims, then drive it with the same dnp3go master
// the dnp3poll/dnp3cmd lab tools use. They exist to catch lab-fidelity
// regressions at the protocol layer that the sims' HTTP-level tests cannot:
//
//   - the data-block CRC framing bug, which silently corrupted any integrity-
//     poll response longer than one 16-byte link block (the RTAC's 15 binary
//     + 8 analog inputs were the worst case — analog values came back garbage);
//   - Direct Operate (FC 0x05), Direct Operate No-Ack (FC 0x06), and
//     Select-Before-Operate (FC 0x03 + 0x04) all driving a sim's Operate
//     callback.
//
// If a future dnp3go change breaks framing or a control path, these fail with
// a point-level diff instead of a mysteriously wrong value in a lab terminal.
package dnp3wire

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/tonylturner/dnp3go"
)

// listenEphemeral starts the outstation on 127.0.0.1:0 and returns the address.
// dnp3go.ListenAndServe binds a fixed port and blocks, so we replicate its
// accept loop here against a :0 listener to get a test-friendly endpoint.
func listenEphemeral(t *testing.T, cfg *dnp3go.OutstationConfig) string {
	t.Helper()
	cfg.Logger = func(string, ...any) {} // silence
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go dnp3go.ServeConn(conn, cfg)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

// rtacConfig mirrors services/rtac-sim/dnp3.go: 15 binary inputs + 8 analog
// inputs, read-only. This is the framing bug's worst case.
func rtacConfig() (*dnp3go.OutstationConfig, []bool, []float32) {
	bools := []bool{true, true, false, true, true, true, false, true, true, true, false, false, true, false, true}
	floats := []float32{13800, 7200, 7180, 142.5, 850.25, 410.75, -3, 2}

	var bis []dnp3go.BinaryPoint
	for i := range bools {
		i := i
		bis = append(bis, dnp3go.BinaryPoint{Index: uint16(i), Read: func() bool { return bools[i] }})
	}
	var ais []dnp3go.AnalogPoint
	for i := range floats {
		i := i
		ais = append(ais, dnp3go.AnalogPoint{Index: uint16(i), Read: func() float32 { return floats[i] }})
	}
	return &dnp3go.OutstationConfig{
		OutstationAddr:  10,
		AcceptAnyMaster: true,
		BinaryInputs:    bis,
		AnalogInputs:    ais,
	}, bools, floats
}

// relayConfig mirrors services/relay-sim/dnp3.go: a controllable breaker output
// gated on remote-control-enabled, plus binary and analog inputs.
type relayState struct {
	mu            sync.Mutex
	breakerClosed bool
	remoteEnabled bool
	operates      int
}

func relayConfig(st *relayState) *dnp3go.OutstationConfig {
	return &dnp3go.OutstationConfig{
		OutstationAddr:  1,
		AcceptAnyMaster: true,
		BinaryInputs: []dnp3go.BinaryPoint{
			{Index: 0, Read: func() bool { st.mu.Lock(); defer st.mu.Unlock(); return st.remoteEnabled }},
			{Index: 1, Read: func() bool { return false }},
			{Index: 2, Read: func() bool { return false }},
			{Index: 3, Read: func() bool { return true }},
		},
		BinaryOutputs: []dnp3go.BinaryOutputPoint{
			{
				Index: 0,
				Read:  func() bool { st.mu.Lock(); defer st.mu.Unlock(); return st.breakerClosed },
				Operate: func(cc uint8) uint8 {
					st.mu.Lock()
					defer st.mu.Unlock()
					if !st.remoteEnabled {
						return dnp3go.CROBStatusBlocked
					}
					switch cc {
					case dnp3go.CROBTripPulse, dnp3go.CROBLatchOff, dnp3go.CROBPulseOff:
						st.breakerClosed = false
					case dnp3go.CROBClosePulse, dnp3go.CROBLatchOn, dnp3go.CROBPulseOn:
						st.breakerClosed = true
					default:
						return dnp3go.CROBStatusNotSupported
					}
					st.operates++
					return dnp3go.CROBStatusSuccess
				},
			},
		},
		AnalogInputs: []dnp3go.AnalogPoint{
			{Index: 0, Read: func() float32 { return 142.5 }},
			{Index: 1, Read: func() float32 { return 13.8 }},
		},
	}
}

// TestRTACIntegrityPollRoundTrip is the headline guard for the framing fix:
// every one of the RTAC's 15 binary + 8 analog inputs must survive a Class-0
// integrity poll, including the points that fall past the first 16-byte link
// block (where the old CRC-aliasing bug corrupted the wire).
func TestRTACIntegrityPollRoundTrip(t *testing.T) {
	cfg, wantBools, wantFloats := rtacConfig()
	addr := listenEphemeral(t, cfg)

	res := dnp3go.Poll(&dnp3go.MasterConfig{
		MasterAddr: 100, OutstationAddr: 10, Endpoint: addr, Timeout: 2 * time.Second,
	})
	if res.Error != nil {
		t.Fatalf("poll: %v", res.Error)
	}

	if len(res.BinaryInputs) != len(wantBools) {
		t.Errorf("binary input count: got %d want %d", len(res.BinaryInputs), len(wantBools))
	}
	for i, want := range wantBools {
		got, ok := res.BinaryInputs[uint16(i)]
		if !ok {
			t.Errorf("binary input %d missing from poll response", i)
			continue
		}
		if got != want {
			t.Errorf("binary input %d: got %v want %v", i, got, want)
		}
	}

	if len(res.AnalogInputs) != len(wantFloats) {
		t.Errorf("analog input count: got %d want %d", len(res.AnalogInputs), len(wantFloats))
	}
	for i, want := range wantFloats {
		got, ok := res.AnalogInputs[uint16(i)]
		if !ok {
			t.Errorf("analog input %d missing from poll response (framing bug?)", i)
			continue
		}
		if got != want {
			t.Errorf("analog input %d: got %v want %v", i, got, want)
		}
	}
}

// crobAPDU builds a CROB control APDU for a given function code.
func crobAPDU(fc, seq, index, cc uint8) []byte {
	apdu := []byte{0xC0 | (seq & 0x0F), fc, dnp3go.GroupCROB, dnp3go.VarCROB, dnp3go.QualCount8Pfx8, 1, index}
	crob := make([]byte, 11)
	crob[0] = cc
	crob[1] = 1
	return append(apdu, crob...)
}

// sendOnConn writes one APDU and returns the parsed response (nil if none).
func sendOnConn(t *testing.T, conn net.Conn, tpSeq uint8, apdu []byte, expectReply bool) *dnp3go.APDU {
	t.Helper()
	f := &dnp3go.LinkFrame{Control: 0xC4, Dest: 1, Source: 100, Payload: dnp3go.WrapTransport(apdu, tpSeq)}
	if err := dnp3go.WriteLinkFrame(conn, f); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !expectReply {
		conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		if _, err := dnp3go.ReadLinkFrame(conn); err == nil {
			t.Fatalf("expected no reply for no-ack, but got one")
		}
		return nil
	}
	rf, err := dnp3go.ReadLinkFrame(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	tr := &dnp3go.TransportReader{}
	ad := tr.ProcessSegment(rf.Payload)
	if ad == nil {
		t.Fatalf("incomplete transport")
	}
	ap, err := dnp3go.ParseAPDU(ad)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return ap
}

func lastStatus(t *testing.T, ap *dnp3go.APDU) uint8 {
	t.Helper()
	for _, oh := range ap.Objects {
		for _, po := range oh.PrefixedData {
			if len(po.Data) > 0 {
				return po.Data[len(po.Data)-1]
			}
		}
	}
	t.Fatalf("no status object in response")
	return 0
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c.SetDeadline(time.Now().Add(2 * time.Second))
	t.Cleanup(func() { c.Close() })
	return c
}

// TestRelayDirectOperateTrip exercises FC 0x05 against the relay breaker,
// the path dnp3cmd's default mode uses (`dnp3cmd ... crob 0 trip`).
func TestRelayDirectOperateTrip(t *testing.T) {
	st := &relayState{breakerClosed: true, remoteEnabled: true}
	addr := listenEphemeral(t, relayConfig(st))
	conn := dial(t, addr)

	resp := sendOnConn(t, conn, 0, crobAPDU(dnp3go.FCDirectOperate, 0, 0, dnp3go.CROBTripPulse), true)
	if s := lastStatus(t, resp); s != dnp3go.CROBStatusSuccess {
		t.Fatalf("trip status: got 0x%02X want success", s)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.breakerClosed {
		t.Errorf("breaker should be open after trip")
	}
}

// TestRelayDirectOperateBlockedByRemoteDisable confirms the sim's safety gate
// still surfaces over the wire: with remote control disabled, the CROB is
// rejected with the BLOCKED status dnp3cmd renders to students.
func TestRelayDirectOperateBlockedByRemoteDisable(t *testing.T) {
	st := &relayState{breakerClosed: true, remoteEnabled: false}
	addr := listenEphemeral(t, relayConfig(st))
	conn := dial(t, addr)

	resp := sendOnConn(t, conn, 0, crobAPDU(dnp3go.FCDirectOperate, 0, 0, dnp3go.CROBTripPulse), true)
	if s := lastStatus(t, resp); s != dnp3go.CROBStatusBlocked {
		t.Errorf("status: got 0x%02X want BLOCKED (0x07)", s)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.breakerClosed {
		t.Errorf("breaker must stay closed when remote control disabled")
	}
}

// TestRelayDirectOperateNoAck exercises FC 0x06: the control takes effect but
// the outstation sends no reply (`dnp3cmd ... -no-ack crob 0 trip`).
func TestRelayDirectOperateNoAck(t *testing.T) {
	st := &relayState{breakerClosed: true, remoteEnabled: true}
	addr := listenEphemeral(t, relayConfig(st))
	conn := dial(t, addr)

	sendOnConn(t, conn, 0, crobAPDU(dnp3go.FCDirectOperateNoAck, 0, 0, dnp3go.CROBTripPulse), false)

	deadline := time.Now().Add(time.Second)
	for {
		st.mu.Lock()
		open := !st.breakerClosed
		ops := st.operates
		st.mu.Unlock()
		if open && ops == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no-ack operate did not take effect: open=%v ops=%d", open, ops)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRelaySelectBeforeOperate exercises the SBO path (FC 0x03 then 0x04) on a
// single connection (`dnp3cmd ... -sbo crob 0 trip`).
func TestRelaySelectBeforeOperate(t *testing.T) {
	st := &relayState{breakerClosed: true, remoteEnabled: true}
	addr := listenEphemeral(t, relayConfig(st))
	conn := dial(t, addr)

	selResp := sendOnConn(t, conn, 0, crobAPDU(dnp3go.FCSelect, 0, 0, dnp3go.CROBTripPulse), true)
	if s := lastStatus(t, selResp); s != dnp3go.CROBStatusSuccess {
		t.Fatalf("select status: got 0x%02X want success", s)
	}
	opResp := sendOnConn(t, conn, 1, crobAPDU(dnp3go.FCOperate, 1, 0, dnp3go.CROBTripPulse), true)
	if s := lastStatus(t, opResp); s != dnp3go.CROBStatusSuccess {
		t.Fatalf("operate status: got 0x%02X want success", s)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.breakerClosed {
		t.Errorf("breaker should be open after SBO trip")
	}
}
