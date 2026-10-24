// SPDX-License-Identifier: Apache-2.0

package iec104go

const (
	// F_FR_NA_1 is file ready (120).
	F_FR_NA_1 TypeID = 120 + iota
	// F_SR_NA_1 is section ready (121).
	F_SR_NA_1
	// F_SC_NA_1 is select and call (122).
	F_SC_NA_1
	// F_LS_NA_1 is last section/segment (123).
	F_LS_NA_1
	// F_AF_NA_1 is file/section acknowledgement (124).
	F_AF_NA_1
	// F_SG_NA_1 is a variable-length segment (125).
	F_SG_NA_1
	// F_DR_TA_1 is a directory with per-entry CP56 time (126).
	F_DR_TA_1
	// F_SC_NB_1 is QueryLog with inclusive time range (127).
	F_SC_NB_1
)

// FileReady is NOF, uint24 LOF, FRQ. Qualifier bit7 means not ready.
type FileReady struct {
	Name      uint16
	Length    uint32
	Qualifier uint8
}

func (FileReady) elementType() TypeID { return F_FR_NA_1 }

// SectionReady is NOF, one-octet NOS, uint24 section length, SRQ.
type SectionReady struct {
	Name      uint16
	Section   uint8
	Length    uint32
	Qualifier uint8
}

func (SectionReady) elementType() TypeID { return F_SR_NA_1 }

// FileCall is NOF, NOS, SCQ (operation low nibble, error high nibble).
type FileCall struct {
	Name      uint16
	Section   uint8
	Qualifier uint8
}

func (FileCall) elementType() TypeID { return F_SC_NA_1 }

// FileLast is NOF, NOS, LSQ, CHS. LSQ3 ends a section; LSQ1 ends a file.
type FileLast struct {
	Name                uint16
	Section             uint8
	Qualifier, Checksum uint8
}

func (FileLast) elementType() TypeID { return F_LS_NA_1 }

// FileACK is NOF, NOS, AFQ (acknowledgement low nibble, error high nibble).
type FileACK struct {
	Name      uint16
	Section   uint8
	Qualifier uint8
}

func (FileACK) elementType() TypeID { return F_AF_NA_1 }

// FileSegment contains owned data after NOF, NOS and a derived one-octet LOS.
// The codec retains zero lengths/reserved values; procedures reject unused zero lengths.
type FileSegment struct {
	Name    uint16
	Section uint8
	Data    []byte
}

func (FileSegment) elementType() TypeID { return F_SG_NA_1 }

// FileDirectory is NOF, LOF, SOF, CP56; every entry shares the directory IOA.
// SOF bits0..4 are status; 0x20 last entry; 0x40 subdirectory; 0x80 active.
type FileDirectory struct {
	Name   uint16
	Length uint32
	Status uint8
	Time   CP56Time2a
}

func (FileDirectory) elementType() TypeID { return F_DR_TA_1 }

// QueryLog requests the inclusive interval [Start,Stop]. All-zero endpoints
// mean beginning/end respectively; they are not invalid-calendar errors here.
type QueryLog struct {
	Name        uint16
	Start, Stop CP56Time2a
}

func (QueryLog) elementType() TypeID { return F_SC_NB_1 }

// File operation/acknowledgement values occupy the low nibble of SCQ/AFQ.
const (
	// SelectFile selects a file before download.
	SelectFile uint8 = 1
	// CallFile requests the selected file.
	CallFile uint8 = 2
	// DeactivateFile aborts the selected file transfer.
	DeactivateFile uint8 = 3
	// DeleteFile requests deletion (not supported by the serving helper).
	DeleteFile uint8 = 4
	// SelectSection selects a section (not supported by the serving helper).
	SelectSection uint8 = 5
	// CallSection requests section data.
	CallSection uint8 = 6
	// DeactivateSection deactivates a section (not supported by the serving helper).
	DeactivateSection uint8 = 7
	// PositiveFileACK acknowledges the complete file.
	PositiveFileACK uint8 = 1
	// NegativeFileACK rejects the complete file.
	NegativeFileACK uint8 = 2
	// PositiveSectionACK acknowledges one section.
	PositiveSectionACK uint8 = 3
	// NegativeSectionACK rejects one section.
	NegativeSectionACK uint8 = 4
)

// File error nibble values are shifted left four bits in SCQ/AFQ.
const (
	// FileMemoryUnavailable indicates unavailable requested storage/content.
	FileMemoryUnavailable uint8 = 1
	// FileChecksumFailure indicates a checksum mismatch.
	FileChecksumFailure uint8 = 2
	// FileUnexpectedService indicates an unsupported/unordered service.
	FileUnexpectedService uint8 = 3
	// FileUnknownName indicates an unknown file.
	FileUnknownName uint8 = 4
	// FileUnknownSection indicates an unknown section.
	FileUnknownSection uint8 = 5
)

// FileChecksum sums content octets modulo 256, excluding all protocol metadata.
func FileChecksum(data []byte) uint8 {
	var sum uint8
	for _, b := range data {
		sum += b
	}
	return sum
}
