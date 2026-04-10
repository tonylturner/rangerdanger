package dnp3go

import (
	"encoding/binary"
	"math"
)

// DNP3 application layer function codes.
const (
	FCRead          = 0x01
	FCWrite         = 0x02
	FCSelect        = 0x03
	FCOperate       = 0x04
	FCDirectOperate = 0x05
	FCResponse      = 0x81
)

// Application control byte bits.
const (
	acFIR = 0x80
	acFIN = 0x40
	acCON = 0x20
	acUNS = 0x10
	acSEQ = 0x0F
)

// Object group/variation constants.
const (
	// Binary Input with status flags
	GroupBinaryInput = 1
	VarBIStatus      = 2 // 1 byte: flags(7) + value(0)

	// Binary Output Status
	GroupBinaryOutput = 10
	VarBOStatus       = 2 // 1 byte: flags(7) + value(0)

	// CROB (Control Relay Output Block)
	GroupCROB = 12
	VarCROB   = 1 // 11 bytes: control(1) + count(1) + onTime(4) + offTime(4) + status(1)

	// Analog Input (float32)
	GroupAnalogInput = 30
	VarAIFloat       = 5 // 5 bytes: flags(1) + float32_LE(4)

	// Analog Output Status (float32)
	GroupAnalogOutput = 40
	VarAOFloat        = 3 // 5 bytes: flags(1) + float32_LE(4)

	// Analog Output Command (float32)
	GroupAnalogOutputCmd = 41
	VarAOCmdFloat        = 3 // 5 bytes: status(1) + float32_LE(4)

	// Class data objects
	GroupClassData = 60
	VarClass0      = 1
	VarClass1      = 2
	VarClass2      = 3
	VarClass3      = 4
)

// Qualifier codes.
const (
	QualStartStop8  = 0x00 // 1-byte start, 1-byte stop
	QualStartStop16 = 0x01 // 2-byte start, 2-byte stop
	QualAllPoints   = 0x06 // no range — all points
	QualCount8Pfx8  = 0x17 // 1-byte count, 1-byte prefix index
	QualCount16Pfx16 = 0x28 // 2-byte count, 2-byte prefix index
)

// CROB control codes (Trip/Close/Latch).
const (
	CROBLatchOn    = 0x03
	CROBLatchOff   = 0x04
	CROBPulseOn    = 0x01
	CROBPulseOff   = 0x02
	CROBClosePulse = 0x41 // paired close
	CROBTripPulse  = 0x81 // paired trip
)

// CROB status codes.
const (
	CROBStatusSuccess           = 0x00
	CROBStatusTimeout           = 0x01
	CROBStatusNoSelect          = 0x02
	CROBStatusNotSupported      = 0x04
	CROBStatusAlreadyActive     = 0x05
	CROBStatusHardwareError     = 0x06
	CROBStatusBlocked           = 0x07 // used when remote control disabled / lockout
)

// IIN (Internal Indications) bit masks.
const (
	IIN1DeviceRestart = 0x80
	IIN1NoFuncCode    = 0x01
	IIN2ObjectUnknown = 0x02
	IIN2ParamError    = 0x04
)

// ObjectHeader represents a parsed DNP3 object header with its data.
type ObjectHeader struct {
	Group     uint8
	Variation uint8
	Qualifier uint8
	Start     uint16
	Stop      uint16
	Count     uint16
	PrefixedData []PrefixedObject // for qualifier 0x17/0x28
	Data      []byte
}

// PrefixedObject is a single object with its point index prefix.
type PrefixedObject struct {
	Index uint16
	Data  []byte
}

// APDU represents a parsed DNP3 application protocol data unit.
type APDU struct {
	Control  uint8
	FuncCode uint8
	IIN      uint16 // response only (IIN1 in high byte, IIN2 in low byte)
	Objects  []ObjectHeader
}

// ParseAPDU parses an application-layer message from raw bytes.
func ParseAPDU(data []byte) (*APDU, error) {
	if len(data) < 2 {
		return nil, ErrFrameTooShort
	}

	apdu := &APDU{
		Control:  data[0],
		FuncCode: data[1],
	}

	offset := 2

	// If this is a response, next 2 bytes are IIN
	if apdu.FuncCode >= 0x81 {
		if len(data) < 4 {
			return apdu, nil
		}
		apdu.IIN = uint16(data[2])<<8 | uint16(data[3])
		offset = 4
	}

	// Parse object headers
	for offset < len(data) {
		if offset+3 > len(data) {
			break
		}

		oh := ObjectHeader{
			Group:     data[offset],
			Variation: data[offset+1],
			Qualifier: data[offset+2],
		}
		offset += 3

		switch oh.Qualifier {
		case QualAllPoints:
			// No range bytes — request for all points
		case QualStartStop8:
			if offset+2 > len(data) {
				break
			}
			oh.Start = uint16(data[offset])
			oh.Stop = uint16(data[offset+1])
			offset += 2
			// Consume inline object data for response messages
			count := int(oh.Stop-oh.Start) + 1
			objSize := objectDataSize(oh.Group, oh.Variation)
			totalBytes := count * objSize
			if objSize > 0 && offset+totalBytes <= len(data) {
				oh.Data = make([]byte, totalBytes)
				copy(oh.Data, data[offset:offset+totalBytes])
				offset += totalBytes
			}
		case QualStartStop16:
			if offset+4 > len(data) {
				break
			}
			oh.Start = binary.LittleEndian.Uint16(data[offset:])
			oh.Stop = binary.LittleEndian.Uint16(data[offset+2:])
			offset += 4
			// Consume inline object data for response messages
			count16 := int(oh.Stop-oh.Start) + 1
			objSize16 := objectDataSize(oh.Group, oh.Variation)
			totalBytes16 := count16 * objSize16
			if objSize16 > 0 && offset+totalBytes16 <= len(data) {
				oh.Data = make([]byte, totalBytes16)
				copy(oh.Data, data[offset:offset+totalBytes16])
				offset += totalBytes16
			}
		case QualCount8Pfx8:
			if offset+1 > len(data) {
				break
			}
			count := int(data[offset])
			offset++
			oh.Count = uint16(count)
			objSize := objectDataSize(oh.Group, oh.Variation)
			for i := 0; i < count && offset+1+objSize <= len(data); i++ {
				idx := uint16(data[offset])
				offset++
				objData := make([]byte, objSize)
				copy(objData, data[offset:offset+objSize])
				offset += objSize
				oh.PrefixedData = append(oh.PrefixedData, PrefixedObject{Index: idx, Data: objData})
			}
		case QualCount16Pfx16:
			if offset+2 > len(data) {
				break
			}
			count := int(binary.LittleEndian.Uint16(data[offset:]))
			offset += 2
			oh.Count = uint16(count)
			objSize := objectDataSize(oh.Group, oh.Variation)
			for i := 0; i < count && offset+2+objSize <= len(data); i++ {
				idx := binary.LittleEndian.Uint16(data[offset:])
				offset += 2
				objData := make([]byte, objSize)
				copy(objData, data[offset:offset+objSize])
				offset += objSize
				oh.PrefixedData = append(oh.PrefixedData, PrefixedObject{Index: idx, Data: objData})
			}
		}

		apdu.Objects = append(apdu.Objects, oh)
	}

	return apdu, nil
}

// objectDataSize returns the per-object byte size for known group/variations.
func objectDataSize(group, variation uint8) int {
	switch {
	case group == GroupBinaryInput && variation == VarBIStatus:
		return 1
	case group == GroupBinaryOutput && variation == VarBOStatus:
		return 1
	case group == GroupCROB && variation == VarCROB:
		return 11 // control(1) + count(1) + onTime(4) + offTime(4) + status(1)
	case group == GroupAnalogInput && variation == VarAIFloat:
		return 5 // flags(1) + float32(4)
	case group == GroupAnalogOutput && variation == VarAOFloat:
		return 5
	case group == GroupAnalogOutputCmd && variation == VarAOCmdFloat:
		return 5 // status(1) + float32(4)
	default:
		return 0
	}
}

// ── Response Builders ────────────────────────────────────────────

// BuildResponse builds a response APDU with the given sequence, IIN, and object data.
func BuildResponse(reqSeq uint8, iin uint16, objects []byte) []byte {
	ac := acFIR | acFIN | (reqSeq & acSEQ)
	buf := []byte{
		ac,
		FCResponse,
		byte(iin >> 8), // IIN1
		byte(iin),      // IIN2
	}
	buf = append(buf, objects...)
	return buf
}

// EncodeBinaryInputs encodes binary input points as Group 1 Var 2 with start/stop qualifier.
func EncodeBinaryInputs(startIdx uint16, values []bool) []byte {
	if len(values) == 0 {
		return nil
	}
	stopIdx := startIdx + uint16(len(values)) - 1

	buf := []byte{
		GroupBinaryInput,
		VarBIStatus,
		QualStartStop8,
		byte(startIdx),
		byte(stopIdx),
	}
	for _, v := range values {
		flags := byte(0x01) // online
		if v {
			flags |= 0x80 // value bit
		}
		buf = append(buf, flags)
	}
	return buf
}

// EncodeBinaryOutputStatus encodes binary output status points as Group 10 Var 2.
func EncodeBinaryOutputStatus(startIdx uint16, values []bool) []byte {
	if len(values) == 0 {
		return nil
	}
	stopIdx := startIdx + uint16(len(values)) - 1

	buf := []byte{
		GroupBinaryOutput,
		VarBOStatus,
		QualStartStop8,
		byte(startIdx),
		byte(stopIdx),
	}
	for _, v := range values {
		flags := byte(0x01) // online
		if v {
			flags |= 0x80 // value bit
		}
		buf = append(buf, flags)
	}
	return buf
}

// EncodeAnalogInputs encodes analog input points as Group 30 Var 5 (float32).
func EncodeAnalogInputs(startIdx uint16, values []float32) []byte {
	if len(values) == 0 {
		return nil
	}
	stopIdx := startIdx + uint16(len(values)) - 1

	buf := []byte{
		GroupAnalogInput,
		VarAIFloat,
		QualStartStop8,
		byte(startIdx),
		byte(stopIdx),
	}
	for _, v := range values {
		flags := byte(0x01) // online
		buf = append(buf, flags)
		var fb [4]byte
		binary.LittleEndian.PutUint32(fb[:], math.Float32bits(v))
		buf = append(buf, fb[:]...)
	}
	return buf
}

// EncodeAnalogOutputStatus encodes analog output status as Group 40 Var 3 (float32).
func EncodeAnalogOutputStatus(startIdx uint16, values []float32) []byte {
	if len(values) == 0 {
		return nil
	}
	stopIdx := startIdx + uint16(len(values)) - 1

	buf := []byte{
		GroupAnalogOutput,
		VarAOFloat,
		QualStartStop8,
		byte(startIdx),
		byte(stopIdx),
	}
	for _, v := range values {
		flags := byte(0x01) // online
		buf = append(buf, flags)
		var fb [4]byte
		binary.LittleEndian.PutUint32(fb[:], math.Float32bits(v))
		buf = append(buf, fb[:]...)
	}
	return buf
}

// EncodeCROBResponse encodes a CROB response for Group 12 Var 1.
func EncodeCROBResponse(index uint16, controlCode uint8, status uint8) []byte {
	buf := []byte{
		GroupCROB,
		VarCROB,
		QualCount8Pfx8,
		1,            // count
		byte(index),  // prefix index
	}
	// CROB data: control(1) + count(1) + onTime(4) + offTime(4) + status(1) = 11
	obj := make([]byte, 11)
	obj[0] = controlCode
	obj[1] = 1 // count
	// onTime and offTime = 0
	obj[10] = status
	buf = append(buf, obj...)
	return buf
}

// EncodeAnalogOutputResponse encodes an analog output command response for Group 41 Var 3.
func EncodeAnalogOutputResponse(index uint16, value float32, status uint8) []byte {
	buf := []byte{
		GroupAnalogOutputCmd,
		VarAOCmdFloat,
		QualCount8Pfx8,
		1,            // count
		byte(index),  // prefix index
	}
	obj := make([]byte, 5)
	obj[0] = status
	binary.LittleEndian.PutUint32(obj[1:], math.Float32bits(value))
	buf = append(buf, obj...)
	return buf
}

// ParseCROB extracts the control code from a CROB data block.
func ParseCROB(data []byte) (controlCode uint8, ok bool) {
	if len(data) < 11 {
		return 0, false
	}
	return data[0], true
}

// ParseAnalogOutputCmd extracts the float value from an analog output command.
func ParseAnalogOutputCmd(data []byte) (float32, bool) {
	if len(data) < 5 {
		return 0, false
	}
	bits := binary.LittleEndian.Uint32(data[1:5])
	return math.Float32frombits(bits), true
}
