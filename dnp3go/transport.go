package dnp3go

// DNP3 transport layer: reassembles application-layer fragments from
// transport segments carried in data link frames.

const (
	tpFIN = 0x80 // Final segment
	tpFIR = 0x40 // First segment
	tpSEQ = 0x3F // Sequence number mask
)

// TransportReader reassembles multi-segment transport messages.
type TransportReader struct {
	buf       []byte
	expectSeq uint8
	active    bool
}

// ProcessSegment processes one transport segment and returns the complete
// application-layer message when reassembly is done. Returns nil if more
// segments are needed.
func (t *TransportReader) ProcessSegment(data []byte) []byte {
	if len(data) < 1 {
		return nil
	}

	header := data[0]
	payload := data[1:]
	fir := header&tpFIR != 0
	fin := header&tpFIN != 0
	seq := header & tpSEQ

	if fir {
		// Start of new message
		t.buf = make([]byte, 0, len(payload)*4)
		t.buf = append(t.buf, payload...)
		t.expectSeq = (seq + 1) & tpSEQ
		t.active = true
	} else if t.active && seq == t.expectSeq {
		// Continuation
		t.buf = append(t.buf, payload...)
		t.expectSeq = (seq + 1) & tpSEQ
	} else {
		// Out of sequence — reset
		t.buf = nil
		t.active = false
		return nil
	}

	if fin {
		result := t.buf
		t.buf = nil
		t.active = false
		return result
	}

	return nil
}

// WrapTransport wraps an application-layer message in transport segment(s).
// For simplicity and typical ICS message sizes, this produces a single
// segment with FIR=1, FIN=1.
func WrapTransport(appData []byte, seq uint8) []byte {
	header := tpFIR | tpFIN | (seq & tpSEQ)
	out := make([]byte, 1+len(appData))
	out[0] = header
	copy(out[1:], appData)
	return out
}
