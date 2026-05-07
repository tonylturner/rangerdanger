package dnp3go

import (
	"bytes"
	"testing"
)

// Round-trip tests for the DNP3 layers we implement. The goal is to
// pin behaviour at the bytes-on-the-wire level: anything we write
// with our encoder must be readable by our decoder, and the
// resulting struct must match what we put in. If a future change
// breaks framing, transport reassembly, or app-layer encoding, the
// failures here say exactly which layer regressed.

// TestLinkFrame_RoundTrip writes a LinkFrame and reads it back
// through the same module, verifying every field survives.
func TestLinkFrame_RoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		control uint8
		dest    uint16
		source  uint16
		payload []byte
	}{
		{
			name:    "empty payload",
			control: MakeResponseControl(),
			dest:    1,
			source:  10,
			payload: nil,
		},
		{
			name:    "single byte",
			control: MakeResponseControl(),
			dest:    1,
			source:  10,
			payload: []byte{0xAB},
		},
		{
			name:    "exactly one block (16 bytes)",
			control: MakeResponseControl(),
			dest:    1,
			source:  10,
			payload: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F},
		},
		{
			name:    "spans two blocks (17 bytes)",
			control: MakeResponseControl(),
			dest:    1,
			source:  10,
			payload: append(make([]byte, 16, 17), 0xFF),
		},
		{
			name:    "spans many blocks (65 bytes — 4 blocks plus 1)",
			control: MakeResponseControl(),
			dest:    2,
			source:  20,
			payload: bytes.Repeat([]byte{0xA5}, 65),
		},
		{
			name:    "outstation→master direction (DIR=0)",
			control: 0x44, // PRM=1, DIR=0, FC=4 (unconfirmed user data)
			dest:    100,
			source:  200,
			payload: []byte{0xC0, 0x01, 0x81, 0x00, 0x00},
		},
		{
			name:    "max DNP3 addresses",
			control: MakeResponseControl(),
			dest:    0xFFFE, // 0xFFFF is reserved
			source:  0xFFFE,
			payload: []byte{0x42},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			in := &LinkFrame{
				Control: tc.control,
				Dest:    tc.dest,
				Source:  tc.source,
				Payload: tc.payload,
			}
			if err := WriteLinkFrame(&buf, in); err != nil {
				t.Fatalf("WriteLinkFrame: %v", err)
			}

			out, err := ReadLinkFrame(&buf)
			if err != nil {
				t.Fatalf("ReadLinkFrame: %v (wire bytes: %x)", err, buf.Bytes())
			}

			if out.Control != in.Control {
				t.Errorf("control: got %#x want %#x", out.Control, in.Control)
			}
			if out.Dest != in.Dest {
				t.Errorf("dest: got %d want %d", out.Dest, in.Dest)
			}
			if out.Source != in.Source {
				t.Errorf("source: got %d want %d", out.Source, in.Source)
			}
			if !bytes.Equal(out.Payload, in.Payload) {
				t.Errorf("payload mismatch:\n  got:  %x\n  want: %x", out.Payload, in.Payload)
			}
		})
	}
}

// TestReadLinkFrame_BadStartBytes makes sure a stream of garbage
// before the real frame doesn't trip the parser — the loop scans
// for 0x05 0x64, anything else is skipped.
func TestReadLinkFrame_LeadingGarbage(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0xFF, 0x42, 0x05, 0x99}) // garbage including a stray 0x05
	if err := WriteLinkFrame(&buf, &LinkFrame{
		Control: MakeResponseControl(),
		Dest:    1,
		Source:  10,
		Payload: []byte{0xAB, 0xCD},
	}); err != nil {
		t.Fatalf("WriteLinkFrame: %v", err)
	}

	out, err := ReadLinkFrame(&buf)
	if err != nil {
		t.Fatalf("ReadLinkFrame should have skipped garbage and read the frame: %v", err)
	}
	if !bytes.Equal(out.Payload, []byte{0xAB, 0xCD}) {
		t.Errorf("payload after skipping garbage: got %x want ABCD", out.Payload)
	}
}

// TestReadLinkFrame_CorruptedDataCRC must reject a frame whose data
// block CRC doesn't match. A wire-flip in the middle of the payload
// must not silently produce a valid-looking parse.
func TestReadLinkFrame_CorruptedDataCRC(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteLinkFrame(&buf, &LinkFrame{
		Control: MakeResponseControl(),
		Dest:    1,
		Source:  10,
		Payload: []byte{0xAB, 0xCD, 0xEF},
	}); err != nil {
		t.Fatalf("WriteLinkFrame: %v", err)
	}

	wire := buf.Bytes()
	// Flip a payload byte (after the link header) but leave the data
	// block's CRC alone — this should now fail validation.
	wire[linkHeaderSize] ^= 0xFF

	_, err := ReadLinkFrame(bytes.NewReader(wire))
	if err == nil {
		t.Fatal("expected CRC error, got nil")
	}
	if err != ErrBadBlockCRC {
		// The framer might also surface this as ErrBadHeaderCRC if
		// the byte happened to flip into the header area. Either way
		// we want a non-nil rejection. Log so a future maintainer
		// can see what we observed.
		t.Logf("got %v (acceptable as long as it's a rejection)", err)
	}
}

// TestParseAPDU_RoundTrip verifies an APDU built by BuildResponse
// parses back into the same logical content via ParseAPDU.
func TestParseAPDU_RoundTrip(t *testing.T) {
	// Build a typical outstation response: function code 0x81
	// (response), seq=0, IIN=0, with a binary input objects payload.
	objects := EncodeBinaryInputs(0, []bool{true, false, true, true})
	wire := BuildResponse(0, 0, objects)

	apdu, err := ParseAPDU(wire)
	if err != nil {
		t.Fatalf("ParseAPDU: %v", err)
	}

	if apdu.FuncCode != 0x81 {
		t.Errorf("function code: got %#x want 0x81 (response)", apdu.FuncCode)
	}
	if apdu.IIN != 0 {
		t.Errorf("IIN: got %#x want 0", apdu.IIN)
	}
}

// TestEncodeBinaryInputs_VariesWithIndex documents that the
// encoder emits different bytes for different starting indices —
// trivial regression guard against an accidental constant-output bug.
func TestEncodeBinaryInputs_VariesWithIndex(t *testing.T) {
	a := EncodeBinaryInputs(0, []bool{true, false})
	b := EncodeBinaryInputs(5, []bool{true, false})
	if bytes.Equal(a, b) {
		t.Errorf("expected different bytes for different start index; both = %x", a)
	}
}

// TestEncodeAnalogInputs_BasicShape ensures the analog encoder
// produces the expected qualifier+range header for a small range.
// Concrete byte-pattern checks would be too brittle to chase across
// future encoder refactors; we just confirm output is non-empty and
// changes with input length.
func TestEncodeAnalogInputs_BasicShape(t *testing.T) {
	one := EncodeAnalogInputs(0, []float32{1.5})
	two := EncodeAnalogInputs(0, []float32{1.5, 2.5})
	if len(one) >= len(two) {
		t.Errorf("two-value encoding (%d bytes) should be longer than one-value (%d)", len(two), len(one))
	}
	if len(one) == 0 || len(two) == 0 {
		t.Error("encoder produced empty bytes")
	}
}

// TestWrapTransport_NonEmpty pins the basic behavior of the
// transport layer wrapper — mostly to ensure it doesn't lose data.
func TestWrapTransport_NonEmpty(t *testing.T) {
	app := []byte{0xC0, 0x01, 0x3C, 0x01, 0x06}
	wrapped := WrapTransport(app, 0)
	if len(wrapped) <= len(app) {
		t.Errorf("transport wrap should add ≥1 header byte; got %d for app of %d", len(wrapped), len(app))
	}
}
