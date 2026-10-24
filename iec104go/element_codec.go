// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"encoding/binary"
	"fmt"
	"math"
)

var errFieldRange = fmt.Errorf("%w: field out of range", ErrMalformed)

func timedType(v Element) TypeID {
	switch v.(type) {
	case SinglePoint:
		return M_SP_TB_1
	case DoublePoint:
		return M_DP_TB_1
	case StepPosition:
		return M_ST_TB_1
	case Bitstring32:
		return M_BO_TB_1
	case Normalized:
		return M_ME_TD_1
	case Scaled:
		return M_ME_TE_1
	case Float:
		return M_ME_TF_1
	case Counter:
		return M_IT_TB_1
	case ProtectionEvent:
		return M_EP_TD_1
	case ProtectionStart:
		return M_EP_TE_1
	case ProtectionOutput:
		return M_EP_TF_1
	case SingleCommand:
		return C_SC_TA_1
	case DoubleCommand:
		return C_DC_TA_1
	case RegulatingCommand:
		return C_RC_TA_1
	case SetpointNormalized:
		return C_SE_TA_1
	case SetpointScaled:
		return C_SE_TB_1
	case SetpointFloat:
		return C_SE_TC_1
	case BitstringCommand:
		return C_BO_TA_1
	}
	return 0
}

var timedBases = map[TypeID]TypeID{
	M_SP_TB_1: M_SP_NA_1, M_DP_TB_1: M_DP_NA_1, M_ST_TB_1: M_ST_NA_1,
	M_BO_TB_1: M_BO_NA_1, M_ME_TD_1: M_ME_NA_1, M_ME_TE_1: M_ME_NB_1,
	M_ME_TF_1: M_ME_NC_1, M_IT_TB_1: M_IT_NA_1,
	C_SC_TA_1: C_SC_NA_1, C_DC_TA_1: C_DC_NA_1, C_RC_TA_1: C_RC_NA_1,
	C_SE_TA_1: C_SE_NA_1, C_SE_TB_1: C_SE_NB_1, C_SE_TC_1: C_SE_NC_1,
	C_BO_TA_1: C_BO_NA_1,
}

func put16(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }
func put32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }

// encodeElement accepts only value forms, so even a typed nil pointer supplied
// through the exported interface becomes an error rather than a panic.
func encodeElement(v Element) (TypeID, []byte, error) {
	if t, ok := v.(Timed); ok {
		id := timedType(t.Value)
		if id == 0 {
			return 0, nil, fmt.Errorf("%w: unsupported timed element", ErrMalformed)
		}
		b, err := encodePayload(t.Value)
		if err != nil {
			return 0, nil, err
		}
		tag, err := t.Time.MarshalBinary()
		if err != nil {
			return 0, nil, err
		}
		return id, append(b, tag...), nil
	}
	b, err := encodePayload(v)
	if err != nil {
		return 0, nil, err
	}
	id := v.elementType()
	if !id.Supported() {
		return 0, nil, fmt.Errorf("%w: element needs time tag", ErrMalformed)
	}
	return id, b, nil
}

func encodePayload(v Element) ([]byte, error) {
	switch e := v.(type) {
	case SinglePoint:
		return []byte{byte(e.SIQ)}, nil
	case DoublePoint:
		return []byte{byte(e.DIQ)}, nil
	case StepPosition:
		return []byte{byte(e.Value), byte(e.Quality)}, nil
	case Bitstring32:
		return append(put32(e.Value), byte(e.Quality)), nil
	case Normalized:
		return append(put16(uint16(e.Value)), byte(e.Quality)), nil
	case Scaled:
		return append(put16(uint16(e.Value)), byte(e.Quality)), nil
	case Float:
		return append(put32(math.Float32bits(e.Value)), byte(e.Quality)), nil
	case Counter:
		return append(put32(uint32(e.Value)), e.Flags), nil
	case PackedStatus:
		return append(append(put16(e.Status), put16(e.Change)...), byte(e.Quality)), nil
	case NormalizedNoQuality:
		return put16(uint16(e.Value)), nil
	case ProtectionEvent:
		return append([]byte{e.SEP}, put16(uint16(e.Elapsed))...), nil
	case ProtectionStart:
		return append([]byte{e.SPE, e.QDP}, put16(uint16(e.Elapsed))...), nil
	case ProtectionOutput:
		return append([]byte{e.OCI, e.QDP}, put16(uint16(e.Elapsed))...), nil
	case SingleCommand:
		return []byte{byte(e.Qualifier)}, nil
	case DoubleCommand:
		return []byte{byte(e.Qualifier)}, nil
	case RegulatingCommand:
		return []byte{byte(e.Qualifier)}, nil
	case SetpointNormalized:
		return append(put16(uint16(e.Value)), byte(e.Qualifier)), nil
	case SetpointScaled:
		return append(put16(uint16(e.Value)), byte(e.Qualifier)), nil
	case SetpointFloat:
		return append(put32(math.Float32bits(e.Value)), byte(e.Qualifier)), nil
	case BitstringCommand:
		return put32(e.Value), nil
	case Initialization:
		return []byte{e.COI}, nil
	case Interrogation:
		return []byte{e.QOI}, nil
	case CounterInterrogation:
		return []byte{e.QCC}, nil
	case ReadCommand:
		return []byte{}, nil
	case ClockSync:
		return e.Time.MarshalBinary()
	case ResetProcess:
		return []byte{e.QRP}, nil
	case TestCommand:
		b, err := e.Time.MarshalBinary()
		if err != nil {
			return nil, err
		}
		return append(put16(e.Sequence), b...), nil
	case ParameterNormalized:
		return append(put16(uint16(e.Value)), e.QPM), nil
	case ParameterScaled:
		return append(put16(uint16(e.Value)), e.QPM), nil
	case ParameterFloat:
		return append(put32(math.Float32bits(e.Value)), e.QPM), nil
	case ParameterActivation:
		return []byte{e.QPA}, nil
	default:
		return encodeFileElement(v)
	}
}

func decodeElement(id TypeID, b []byte) (Element, error) {
	if isFileType(id) {
		return decodeFileElement(id, b)
	}
	n, ok := id.ElementSize()
	if !ok {
		return nil, ErrUnsupportedType
	}
	if len(b) != n {
		return nil, fmt.Errorf("%w: element length for %s", ErrMalformed, id)
	}
	if base, ok := timedBases[id]; ok {
		e, err := decodeElement(base, b[:n-7])
		if err != nil {
			return nil, err
		}
		tag, err := DecodeCP56Time2a(b[n-7:])
		if err != nil {
			return nil, err
		}
		return Timed{Value: e, Time: tag}, nil
	}
	switch id {
	case M_SP_NA_1:
		return SinglePoint{SIQ: SIQ(b[0])}, nil
	case M_DP_NA_1:
		return DoublePoint{DIQ: DIQ(b[0])}, nil
	case M_ST_NA_1:
		return StepPosition{Value: VTI(b[0]), Quality: Quality(b[1])}, nil
	case M_BO_NA_1:
		return Bitstring32{Value: binary.LittleEndian.Uint32(b), Quality: Quality(b[4])}, nil
	case M_ME_NA_1:
		return Normalized{Value: int16(binary.LittleEndian.Uint16(b)), Quality: Quality(b[2])}, nil
	case M_ME_NB_1:
		return Scaled{Value: int16(binary.LittleEndian.Uint16(b)), Quality: Quality(b[2])}, nil
	case M_ME_NC_1:
		return Float{Value: math.Float32frombits(binary.LittleEndian.Uint32(b)), Quality: Quality(b[4])}, nil
	case M_IT_NA_1:
		return Counter{Value: int32(binary.LittleEndian.Uint32(b)), Flags: b[4]}, nil
	case M_PS_NA_1:
		return PackedStatus{Status: binary.LittleEndian.Uint16(b), Change: binary.LittleEndian.Uint16(b[2:]), Quality: Quality(b[4])}, nil
	case M_ME_ND_1:
		return NormalizedNoQuality{Value: int16(binary.LittleEndian.Uint16(b))}, nil
	case M_EP_TD_1, M_EP_TE_1, M_EP_TF_1:
		tag, err := DecodeCP56Time2a(b[n-7:])
		if err != nil {
			return nil, err
		}
		elapsed, err := DecodeCP16Time2a(b[n-9 : n-7])
		if err != nil {
			return nil, err
		}
		var e Element
		switch id {
		case M_EP_TD_1:
			e = ProtectionEvent{SEP: b[0], Elapsed: elapsed}
		case M_EP_TE_1:
			e = ProtectionStart{SPE: b[0], QDP: b[1], Elapsed: elapsed}
		case M_EP_TF_1:
			e = ProtectionOutput{OCI: b[0], QDP: b[1], Elapsed: elapsed}
		}
		return Timed{Value: e, Time: tag}, nil
	case C_SC_NA_1:
		return SingleCommand{Qualifier: CommandQualifier(b[0])}, nil
	case C_DC_NA_1:
		return DoubleCommand{Qualifier: CommandQualifier(b[0])}, nil
	case C_RC_NA_1:
		return RegulatingCommand{Qualifier: CommandQualifier(b[0])}, nil
	case C_SE_NA_1:
		return SetpointNormalized{Value: int16(binary.LittleEndian.Uint16(b)), Qualifier: SetpointQualifier(b[2])}, nil
	case C_SE_NB_1:
		return SetpointScaled{Value: int16(binary.LittleEndian.Uint16(b)), Qualifier: SetpointQualifier(b[2])}, nil
	case C_SE_NC_1:
		return SetpointFloat{Value: math.Float32frombits(binary.LittleEndian.Uint32(b)), Qualifier: SetpointQualifier(b[4])}, nil
	case C_BO_NA_1:
		return BitstringCommand{Value: binary.LittleEndian.Uint32(b)}, nil
	case M_EI_NA_1:
		return Initialization{COI: b[0]}, nil
	case C_IC_NA_1:
		return Interrogation{QOI: b[0]}, nil
	case C_CI_NA_1:
		return CounterInterrogation{QCC: b[0]}, nil
	case C_RD_NA_1:
		return ReadCommand{}, nil
	case C_CS_NA_1:
		tag, err := DecodeCP56Time2a(b)
		if err != nil {
			return nil, err
		}
		return ClockSync{Time: tag}, nil
	case C_RP_NA_1:
		return ResetProcess{QRP: b[0]}, nil
	case C_TS_TA_1:
		tag, err := DecodeCP56Time2a(b[2:])
		if err != nil {
			return nil, err
		}
		return TestCommand{Sequence: binary.LittleEndian.Uint16(b), Time: tag}, nil
	case P_ME_NA_1:
		return ParameterNormalized{Value: int16(binary.LittleEndian.Uint16(b)), QPM: b[2]}, nil
	case P_ME_NB_1:
		return ParameterScaled{Value: int16(binary.LittleEndian.Uint16(b)), QPM: b[2]}, nil
	case P_ME_NC_1:
		return ParameterFloat{Value: math.Float32frombits(binary.LittleEndian.Uint32(b)), QPM: b[4]}, nil
	case P_AC_NA_1:
		return ParameterActivation{QPA: b[0]}, nil
	}
	return nil, ErrUnsupportedType
}
