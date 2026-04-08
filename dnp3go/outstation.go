package dnp3go

import (
	"log"
	"net"
	"sync"
	"time"
)

// BinaryPoint represents a read-only binary input point.
type BinaryPoint struct {
	Index uint16
	Read  func() bool
}

// BinaryOutputPoint represents a controllable binary output point.
type BinaryOutputPoint struct {
	Index   uint16
	Read    func() bool
	Operate func(controlCode uint8) uint8 // returns CROB status code
}

// AnalogPoint represents a read-only analog input point.
type AnalogPoint struct {
	Index uint16
	Read  func() float32
}

// AnalogOutputPoint represents a controllable analog output point.
type AnalogOutputPoint struct {
	Index   uint16
	Read    func() float32
	Operate func(value float32) uint8 // returns status code (0=success)
}

// OutstationConfig configures a DNP3 outstation.
type OutstationConfig struct {
	// OutstationAddr is the DNP3 address of this outstation (e.g., 1 for relay).
	OutstationAddr uint16

	// AcceptAnyMaster allows connections from any master address when true.
	// When false, only MasterAddr is accepted.
	AcceptAnyMaster bool
	MasterAddr      uint16

	// Point definitions
	BinaryInputs  []BinaryPoint
	BinaryOutputs []BinaryOutputPoint
	AnalogInputs  []AnalogPoint
	AnalogOutputs []AnalogOutputPoint

	// Logger for protocol events. If nil, uses log.Printf.
	Logger func(format string, args ...any)
}

func (c *OutstationConfig) logf(format string, args ...any) {
	if c.Logger != nil {
		c.Logger(format, args...)
	} else {
		log.Printf("DNP3 [%d] "+format, append([]any{c.OutstationAddr}, args...)...)
	}
}

// sboEntry tracks a pending Select-Before-Operate.
type sboEntry struct {
	group       uint8
	index       uint16
	controlCode uint8
	value       float32
	expires     time.Time
}

// connState holds per-connection state.
type connState struct {
	mu       sync.Mutex
	pending  *sboEntry
	txSeq    uint8 // transport sequence counter
}

// ListenAndServe starts a DNP3 TCP outstation on the given address (e.g., ":20000").
// It blocks forever, accepting connections.
func ListenAndServe(addr string, cfg *OutstationConfig) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	cfg.logf("listening on %s", addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			cfg.logf("accept error: %v", err)
			continue
		}
		go handleConn(conn, cfg)
	}
}

func handleConn(conn net.Conn, cfg *OutstationConfig) {
	defer conn.Close()
	cfg.logf("connection from %s", conn.RemoteAddr())

	tr := &TransportReader{}
	cs := &connState{}

	for {
		frame, err := ReadLinkFrame(conn)
		if err != nil {
			return // connection closed or protocol error
		}

		// Validate destination address
		if frame.Dest != cfg.OutstationAddr && frame.Dest != 0xFFFF { // 0xFFFF = broadcast
			continue
		}

		// Validate source (master) address
		if !cfg.AcceptAnyMaster && frame.Source != cfg.MasterAddr {
			continue
		}

		// Reassemble transport
		appData := tr.ProcessSegment(frame.Payload)
		if appData == nil {
			continue
		}

		// Parse application layer
		apdu, err := ParseAPDU(appData)
		if err != nil {
			continue
		}

		// Dispatch by function code
		var respObjects []byte
		var iin uint16

		switch apdu.FuncCode {
		case FCRead:
			respObjects, iin = handleRead(cfg, apdu)
		case FCDirectOperate:
			respObjects, iin = handleDirectOperate(cfg, apdu)
		case FCSelect:
			respObjects, iin = handleSelect(cfg, cs, apdu)
		case FCOperate:
			respObjects, iin = handleOperate(cfg, cs, apdu)
		case FCWrite:
			// Accept but do nothing — return empty response
			iin = 0
		default:
			iin = uint16(IIN1NoFuncCode) << 8
		}

		// Build and send response
		reqSeq := apdu.Control & acSEQ
		respAPDU := BuildResponse(reqSeq, iin, respObjects)

		cs.mu.Lock()
		tpData := WrapTransport(respAPDU, cs.txSeq)
		cs.txSeq = (cs.txSeq + 1) & tpSEQ
		cs.mu.Unlock()

		respFrame := &LinkFrame{
			Control: MakeResponseControl(),
			Dest:    frame.Source,
			Source:  cfg.OutstationAddr,
			Payload: tpData,
		}

		if err := WriteLinkFrame(conn, respFrame); err != nil {
			return
		}
	}
}

// ── Function Code Handlers ───────────────────────────────────────

func handleRead(cfg *OutstationConfig, apdu *APDU) ([]byte, uint16) {
	var out []byte

	for _, oh := range apdu.Objects {
		switch {
		case oh.Group == GroupClassData && oh.Variation >= VarClass0 && oh.Variation <= VarClass3:
			// Class 0 = all static data; class 1-3 = events (return empty)
			if oh.Variation == VarClass0 || oh.Variation == VarClass1 {
				out = append(out, encodeAllStatic(cfg)...)
			}
			// Class 1-3 events: no event buffering, return nothing extra

		case oh.Group == GroupBinaryInput:
			out = append(out, readBinaryInputs(cfg, oh)...)
		case oh.Group == GroupBinaryOutput:
			out = append(out, readBinaryOutputs(cfg, oh)...)
		case oh.Group == GroupAnalogInput:
			out = append(out, readAnalogInputs(cfg, oh)...)
		case oh.Group == GroupAnalogOutput:
			out = append(out, readAnalogOutputs(cfg, oh)...)
		default:
			return nil, uint16(IIN2ObjectUnknown)
		}
	}

	return out, 0
}

func handleDirectOperate(cfg *OutstationConfig, apdu *APDU) ([]byte, uint16) {
	var out []byte

	for _, oh := range apdu.Objects {
		switch {
		case oh.Group == GroupCROB && oh.Variation == VarCROB:
			for _, po := range oh.PrefixedData {
				cc, ok := ParseCROB(po.Data)
				if !ok {
					continue
				}
				status := operateBinaryOutput(cfg, po.Index, cc)
				out = append(out, EncodeCROBResponse(po.Index, cc, status)...)
			}

		case oh.Group == GroupAnalogOutputCmd && oh.Variation == VarAOCmdFloat:
			for _, po := range oh.PrefixedData {
				val, ok := ParseAnalogOutputCmd(po.Data)
				if !ok {
					continue
				}
				status := operateAnalogOutput(cfg, po.Index, val)
				out = append(out, EncodeAnalogOutputResponse(po.Index, val, status)...)
			}

		default:
			return nil, uint16(IIN2ObjectUnknown)
		}
	}

	return out, 0
}

func handleSelect(cfg *OutstationConfig, cs *connState, apdu *APDU) ([]byte, uint16) {
	var out []byte

	for _, oh := range apdu.Objects {
		switch {
		case oh.Group == GroupCROB && oh.Variation == VarCROB:
			for _, po := range oh.PrefixedData {
				cc, ok := ParseCROB(po.Data)
				if !ok {
					continue
				}
				// Validate the point exists
				found := false
				for _, bp := range cfg.BinaryOutputs {
					if bp.Index == po.Index {
						found = true
						break
					}
				}
				status := uint8(CROBStatusSuccess)
				if !found {
					status = CROBStatusNotSupported
				} else {
					cs.mu.Lock()
					cs.pending = &sboEntry{
						group:       GroupCROB,
						index:       po.Index,
						controlCode: cc,
						expires:     time.Now().Add(5 * time.Second),
					}
					cs.mu.Unlock()
				}
				out = append(out, EncodeCROBResponse(po.Index, cc, status)...)
			}

		case oh.Group == GroupAnalogOutputCmd && oh.Variation == VarAOCmdFloat:
			for _, po := range oh.PrefixedData {
				val, ok := ParseAnalogOutputCmd(po.Data)
				if !ok {
					continue
				}
				found := false
				for _, ap := range cfg.AnalogOutputs {
					if ap.Index == po.Index {
						found = true
						break
					}
				}
				status := uint8(0)
				if !found {
					status = CROBStatusNotSupported
				} else {
					cs.mu.Lock()
					cs.pending = &sboEntry{
						group:   GroupAnalogOutputCmd,
						index:   po.Index,
						value:   val,
						expires: time.Now().Add(5 * time.Second),
					}
					cs.mu.Unlock()
				}
				out = append(out, EncodeAnalogOutputResponse(po.Index, val, status)...)
			}
		}
	}

	return out, 0
}

func handleOperate(cfg *OutstationConfig, cs *connState, apdu *APDU) ([]byte, uint16) {
	var out []byte

	for _, oh := range apdu.Objects {
		switch {
		case oh.Group == GroupCROB && oh.Variation == VarCROB:
			for _, po := range oh.PrefixedData {
				cc, ok := ParseCROB(po.Data)
				if !ok {
					continue
				}
				cs.mu.Lock()
				p := cs.pending
				cs.pending = nil
				cs.mu.Unlock()

				status := uint8(CROBStatusNoSelect)
				if p != nil && p.group == GroupCROB && p.index == po.Index &&
					p.controlCode == cc && time.Now().Before(p.expires) {
					status = operateBinaryOutput(cfg, po.Index, cc)
				}
				out = append(out, EncodeCROBResponse(po.Index, cc, status)...)
			}

		case oh.Group == GroupAnalogOutputCmd && oh.Variation == VarAOCmdFloat:
			for _, po := range oh.PrefixedData {
				val, ok := ParseAnalogOutputCmd(po.Data)
				if !ok {
					continue
				}
				cs.mu.Lock()
				p := cs.pending
				cs.pending = nil
				cs.mu.Unlock()

				status := uint8(CROBStatusNoSelect)
				if p != nil && p.group == GroupAnalogOutputCmd && p.index == po.Index &&
					time.Now().Before(p.expires) {
					status = operateAnalogOutput(cfg, po.Index, val)
				}
				out = append(out, EncodeAnalogOutputResponse(po.Index, val, status)...)
			}
		}
	}

	return out, 0
}

// ── Point Access Helpers ─────────────────────────────────────────

func encodeAllStatic(cfg *OutstationConfig) []byte {
	var out []byte

	// Binary inputs
	if len(cfg.BinaryInputs) > 0 {
		vals := make([]bool, len(cfg.BinaryInputs))
		for i, p := range cfg.BinaryInputs {
			vals[i] = p.Read()
		}
		out = append(out, EncodeBinaryInputs(cfg.BinaryInputs[0].Index, vals)...)
	}

	// Binary output status
	if len(cfg.BinaryOutputs) > 0 {
		vals := make([]bool, len(cfg.BinaryOutputs))
		for i, p := range cfg.BinaryOutputs {
			vals[i] = p.Read()
		}
		out = append(out, EncodeBinaryOutputStatus(cfg.BinaryOutputs[0].Index, vals)...)
	}

	// Analog inputs
	if len(cfg.AnalogInputs) > 0 {
		vals := make([]float32, len(cfg.AnalogInputs))
		for i, p := range cfg.AnalogInputs {
			vals[i] = p.Read()
		}
		out = append(out, EncodeAnalogInputs(cfg.AnalogInputs[0].Index, vals)...)
	}

	// Analog output status
	if len(cfg.AnalogOutputs) > 0 {
		vals := make([]float32, len(cfg.AnalogOutputs))
		for i, p := range cfg.AnalogOutputs {
			vals[i] = p.Read()
		}
		out = append(out, EncodeAnalogOutputStatus(cfg.AnalogOutputs[0].Index, vals)...)
	}

	return out
}

func readBinaryInputs(cfg *OutstationConfig, oh ObjectHeader) []byte {
	start, stop := rangeForQualifier(oh, len(cfg.BinaryInputs))
	if start > stop {
		return nil
	}
	var vals []bool
	for _, p := range cfg.BinaryInputs {
		if p.Index >= start && p.Index <= stop {
			vals = append(vals, p.Read())
		}
	}
	return EncodeBinaryInputs(start, vals)
}

func readBinaryOutputs(cfg *OutstationConfig, oh ObjectHeader) []byte {
	start, stop := rangeForQualifier(oh, len(cfg.BinaryOutputs))
	if start > stop {
		return nil
	}
	var vals []bool
	for _, p := range cfg.BinaryOutputs {
		if p.Index >= start && p.Index <= stop {
			vals = append(vals, p.Read())
		}
	}
	return EncodeBinaryOutputStatus(start, vals)
}

func readAnalogInputs(cfg *OutstationConfig, oh ObjectHeader) []byte {
	start, stop := rangeForQualifier(oh, len(cfg.AnalogInputs))
	if start > stop {
		return nil
	}
	var vals []float32
	for _, p := range cfg.AnalogInputs {
		if p.Index >= start && p.Index <= stop {
			vals = append(vals, p.Read())
		}
	}
	return EncodeAnalogInputs(start, vals)
}

func readAnalogOutputs(cfg *OutstationConfig, oh ObjectHeader) []byte {
	start, stop := rangeForQualifier(oh, len(cfg.AnalogOutputs))
	if start > stop {
		return nil
	}
	var vals []float32
	for _, p := range cfg.AnalogOutputs {
		if p.Index >= start && p.Index <= stop {
			vals = append(vals, p.Read())
		}
	}
	return EncodeAnalogOutputStatus(start, vals)
}

func rangeForQualifier(oh ObjectHeader, maxPoints int) (uint16, uint16) {
	switch oh.Qualifier {
	case QualAllPoints:
		if maxPoints == 0 {
			return 1, 0 // empty
		}
		return 0, uint16(maxPoints - 1)
	case QualStartStop8, QualStartStop16:
		return oh.Start, oh.Stop
	default:
		return 0, uint16(maxPoints - 1)
	}
}

func operateBinaryOutput(cfg *OutstationConfig, index uint16, controlCode uint8) uint8 {
	for _, bp := range cfg.BinaryOutputs {
		if bp.Index == index {
			return bp.Operate(controlCode)
		}
	}
	return CROBStatusNotSupported
}

func operateAnalogOutput(cfg *OutstationConfig, index uint16, value float32) uint8 {
	for _, ap := range cfg.AnalogOutputs {
		if ap.Index == index {
			return ap.Operate(value)
		}
	}
	return CROBStatusNotSupported
}
