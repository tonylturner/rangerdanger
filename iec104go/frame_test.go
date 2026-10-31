// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
	"testing/iotest"
)

func TestFrameVectors(t *testing.T) {
	for _, tc := range []struct {
		name, hex string
		frame     Frame
	}{
		{"I", "6809000002000102030405", Frame{Kind: IFrame, Receive: 1, ASDU: []byte{1, 2, 3, 4, 5}}},
		{"counter-max", "6805fefffeffab", Frame{Kind: IFrame, Send: 32767, Receive: 32767, ASDU: []byte{0xab}}},
		{"S-zero", "680401000000", Frame{Kind: SFrame}},
		{"S-max", "68040100feff", Frame{Kind: SFrame, Receive: 32767}},
		{"START-act", "680407000000", Frame{Kind: UFrame, Function: StartDTAct}},
		{"START-con", "68040b000000", Frame{Kind: UFrame, Function: StartDTCon}},
		{"STOP-act", "680413000000", Frame{Kind: UFrame, Function: StopDTAct}},
		{"STOP-con", "680423000000", Frame{Kind: UFrame, Function: StopDTCon}},
		{"TEST-act", "680443000000", Frame{Kind: UFrame, Function: TestFRAct}},
		{"TEST-con", "680483000000", Frame{Kind: UFrame, Function: TestFRCon}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := unhex(t, tc.hex)
			b, err := tc.frame.MarshalBinary()
			if err != nil || !bytes.Equal(b, want) {
				t.Fatalf("encode=%x %v want=%x", b, err, want)
			}
			f, err := DecodeFrame(want)
			if err != nil || !reflect.DeepEqual(f, tc.frame) {
				t.Fatalf("decode=%+v %v", f, err)
			}
		})
	}
}

func TestMalformedFrame(t *testing.T) {
	for _, wire := range []string{
		"", "68", "6803", "6803000000", "680000000000", "68fe00000000", "690407000000",
		"6804070000", "68040700000000", "680500000000", "680400000000",
		"680401010000", "680401000100", "68050100000000", "68050700000000",
		"680407010000", "680407000100", "680407000001", "68040f000000", "680403000000",
		"680500000100ab",
	} {
		t.Run(wire, func(t *testing.T) {
			if _, err := DecodeFrame(unhex(t, wire)); !errors.Is(err, ErrMalformed) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	for _, f := range []Frame{
		{Kind: FrameKind(3)}, {Kind: IFrame}, {Kind: IFrame, ASDU: make([]byte, 250)},
		{Kind: IFrame, Send: 32768, ASDU: []byte{1}}, {Kind: SFrame, Receive: 32768},
		{Kind: IFrame, Function: StartDTAct, ASDU: []byte{1}},
		{Kind: SFrame, Send: 1}, {Kind: SFrame, Function: StartDTAct}, {Kind: SFrame, ASDU: []byte{1}},
		{Kind: UFrame}, {Kind: UFrame, Function: StartDTAct, Send: 1},
		{Kind: UFrame, Function: StartDTAct, Receive: 1}, {Kind: UFrame, Function: StartDTAct, ASDU: []byte{1}},
	} {
		if _, err := f.MarshalBinary(); !errors.Is(err, ErrMalformed) {
			t.Fatalf("accepted %+v: %v", f, err)
		}
	}
}

func TestAPDUReader(t *testing.T) {
	// Include multiple I frames to ensure retaining the first one across future
	// calls cannot alias a reused stream buffer.
	stream := unhex(t, "680407000000680500000000ab680401000200680502000200cd68040b000000")
	for _, oneByte := range []bool{false, true} {
		t.Run(fmt.Sprintf("fragmented=%v", oneByte), func(t *testing.T) {
			var input io.Reader = bytes.NewReader(stream)
			if oneByte {
				input = iotest.OneByteReader(input)
			}
			reader := NewAPDUReader(input)
			var frames []Frame
			for i := 0; i < 5; i++ {
				f, err := reader.ReadFrame()
				if err != nil {
					t.Fatal(err)
				}
				frames = append(frames, f)
			}
			if _, err := reader.ReadFrame(); err != io.EOF {
				t.Fatalf("end=%v", err)
			}
			if frames[1].ASDU[0] != 0xab || frames[3].ASDU[0] != 0xcd {
				t.Fatalf("buffer alias: %+v", frames)
			}
			var encoded []byte
			for _, f := range frames {
				b, err := f.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				encoded = append(encoded, b...)
			}
			if !bytes.Equal(encoded, stream) {
				t.Fatalf("stream=%x", encoded)
			}
		})
	}
	wire := unhex(t, "680500000000ab")
	for n := 0; n < len(wire); n++ {
		_, err := NewAPDUReader(bytes.NewReader(wire[:n])).ReadFrame()
		want := io.ErrUnexpectedEOF
		if n == 0 {
			want = io.EOF
		}
		if err != want {
			t.Fatalf("truncation %d: %v want %v", n, err, want)
		}
	}
	for _, prefix := range []string{"6704", "6803", "68fe"} {
		if _, err := NewAPDUReader(bytes.NewReader(unhex(t, prefix))).ReadFrame(); !errors.Is(err, ErrMalformed) {
			t.Fatalf("prefix=%s err=%v", prefix, err)
		}
	}
	injected := errors.New("reader failure")
	if _, err := NewAPDUReader(iotest.ErrReader(injected)).ReadFrame(); !errors.Is(err, injected) {
		t.Fatalf("lost IO error: %v", err)
	}
}
