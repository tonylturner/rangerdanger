package dnp3go

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// These application vectors follow IEEE 1815 object layouts, independently
// of the library's encoders: G41V3 float then status; G12V1 control, count,
// on/off milliseconds, status. They deliberately use nonzero/negative values.
func TestCommandByteVectors(t *testing.T) {
	analog := []byte{41, 3, 0x17, 1, 0, 0, 0, 0x80, 0xc1, 7}
	if got := EncodeAnalogOutputResponse(0, -16, 7); !bytes.Equal(got, analog) {
		t.Fatalf("G41V3: got %x want %x", got, analog)
	}
	if value, ok := ParseAnalogOutputCmd(analog[5:]); !ok || value != -16 {
		t.Fatalf("G41V3 decoded value=%g ok=%v", value, ok)
	}
	crob := []byte{0x81, 3, 0xe8, 3, 0, 0, 0xd0, 7, 0, 0, 0}
	want := []byte{12, 1, 0x28, 1, 0, 0x23, 1, 0x81, 3, 0xe8, 3, 0, 0, 0xd0, 7, 0, 0, 7}
	if got := EncodeCROBResponse(0x123, crob, 7); !bytes.Equal(got, want) {
		t.Fatalf("CROB echo: got %x want %x", got, want)
	}
	if crob[10] != 0 {
		t.Fatal("response encoder mutated request")
	}
}

func TestLinkPayloadLimit(t *testing.T) {
	for _, size := range []int{249, 250, 251, 506} {
		var buf bytes.Buffer
		err := WriteLinkFrame(&buf, &LinkFrame{Control: 0x44, Payload: make([]byte, size)})
		if size > 250 {
			if !errors.Is(err, ErrFrameTooLarge) || buf.Len() != 0 {
				t.Fatalf("size %d: error=%v wrote=%d", size, err, buf.Len())
			}
		} else if err != nil || buf.Bytes()[2] != byte(size+5) {
			t.Fatalf("size %d: error=%v header=%x", size, err, buf.Bytes())
		}
	}
	if MakeResponseControl() != 0x44 {
		t.Fatalf("outstation control=%02x, want DIR=0 PRM=1 FC=4", MakeResponseControl())
	}
}

func TestEventClassesHaveNoStaticData(t *testing.T) {
	ts := newTestOutstation(t)
	for _, variation := range []byte{2, 3, 4} {
		r := sendAPDU(t, ts.addr, 0, []byte{0xc0, 1, 60, variation, 6}, true)
		if r.IIN != 0x8000 || len(r.Objects) != 0 {
			t.Fatalf("G60V%d: %+v", variation, r)
		}
	}
	r := sendAPDU(t, ts.addr, 0, []byte{0xc0, 1, 60, 2, 6, 60, 1, 6}, true)
	if len(r.Objects) != 4 {
		t.Fatalf("combined event/static poll lost static data: %+v", r)
	}
}

func TestInvalidWritesDoNotClearRestart(t *testing.T) {
	ts := newTestOutstation(t)
	for _, object := range [][]byte{
		nil,
		{80, 1, 0, 7, 7},          // missing packed bit
		{80, 1, 0, 7},             // truncated range
		{80, 1, 0, 7, 7, 1},       // cannot set the restart indication
		{80, 1, 0, 6, 6, 0},       // wrong IIN index
		{80, 1, 0, 8, 7, 0},       // reversed range
		{80, 1, 6},                // no range/value
		{80, 2, 0, 7, 7, 0},       // unknown variation
		{80, 1, 0, 7, 7, 0, 0xff}, // valid clear followed by truncated object
	} {
		r := sendAPDU(t, ts.addr, 0, append([]byte{0xc0, 2}, object...), true)
		if r.IIN&0x8000 == 0 || r.IIN&0xff == 0 {
			t.Fatalf("invalid write %x: IIN=%04x", object, r.IIN)
		}
		r = sendAPDU(t, ts.addr, 0, []byte{0xc0, 1, 60, 1, 6}, true)
		if r.IIN != 0x8000 {
			t.Fatalf("invalid write %x cleared restart: IIN=%04x", object, r.IIN)
		}
	}
	r := sendAPDU(t, ts.addr, 0, []byte{0xc0, 2, 80, 1, 1, 7, 0, 7, 0, 0}, true)
	if r.IIN != 0 {
		t.Fatalf("16-bit range clear: IIN=%04x", r.IIN)
	}
}

func TestSBOMatchesEntireObject(t *testing.T) {
	// No encoder is used for the SELECT/OPERATE command objects.
	for _, tc := range []struct {
		name   string
		object []byte
		change int
	}{
		{"analog-value", []byte{41, 3, 0x17, 1, 0, 0, 0, 0x80, 0xc1, 0}, 7},
		{"analog-index", []byte{41, 3, 0x17, 1, 0, 0, 0, 0x80, 0xc1, 0}, 4},
		{"crob-control", []byte{12, 1, 0x17, 1, 0, 0x81, 3, 0xe8, 3, 0, 0, 0xd0, 7, 0, 0, 0}, 5},
		{"crob-count", []byte{12, 1, 0x17, 1, 0, 0x81, 3, 0xe8, 3, 0, 0, 0xd0, 7, 0, 0, 0}, 6},
		{"crob-on-time", []byte{12, 1, 0x17, 1, 0, 0x81, 3, 0xe8, 3, 0, 0, 0xd0, 7, 0, 0, 0}, 7},
		{"crob-off-time", []byte{12, 1, 0x17, 1, 0, 0x81, 3, 0xe8, 3, 0, 0, 0xd0, 7, 0, 0, 0}, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cfg := &OutstationConfig{
				BinaryOutputs: []BinaryOutputPoint{{Index: 0, Operate: func(uint8) uint8 { calls++; return 0 }}},
				AnalogOutputs: []AnalogOutputPoint{{Index: 0, Operate: func(float32) uint8 { calls++; return 0 }}},
			}
			cs := &connState{}
			parse := func(object []byte) *APDU {
				t.Helper()
				a, err := ParseAPDU(append([]byte{0xc0, 3}, object...))
				if err != nil {
					t.Fatal(err)
				}
				return a
			}
			handleSelect(cfg, cs, parse(tc.object))
			changed := append([]byte(nil), tc.object...)
			changed[tc.change] ^= 1
			got, iin := handleOperate(cfg, cs, parse(changed))
			if calls != 0 || iin != 0 || got[len(got)-1] != 2 {
				t.Fatalf("mismatch: calls=%d IIN=%04x response=%x", calls, iin, got)
			}
			// A failed OPERATE consumes the selection; retries need SELECT.
			got, _ = handleOperate(cfg, cs, parse(tc.object))
			if got[len(got)-1] != 2 || calls != 0 {
				t.Fatalf("selection reused: %x calls=%d", got, calls)
			}
			handleSelect(cfg, cs, parse(tc.object))
			cs.pending[0].expires = time.Now().Add(-time.Second)
			got, _ = handleOperate(cfg, cs, parse(tc.object))
			if got[len(got)-1] != 2 || calls != 0 {
				t.Fatalf("expired selection used: %x calls=%d", got, calls)
			}
			handleSelect(cfg, cs, parse(tc.object))
			got, _ = handleOperate(cfg, cs, parse(tc.object))
			if got[len(got)-1] != 0 || calls != 1 {
				t.Fatalf("matching selection: %x calls=%d", got, calls)
			}
		})
	}
}

func TestControlResponseEchoOnWire(t *testing.T) {
	ts := newTestOutstation(t)
	for _, tc := range []struct {
		object []byte
		status byte
	}{
		{[]byte{12, 1, 0x17, 1, 0, 0x81, 3, 0xe8, 3, 0, 0, 0xd0, 7, 0, 0, 0}, 0},
		{[]byte{12, 1, 0x17, 1, 99, 0x81, 3, 0xe8, 3, 0, 0, 0xd0, 7, 0, 0, 0}, 4},
		{[]byte{41, 3, 0x17, 1, 0, 0, 0, 0x80, 0xc1, 0}, 0},
	} {
		conn, err := net.DialTimeout("tcp", ts.addr, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		request := append([]byte{0xc9, 5}, tc.object...)
		if err := WriteLinkFrame(conn, &LinkFrame{Control: 0xc4, Dest: 1, Source: 100, Payload: append([]byte{0xc0}, request...)}); err != nil {
			t.Fatal(err)
		}
		frame, err := ReadLinkFrame(conn)
		conn.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := append([]byte{0xc0, 0xc9, 0x81, 0x80, 0}, tc.object...)
		want[len(want)-1] = tc.status
		if frame.Control != 0x44 || frame.Dest != 100 || frame.Source != 1 || !bytes.Equal(frame.Payload, want) {
			t.Fatalf("frame=%+v payload=%x want=%x", frame, frame.Payload, want)
		}
	}
}

func TestOversizedOutstationResponseRejected(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))
	logged := make(chan string, 2)
	cfg := &OutstationConfig{OutstationAddr: 1, AcceptAnyMaster: true, Logger: func(s string, args ...any) {
		if len(args) == 1 {
			if err, ok := args[0].(error); ok && errors.Is(err, ErrFrameTooLarge) {
				logged <- s
			}
		}
	}}
	for i := 0; i < 49; i++ {
		cfg.AnalogInputs = append(cfg.AnalogInputs, AnalogPoint{Index: uint16(i), Read: func() float32 { return 0 }})
	}
	go ServeConn(server, cfg)
	if err := WriteLinkFrame(client, &LinkFrame{Control: 0xc4, Dest: 1, Source: 100, Payload: []byte{0xc0, 0xc0, 1, 60, 1, 6}}); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if n, err := client.Read(b[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("oversized response wrote bytes or failed to close: n=%d err=%v", n, err)
	}
	select {
	case <-logged:
	case <-time.After(time.Second):
		t.Fatal("oversized response not reported")
	}
}
