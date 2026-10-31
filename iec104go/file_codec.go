// SPDX-License-Identifier: Apache-2.0

package iec104go

import "fmt"

func isFileType(id TypeID) bool { return id >= F_FR_NA_1 && id <= F_SC_NB_1 }
func fileStructure(h Header) error {
	if !isFileType(h.Type) {
		return nil
	}
	if h.Type == F_DR_TA_1 {
		if h.Sequence {
			return nil
		}
	} else if !h.Sequence && h.Count == 1 {
		return nil
	}
	return fmt.Errorf("%w: file ASDU SQ/count", ErrMalformed)
}
func objectAddress(id TypeID, first uint32, index int) uint32 {
	if id == F_DR_TA_1 {
		return first
	}
	return first + uint32(index)
}
func elementLength(id TypeID, b []byte) (int, error) {
	n, _ := id.ElementSize()
	if id == F_SG_NA_1 {
		if len(b) < 4 {
			return 0, fmt.Errorf("%w: segment header", ErrMalformed)
		}
		n = 4 + int(b[3])
	}
	if len(b) < n {
		return 0, fmt.Errorf("%w: truncated element", ErrMalformed)
	}
	return n, nil
}
func encodeFileElement(v Element) ([]byte, error) {
	length := func(b []byte, n uint32, q uint8) ([]byte, error) {
		if n > 0xffffff {
			return nil, errFieldRange
		}
		return append(appendAddress(b, n, 3), q), nil
	}
	switch e := v.(type) {
	case FileReady:
		return length(put16(e.Name), e.Length, e.Qualifier)
	case SectionReady:
		return length(append(put16(e.Name), e.Section), e.Length, e.Qualifier)
	case FileCall:
		return append(put16(e.Name), e.Section, e.Qualifier), nil
	case FileLast:
		return append(put16(e.Name), e.Section, e.Qualifier, e.Checksum), nil
	case FileACK:
		return append(put16(e.Name), e.Section, e.Qualifier), nil
	case FileSegment:
		if len(e.Data) > 255 {
			return nil, errFieldRange
		}
		return append(append(put16(e.Name), e.Section, byte(len(e.Data))), e.Data...), nil
	case FileDirectory:
		b, err := length(put16(e.Name), e.Length, e.Status)
		if err != nil {
			return nil, err
		}
		tag, err := e.Time.MarshalBinary()
		return append(b, tag...), err
	case QueryLog:
		start, err := e.Start.MarshalBinary()
		if err != nil {
			return nil, err
		}
		stop, err := e.Stop.MarshalBinary()
		return append(append(put16(e.Name), start...), stop...), err
	}
	return nil, fmt.Errorf("%w: element must be a supported value type", ErrMalformed)
}
func decodeFileElement(id TypeID, b []byte) (Element, error) {
	n, err := elementLength(id, b)
	if err != nil {
		return nil, err
	}
	if len(b) != n {
		return nil, fmt.Errorf("%w: file element length", ErrMalformed)
	}
	name := uint16(readAddress(b, 2))
	switch id {
	case F_FR_NA_1:
		return FileReady{name, readAddress(b[2:], 3), b[5]}, nil
	case F_SR_NA_1:
		return SectionReady{name, b[2], readAddress(b[3:], 3), b[6]}, nil
	case F_SC_NA_1:
		return FileCall{name, b[2], b[3]}, nil
	case F_LS_NA_1:
		return FileLast{name, b[2], b[3], b[4]}, nil
	case F_AF_NA_1:
		return FileACK{name, b[2], b[3]}, nil
	case F_SG_NA_1:
		return FileSegment{name, b[2], append([]byte(nil), b[4:]...)}, nil
	case F_DR_TA_1:
		tag, err := DecodeCP56Time2a(b[6:])
		return FileDirectory{name, readAddress(b[2:], 3), b[5], tag}, err
	case F_SC_NB_1:
		start, err := DecodeCP56Time2a(b[2:9])
		if err != nil {
			return nil, err
		}
		stop, err := DecodeCP56Time2a(b[9:])
		return QueryLog{name, start, stop}, err
	}
	return nil, ErrUnsupportedType
}

func fileElementName(e Element) (uint16, bool) {
	switch v := e.(type) {
	case FileReady:
		return v.Name, true
	case SectionReady:
		return v.Name, true
	case FileCall:
		return v.Name, true
	case FileLast:
		return v.Name, true
	case FileACK:
		return v.Name, true
	case FileSegment:
		return v.Name, true
	case FileDirectory:
		return v.Name, true
	case QueryLog:
		return v.Name, true
	}
	return 0, false
}
