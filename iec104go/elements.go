// SPDX-License-Identifier: Apache-2.0

package iec104go

// Element is a supported information-element value. Implementations are value
// types, so decoded objects may be retained independently of reader buffers.
// Use Timed for the corresponding CP56-tagged process or command type.
type Element interface{ elementType() TypeID }

// Quality holds the shared QDS/SIQ/DIQ quality flag positions. Reserved bits are
// deliberately retained in each raw descriptor rather than normalized away.
type Quality uint8

const (
	// Overflow marks an out-of-range measured value (QDS only).
	Overflow Quality = 0x01
	// Blocked marks a blocked point.
	Blocked Quality = 0x10
	// Substituted marks an operator/substitute value.
	Substituted Quality = 0x20
	// NotTopical marks a stale value.
	NotTopical Quality = 0x40
	// Invalid marks an invalid point value, independently of time validity.
	Invalid Quality = 0x80
)

// SIQ contains SPI in bit 0 and quality in bits 4..7; other bits are preserved.
type SIQ uint8

// State returns SPI.
func (s SIQ) State() bool { return s&1 != 0 }

// Quality returns the applicable quality flags.
func (s SIQ) Quality() Quality { return Quality(s & 0xf0) }

// DIQ contains DPI (0 intermediate, 1 off, 2 on, 3 indeterminate) and quality.
type DIQ uint8

// State returns the two-bit DPI, without rejecting intermediate states.
func (s DIQ) State() uint8 { return uint8(s) & 3 }

// Quality returns the applicable quality flags.
func (s DIQ) Quality() Quality { return Quality(s & 0xf0) }

// VTI contains a signed seven-bit step and the transient flag.
type VTI uint8

// Position sign-extends the seven-bit position.
func (v VTI) Position() int8 { return int8(uint8(v)<<1) >> 1 }

// Transient reports the transient-state bit.
func (v VTI) Transient() bool { return v&0x80 != 0 }

// NewVTI constructs a step without wrapping an out-of-range value.
func NewVTI(position int8, transient bool) (VTI, error) {
	if position < -64 || position > 63 {
		return 0, errFieldRange
	}
	v := VTI(uint8(position) & 127)
	if transient {
		v |= 128
	}
	return v, nil
}

// CommandQualifier preserves the SCO/DCO/RCO octet, including state and SE.
type CommandQualifier uint8

// State returns the low two bits (single commands use only bit zero).
func (q CommandQualifier) State() uint8 { return uint8(q) & 3 }

// Select reports select rather than execute.
func (q CommandQualifier) Select() bool { return q&128 != 0 }

// QU returns the five-bit command qualifier.
func (q CommandQualifier) QU() uint8 { return uint8(q) >> 2 & 31 }

// NewCommandQualifier validates field widths; application policy decides which
// state and QU values to accept for a particular command.
func NewCommandQualifier(state, qu uint8, selectCommand bool) (CommandQualifier, error) {
	if state > 3 || qu > 31 {
		return 0, errFieldRange
	}
	q := CommandQualifier(state | qu<<2)
	if selectCommand {
		q |= 128
	}
	return q, nil
}

// SetpointQualifier preserves QL and SE on set-point commands.
type SetpointQualifier uint8

// QL returns the seven-bit qualifier.
func (q SetpointQualifier) QL() uint8 { return uint8(q) & 127 }

// Select reports select rather than execute.
func (q SetpointQualifier) Select() bool { return q&128 != 0 }

// Timed appends CP56Time2a to a supported untimed process/command element.
// Protection events already include CP16 elapsed time in Value.
type Timed struct {
	Value Element
	Time  CP56Time2a
}

func (t Timed) elementType() TypeID { return timedType(t.Value) }

// SinglePoint is M_SP_NA_1.
type SinglePoint struct{ SIQ SIQ }

func (SinglePoint) elementType() TypeID { return M_SP_NA_1 }

// DoublePoint is M_DP_NA_1.
type DoublePoint struct{ DIQ DIQ }

func (DoublePoint) elementType() TypeID { return M_DP_NA_1 }

// StepPosition is M_ST_NA_1.
type StepPosition struct {
	Value   VTI
	Quality Quality
}

func (StepPosition) elementType() TypeID { return M_ST_NA_1 }

// Bitstring32 is M_BO_NA_1.
type Bitstring32 struct {
	Value   uint32
	Quality Quality
}

func (Bitstring32) elementType() TypeID { return M_BO_NA_1 }

// Normalized is M_ME_NA_1; Value is signed Q15, not a rounded float.
type Normalized struct {
	Value   int16
	Quality Quality
}

func (Normalized) elementType() TypeID { return M_ME_NA_1 }

// Scaled is M_ME_NB_1.
type Scaled struct {
	Value   int16
	Quality Quality
}

func (Scaled) elementType() TypeID { return M_ME_NB_1 }

// Float is M_ME_NC_1; nonfinite IEEE-754 values are retained, not filtered.
type Float struct {
	Value   float32
	Quality Quality
}

func (Float) elementType() TypeID { return M_ME_NC_1 }

// Counter is M_IT_NA_1. Flags contains sequence bits0..4, CY, CA and IV.
type Counter struct {
	Value int32
	Flags uint8
}

func (Counter) elementType() TypeID { return M_IT_NA_1 }

// PackedStatus is M_PS_NA_1: sixteen status bits followed by change detection.
type PackedStatus struct {
	Status, Change uint16
	Quality        Quality
}

func (PackedStatus) elementType() TypeID { return M_PS_NA_1 }

// NormalizedNoQuality is M_ME_ND_1.
type NormalizedNoQuality struct{ Value int16 }

func (NormalizedNoQuality) elementType() TypeID { return M_ME_ND_1 }

// ProtectionEvent is the SEP plus elapsed CP16 portion of M_EP_TD_1.
// It must be wrapped in Timed, because 104 does not permit the CP24 variant.
type ProtectionEvent struct {
	SEP     uint8
	Elapsed CP16Time2a
}

func (ProtectionEvent) elementType() TypeID { return 0 }

// ProtectionStart is the SPE, QDP and elapsed CP16 portion of M_EP_TE_1.
type ProtectionStart struct {
	SPE, QDP uint8
	Elapsed  CP16Time2a
}

func (ProtectionStart) elementType() TypeID { return 0 }

// ProtectionOutput is the OCI, QDP and elapsed CP16 portion of M_EP_TF_1.
type ProtectionOutput struct {
	OCI, QDP uint8
	Elapsed  CP16Time2a
}

func (ProtectionOutput) elementType() TypeID { return 0 }

// SingleCommand is C_SC_NA_1 (SCS is bit0; bit1 remains preserved).
type SingleCommand struct{ Qualifier CommandQualifier }

func (SingleCommand) elementType() TypeID { return C_SC_NA_1 }

// DoubleCommand is C_DC_NA_1.
type DoubleCommand struct{ Qualifier CommandQualifier }

func (DoubleCommand) elementType() TypeID { return C_DC_NA_1 }

// RegulatingCommand is C_RC_NA_1.
type RegulatingCommand struct{ Qualifier CommandQualifier }

func (RegulatingCommand) elementType() TypeID { return C_RC_NA_1 }

// SetpointNormalized is C_SE_NA_1 with a signed Q15 value.
type SetpointNormalized struct {
	Value     int16
	Qualifier SetpointQualifier
}

func (SetpointNormalized) elementType() TypeID { return C_SE_NA_1 }

// SetpointScaled is C_SE_NB_1.
type SetpointScaled struct {
	Value     int16
	Qualifier SetpointQualifier
}

func (SetpointScaled) elementType() TypeID { return C_SE_NB_1 }

// SetpointFloat is C_SE_NC_1.
type SetpointFloat struct {
	Value     float32
	Qualifier SetpointQualifier
}

func (SetpointFloat) elementType() TypeID { return C_SE_NC_1 }

// BitstringCommand is C_BO_NA_1 (no quality octet).
type BitstringCommand struct{ Value uint32 }

func (BitstringCommand) elementType() TypeID { return C_BO_NA_1 }

// Initialization is M_EI_NA_1, with COI including local-parameter-change bit.
type Initialization struct{ COI uint8 }

func (Initialization) elementType() TypeID { return M_EI_NA_1 }

// Interrogation is C_IC_NA_1 (QOI 20 station, 21..36 groups).
type Interrogation struct{ QOI uint8 }

func (Interrogation) elementType() TypeID { return C_IC_NA_1 }

// CounterInterrogation is C_CI_NA_1; QCC contains request and freeze bits.
type CounterInterrogation struct{ QCC uint8 }

func (CounterInterrogation) elementType() TypeID { return C_CI_NA_1 }

// ReadCommand is C_RD_NA_1, containing an IOA but no element bytes.
type ReadCommand struct{}

func (ReadCommand) elementType() TypeID { return C_RD_NA_1 }

// ClockSync is C_CS_NA_1.
type ClockSync struct{ Time CP56Time2a }

func (ClockSync) elementType() TypeID { return C_CS_NA_1 }

// ResetProcess is C_RP_NA_1.
type ResetProcess struct{ QRP uint8 }

func (ResetProcess) elementType() TypeID { return C_RP_NA_1 }

// TestCommand is C_TS_TA_1 with a test sequence counter and CP56 tag.
type TestCommand struct {
	Sequence uint16
	Time     CP56Time2a
}

func (TestCommand) elementType() TypeID { return C_TS_TA_1 }

// ParameterNormalized is P_ME_NA_1 with raw QPM and signed Q15 value.
type ParameterNormalized struct {
	Value int16
	QPM   uint8
}

func (ParameterNormalized) elementType() TypeID { return P_ME_NA_1 }

// ParameterScaled is P_ME_NB_1.
type ParameterScaled struct {
	Value int16
	QPM   uint8
}

func (ParameterScaled) elementType() TypeID { return P_ME_NB_1 }

// ParameterFloat is P_ME_NC_1.
type ParameterFloat struct {
	Value float32
	QPM   uint8
}

func (ParameterFloat) elementType() TypeID { return P_ME_NC_1 }

// ParameterActivation is P_AC_NA_1.
type ParameterActivation struct{ QPA uint8 }

func (ParameterActivation) elementType() TypeID { return P_AC_NA_1 }
