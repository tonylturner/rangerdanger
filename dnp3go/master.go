package dnp3go

import (
	"errors"
	"fmt"
	"math"
	"net"
	"time"
)

// WithMasterConnection dials an outstation, applies a deadline, and runs fn
// on the connection. It closes normally when fn succeeds and aborts the TCP
// connection when setup or fn fails, discarding any queued control data.
func WithMasterConnection(endpoint string, timeout time.Duration, fn func(net.Conn) error) error {
	conn, err := net.DialTimeout("tcp", endpoint, timeout)
	if err != nil {
		return err
	}

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return abortMasterConnection(conn, fmt.Errorf("set deadline: %w", err))
	}
	if err := fn(conn); err != nil {
		return abortMasterConnection(conn, err)
	}
	if err := conn.Close(); err != nil {
		return abortMasterConnection(conn, fmt.Errorf("close connection: %w", err))
	}
	return nil
}

func abortMasterConnection(conn net.Conn, cause error) error {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return errors.Join(cause, fmt.Errorf("abortive close requires a TCP connection"), conn.Close())
	}

	if err := tcpConn.SetLinger(0); err != nil {
		cause = errors.Join(cause, fmt.Errorf("set abortive close: %w", err))
	}
	if err := tcpConn.Close(); err != nil {
		cause = errors.Join(cause, fmt.Errorf("abort connection: %w", err))
	}
	return cause
}

// MasterConfig configures a DNP3 master that polls an outstation.
type MasterConfig struct {
	// MasterAddr is the DNP3 address of this master station.
	MasterAddr uint16

	// OutstationAddr is the DNP3 address of the target outstation.
	OutstationAddr uint16

	// Endpoint is the TCP address of the outstation (e.g., "10.40.40.20:20000").
	Endpoint string

	// PollInterval is how often to send an integrity poll.
	PollInterval time.Duration

	// Timeout for TCP connection and read operations.
	Timeout time.Duration
}

// PollResult contains the parsed response from an integrity poll.
type PollResult struct {
	BinaryInputs  map[uint16]bool
	BinaryOutputs map[uint16]bool
	AnalogInputs  map[uint16]float32
	AnalogOutputs map[uint16]float32
	IIN           uint16
	Error         error
}

// Poll sends a single Class 0 integrity poll to the outstation and returns
// the parsed point values. This is a one-shot operation: connect, send, receive, close.
func Poll(cfg *MasterConfig) *PollResult {
	result := &PollResult{
		BinaryInputs:  make(map[uint16]bool),
		BinaryOutputs: make(map[uint16]bool),
		AnalogInputs:  make(map[uint16]float32),
		AnalogOutputs: make(map[uint16]float32),
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}

	started := false
	err := WithMasterConnection(cfg.Endpoint, timeout, func(conn net.Conn) error {
		started = true

		// Build integrity poll: Read FC01, Group 60 Var 1 (Class 0), Qualifier 0x06 (all)
		apdu := []byte{
			acFIR | acFIN,                            // AC: FIR=1, FIN=1, SEQ=0
			FCRead,                                   // FC 0x01: Read
			GroupClassData, VarClass0, QualAllPoints, // Group 60 Var 1 Qual 0x06
		}

		tpData := WrapTransport(apdu, 0)

		reqFrame := &LinkFrame{
			Control: ctrlDIR | ctrlPRM | ctrlFCUnconfirmedData, // 0xC4
			Dest:    cfg.OutstationAddr,
			Source:  cfg.MasterAddr,
			Payload: tpData,
		}

		if err := WriteLinkFrame(conn, reqFrame); err != nil {
			return fmt.Errorf("write: %w", err)
		}

		// Read response
		respFrame, err := ReadLinkFrame(conn)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		// Reassemble transport
		tr := &TransportReader{}
		appData := tr.ProcessSegment(respFrame.Payload)
		if appData == nil {
			return fmt.Errorf("incomplete transport segment")
		}

		// Parse APDU
		respAPDU, err := ParseAPDU(appData)
		if err != nil {
			return fmt.Errorf("parse APDU: %w", err)
		}

		result.IIN = respAPDU.IIN

		// Extract point values from response objects
		for _, oh := range respAPDU.Objects {
			switch {
			case oh.Group == GroupBinaryInput && oh.Variation == VarBIStatus:
				// Data is inline after the header — parse flags bytes.
				parseInlineBinary(oh, result.BinaryInputs, oh.Start, oh.Stop)

			case oh.Group == GroupBinaryOutput && oh.Variation == VarBOStatus:
				parseInlineBinary(oh, result.BinaryOutputs, oh.Start, oh.Stop)

			case oh.Group == GroupAnalogInput && oh.Variation == VarAIFloat:
				parseInlineAnalog(oh, result.AnalogInputs, oh.Start, oh.Stop)

			case oh.Group == GroupAnalogOutput && oh.Variation == VarAOFloat:
				parseInlineAnalog(oh, result.AnalogOutputs, oh.Start, oh.Stop)
			}
		}

		return nil
	})
	if err != nil {
		if !started {
			result.Error = fmt.Errorf("connect: %w", err)
		} else {
			result.Error = err
		}
	}
	return result
}

// parseInlineBinary extracts boolean values from a response object's inline data.
func parseInlineBinary(oh ObjectHeader, out map[uint16]bool, start, stop uint16) {
	if len(oh.Data) == 0 {
		return
	}
	for i := start; i <= stop; i++ {
		offset := int(i - start)
		if offset >= len(oh.Data) {
			break
		}
		out[i] = oh.Data[offset]&0x80 != 0
	}
}

// parseInlineAnalog extracts float32 values from a response object's inline data.
func parseInlineAnalog(oh ObjectHeader, out map[uint16]float32, start, stop uint16) {
	if len(oh.Data) == 0 {
		return
	}
	offset := 0
	for i := start; i <= stop; i++ {
		if offset+5 > len(oh.Data) {
			break
		}
		// flags(1) + float32_LE(4)
		offset++ // skip flags
		if offset+4 > len(oh.Data) {
			break
		}
		bits := uint32(oh.Data[offset]) | uint32(oh.Data[offset+1])<<8 |
			uint32(oh.Data[offset+2])<<16 | uint32(oh.Data[offset+3])<<24
		out[i] = float32frombits(bits)
		offset += 4
	}
}

func float32frombits(b uint32) float32 {
	return math.Float32frombits(b)
}
