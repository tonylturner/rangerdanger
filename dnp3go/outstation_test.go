package dnp3go

import (
	"net"
	"sync"
	"testing"
	"time"
)

// These tests drive a real in-process outstation over a loopback TCP
// connection, exercising the same paths the lab tools (dnp3poll / dnp3cmd)
// use: integrity poll (FC 0x01), Direct Operate (FC 0x05), Direct Operate
// No-Ack (FC 0x06), and Select-Before-Operate (FC 0x03 + FC 0x04).

// testOutstation holds a running outstation plus the mutable point state the
// control callbacks act on, so tests can assert that operates took effect.
type testOutstation struct {
	addr     string
	mu       sync.Mutex
	breaker  bool    // binary output index 0
	tap      float32 // analog output index 0
	operates int     // count of successful binary operates
}

func newTestOutstation(t *testing.T) *testOutstation {
	t.Helper()
	ts := &testOutstation{}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ts.addr = ln.Addr().String()

	cfg := &OutstationConfig{
		OutstationAddr:  1,
		AcceptAnyMaster: true,
		Logger:          func(string, ...any) {}, // silence
		BinaryInputs: []BinaryPoint{
			{Index: 0, Read: func() bool { return true }},
		},
		BinaryOutputs: []BinaryOutputPoint{
			{
				Index: 0,
				Read:  func() bool { ts.mu.Lock(); defer ts.mu.Unlock(); return ts.breaker },
				Operate: func(cc uint8) uint8 {
					ts.mu.Lock()
					defer ts.mu.Unlock()
					// Trip (0x81) opens, Close (0x41)/Latch-On (0x03) closes.
					switch cc {
					case CROBTripPulse, CROBLatchOff:
						ts.breaker = false
					default:
						ts.breaker = true
					}
					ts.operates++
					return CROBStatusSuccess
				},
			},
		},
		AnalogInputs: []AnalogPoint{
			{Index: 0, Read: func() float32 { return 12.5 }},
		},
		AnalogOutputs: []AnalogOutputPoint{
			{
				Index: 0,
				Read:  func() float32 { ts.mu.Lock(); defer ts.mu.Unlock(); return ts.tap },
				Operate: func(v float32) uint8 {
					ts.mu.Lock()
					defer ts.mu.Unlock()
					ts.tap = v
					return 0
				},
			},
		},
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConn(conn, cfg)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ts
}

// sendAPDU opens a connection, sends one control/read APDU, and returns the
// parsed response APDU (or nil if the outstation sent nothing).
func sendAPDU(t *testing.T, addr string, tpSeq uint8, apdu []byte, expectReply bool) *APDU {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	frame := &LinkFrame{
		Control: ctrlDIR | ctrlPRM | ctrlFCUnconfirmedData,
		Dest:    1,
		Source:  100,
		Payload: WrapTransport(apdu, tpSeq),
	}
	if err := WriteLinkFrame(conn, frame); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !expectReply {
		// Give the outstation a moment, then confirm no frame arrives.
		conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		if _, err := ReadLinkFrame(conn); err == nil {
			t.Fatalf("expected no reply for no-ack request, but got one")
		}
		return nil
	}

	respFrame, err := ReadLinkFrame(conn)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	tr := &TransportReader{}
	appData := tr.ProcessSegment(respFrame.Payload)
	if appData == nil {
		t.Fatalf("incomplete transport")
	}
	apduResp, err := ParseAPDU(appData)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return apduResp
}

func crobAPDU(fc, seq, index, cc uint8) []byte {
	apdu := []byte{0xC0 | (seq & 0x0F), fc, GroupCROB, VarCROB, QualCount8Pfx8, 1, index}
	crob := make([]byte, 11)
	crob[0] = cc
	crob[1] = 1
	return append(apdu, crob...)
}

func lastStatus(t *testing.T, apdu *APDU) uint8 {
	t.Helper()
	for _, oh := range apdu.Objects {
		for _, po := range oh.PrefixedData {
			if len(po.Data) > 0 {
				return po.Data[len(po.Data)-1]
			}
		}
	}
	t.Fatalf("no status object in response")
	return 0
}

func TestIntegrityPollReadsPoints(t *testing.T) {
	ts := newTestOutstation(t)
	res := Poll(&MasterConfig{
		MasterAddr: 100, OutstationAddr: 1, Endpoint: ts.addr, Timeout: 2 * time.Second,
	})
	if res.Error != nil {
		t.Fatalf("poll: %v", res.Error)
	}
	if v, ok := res.BinaryInputs[0]; !ok || !v {
		t.Errorf("binary input 0: got %v ok=%v, want true", v, ok)
	}
	if v, ok := res.AnalogInputs[0]; !ok || v != 12.5 {
		t.Errorf("analog input 0: got %v ok=%v, want 12.5", v, ok)
	}
	// IIN1.7 Device Restart must be set on the very first response.
	if res.IIN&(uint16(IIN1DeviceRestart)<<8) == 0 {
		t.Errorf("expected Device Restart IIN bit on first response, got 0x%04X", res.IIN)
	}
}

func TestDirectOperateCROB(t *testing.T) {
	ts := newTestOutstation(t)
	resp := sendAPDU(t, ts.addr, 0, crobAPDU(FCDirectOperate, 0, 0, CROBTripPulse), true)
	if st := lastStatus(t, resp); st != CROBStatusSuccess {
		t.Fatalf("status: got 0x%02X want success", st)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.breaker {
		t.Errorf("breaker should be open after trip")
	}
	if ts.operates != 1 {
		t.Errorf("operate count: got %d want 1", ts.operates)
	}
}

func TestDirectOperateNoAck(t *testing.T) {
	ts := newTestOutstation(t)
	// FC 0x06: operate happens, but NO response is sent.
	sendAPDU(t, ts.addr, 0, crobAPDU(FCDirectOperateNoAck, 0, 0, CROBLatchOn), false)

	// Poll on a fresh connection to confirm the operate actually took effect.
	deadline := time.Now().Add(time.Second)
	for {
		ts.mu.Lock()
		ops := ts.operates
		closed := ts.breaker
		ts.mu.Unlock()
		if ops == 1 && closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no-ack operate did not take effect: ops=%d breaker=%v", ops, closed)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSelectBeforeOperate(t *testing.T) {
	ts := newTestOutstation(t)
	conn, err := net.Dial("tcp", ts.addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	send := func(apdu []byte, tpSeq uint8) *APDU {
		f := &LinkFrame{Control: ctrlDIR | ctrlPRM | ctrlFCUnconfirmedData, Dest: 1, Source: 100, Payload: WrapTransport(apdu, tpSeq)}
		if err := WriteLinkFrame(conn, f); err != nil {
			t.Fatalf("write: %v", err)
		}
		rf, err := ReadLinkFrame(conn)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		tr := &TransportReader{}
		ad := tr.ProcessSegment(rf.Payload)
		ap, err := ParseAPDU(ad)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return ap
	}

	// Select then Operate on the SAME connection (SBO state is per-connection).
	selResp := send(crobAPDU(FCSelect, 0, 0, CROBTripPulse), 0)
	if st := lastStatus(t, selResp); st != CROBStatusSuccess {
		t.Fatalf("select status: got 0x%02X want success", st)
	}
	opResp := send(crobAPDU(FCOperate, 1, 0, CROBTripPulse), 1)
	if st := lastStatus(t, opResp); st != CROBStatusSuccess {
		t.Fatalf("operate status: got 0x%02X want success", st)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.breaker {
		t.Errorf("breaker should be open after SBO trip")
	}
}

func TestOperateWithoutSelectFails(t *testing.T) {
	ts := newTestOutstation(t)
	// Operate (FC 0x04) with no prior Select must return NO_SELECT and must
	// NOT actuate the point.
	resp := sendAPDU(t, ts.addr, 0, crobAPDU(FCOperate, 0, 0, CROBTripPulse), true)
	if st := lastStatus(t, resp); st != CROBStatusNoSelect {
		t.Errorf("status: got 0x%02X want NoSelect (0x02)", st)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.operates != 0 {
		t.Errorf("operate count: got %d want 0 (no select)", ts.operates)
	}
}

func TestUnknownFunctionCodeSetsIIN(t *testing.T) {
	ts := newTestOutstation(t)
	// FC 0x10 is not implemented — outstation must set IIN1.0 (No Func Code).
	resp := sendAPDU(t, ts.addr, 0, []byte{0xC0, 0x10}, true)
	if resp.IIN&(uint16(IIN1NoFuncCode)<<8) == 0 {
		t.Errorf("expected No-Func-Code IIN bit, got 0x%04X", resp.IIN)
	}
}
