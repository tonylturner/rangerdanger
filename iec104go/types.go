// SPDX-License-Identifier: Apache-2.0

package iec104go

import "fmt"

// TypeID identifies the information-element layout in an ASDU.
type TypeID uint8

const (
	// M_SP_NA_1 is companion-standard type 1.
	M_SP_NA_1 TypeID = 1
	// M_DP_NA_1 is companion-standard type 3.
	M_DP_NA_1 TypeID = 3
	// M_ST_NA_1 is companion-standard type 5.
	M_ST_NA_1 TypeID = 5
	// M_BO_NA_1 is companion-standard type 7.
	M_BO_NA_1 TypeID = 7
	// M_ME_NA_1 is companion-standard type 9.
	M_ME_NA_1 TypeID = 9
	// M_ME_NB_1 is companion-standard type 11.
	M_ME_NB_1 TypeID = 11
	// M_ME_NC_1 is companion-standard type 13.
	M_ME_NC_1 TypeID = 13
	// M_IT_NA_1 is companion-standard type 15.
	M_IT_NA_1 TypeID = 15
	// M_PS_NA_1 is companion-standard type 20.
	M_PS_NA_1 TypeID = 20
	// M_ME_ND_1 is companion-standard type 21.
	M_ME_ND_1 TypeID = 21
	// M_SP_TB_1 is companion-standard type 30.
	M_SP_TB_1 TypeID = 30
	// M_DP_TB_1 is companion-standard type 31.
	M_DP_TB_1 TypeID = 31
	// M_ST_TB_1 is companion-standard type 32.
	M_ST_TB_1 TypeID = 32
	// M_BO_TB_1 is companion-standard type 33.
	M_BO_TB_1 TypeID = 33
	// M_ME_TD_1 is companion-standard type 34.
	M_ME_TD_1 TypeID = 34
	// M_ME_TE_1 is companion-standard type 35.
	M_ME_TE_1 TypeID = 35
	// M_ME_TF_1 is companion-standard type 36.
	M_ME_TF_1 TypeID = 36
	// M_IT_TB_1 is companion-standard type 37.
	M_IT_TB_1 TypeID = 37
	// M_EP_TD_1 is companion-standard type 38.
	M_EP_TD_1 TypeID = 38
	// M_EP_TE_1 is companion-standard type 39.
	M_EP_TE_1 TypeID = 39
	// M_EP_TF_1 is companion-standard type 40.
	M_EP_TF_1 TypeID = 40
	// C_SC_NA_1 is companion-standard type 45.
	C_SC_NA_1 TypeID = 45
	// C_DC_NA_1 is companion-standard type 46.
	C_DC_NA_1 TypeID = 46
	// C_RC_NA_1 is companion-standard type 47.
	C_RC_NA_1 TypeID = 47
	// C_SE_NA_1 is companion-standard type 48.
	C_SE_NA_1 TypeID = 48
	// C_SE_NB_1 is companion-standard type 49.
	C_SE_NB_1 TypeID = 49
	// C_SE_NC_1 is companion-standard type 50.
	C_SE_NC_1 TypeID = 50
	// C_BO_NA_1 is companion-standard type 51.
	C_BO_NA_1 TypeID = 51
	// C_SC_TA_1 is companion-standard type 58.
	C_SC_TA_1 TypeID = 58
	// C_DC_TA_1 is companion-standard type 59.
	C_DC_TA_1 TypeID = 59
	// C_RC_TA_1 is companion-standard type 60.
	C_RC_TA_1 TypeID = 60
	// C_SE_TA_1 is companion-standard type 61.
	C_SE_TA_1 TypeID = 61
	// C_SE_TB_1 is companion-standard type 62.
	C_SE_TB_1 TypeID = 62
	// C_SE_TC_1 is companion-standard type 63.
	C_SE_TC_1 TypeID = 63
	// C_BO_TA_1 is companion-standard type 64.
	C_BO_TA_1 TypeID = 64
	// M_EI_NA_1 is companion-standard type 70.
	M_EI_NA_1 TypeID = 70
	// C_IC_NA_1 is companion-standard type 100.
	C_IC_NA_1 TypeID = 100
	// C_CI_NA_1 is companion-standard type 101.
	C_CI_NA_1 TypeID = 101
	// C_RD_NA_1 is companion-standard type 102.
	C_RD_NA_1 TypeID = 102
	// C_CS_NA_1 is companion-standard type 103.
	C_CS_NA_1 TypeID = 103
	// C_RP_NA_1 is companion-standard type 105.
	C_RP_NA_1 TypeID = 105
	// C_TS_TA_1 is companion-standard type 107.
	C_TS_TA_1 TypeID = 107
	// P_ME_NA_1 is companion-standard type 110.
	P_ME_NA_1 TypeID = 110
	// P_ME_NB_1 is companion-standard type 111.
	P_ME_NB_1 TypeID = 111
	// P_ME_NC_1 is companion-standard type 112.
	P_ME_NC_1 TypeID = 112
	// P_AC_NA_1 is companion-standard type 113.
	P_AC_NA_1 TypeID = 113
)

var typeNames = map[TypeID]string{
	M_SP_NA_1: "M_SP_NA_1",
	M_DP_NA_1: "M_DP_NA_1",
	M_ST_NA_1: "M_ST_NA_1",
	M_BO_NA_1: "M_BO_NA_1",
	M_ME_NA_1: "M_ME_NA_1",
	M_ME_NB_1: "M_ME_NB_1",
	M_ME_NC_1: "M_ME_NC_1",
	M_IT_NA_1: "M_IT_NA_1",
	M_PS_NA_1: "M_PS_NA_1",
	M_ME_ND_1: "M_ME_ND_1",
	M_SP_TB_1: "M_SP_TB_1",
	M_DP_TB_1: "M_DP_TB_1",
	M_ST_TB_1: "M_ST_TB_1",
	M_BO_TB_1: "M_BO_TB_1",
	M_ME_TD_1: "M_ME_TD_1",
	M_ME_TE_1: "M_ME_TE_1",
	M_ME_TF_1: "M_ME_TF_1",
	M_IT_TB_1: "M_IT_TB_1",
	M_EP_TD_1: "M_EP_TD_1",
	M_EP_TE_1: "M_EP_TE_1",
	M_EP_TF_1: "M_EP_TF_1",
	C_SC_NA_1: "C_SC_NA_1",
	C_DC_NA_1: "C_DC_NA_1",
	C_RC_NA_1: "C_RC_NA_1",
	C_SE_NA_1: "C_SE_NA_1",
	C_SE_NB_1: "C_SE_NB_1",
	C_SE_NC_1: "C_SE_NC_1",
	C_BO_NA_1: "C_BO_NA_1",
	C_SC_TA_1: "C_SC_TA_1",
	C_DC_TA_1: "C_DC_TA_1",
	C_RC_TA_1: "C_RC_TA_1",
	C_SE_TA_1: "C_SE_TA_1",
	C_SE_TB_1: "C_SE_TB_1",
	C_SE_TC_1: "C_SE_TC_1",
	C_BO_TA_1: "C_BO_TA_1",
	M_EI_NA_1: "M_EI_NA_1",
	C_IC_NA_1: "C_IC_NA_1",
	C_CI_NA_1: "C_CI_NA_1",
	C_RD_NA_1: "C_RD_NA_1",
	C_CS_NA_1: "C_CS_NA_1",
	C_RP_NA_1: "C_RP_NA_1",
	C_TS_TA_1: "C_TS_TA_1",
	P_ME_NA_1: "P_ME_NA_1",
	P_ME_NB_1: "P_ME_NB_1",
	P_ME_NC_1: "P_ME_NC_1",
	P_AC_NA_1: "P_AC_NA_1",
	F_FR_NA_1: "F_FR_NA_1",
	F_SR_NA_1: "F_SR_NA_1",
	F_SC_NA_1: "F_SC_NA_1",
	F_LS_NA_1: "F_LS_NA_1",
	F_AF_NA_1: "F_AF_NA_1",
	F_SG_NA_1: "F_SG_NA_1",
	F_DR_TA_1: "F_DR_TA_1",
	F_SC_NB_1: "F_SC_NB_1",
}

var elementSizes = map[TypeID]int{
	M_SP_NA_1: 1,
	M_DP_NA_1: 1,
	M_ST_NA_1: 2,
	M_BO_NA_1: 5,
	M_ME_NA_1: 3,
	M_ME_NB_1: 3,
	M_ME_NC_1: 5,
	M_IT_NA_1: 5,
	M_PS_NA_1: 5,
	M_ME_ND_1: 2,
	M_SP_TB_1: 8,
	M_DP_TB_1: 8,
	M_ST_TB_1: 9,
	M_BO_TB_1: 12,
	M_ME_TD_1: 10,
	M_ME_TE_1: 10,
	M_ME_TF_1: 12,
	M_IT_TB_1: 12,
	M_EP_TD_1: 10,
	M_EP_TE_1: 11,
	M_EP_TF_1: 11,
	C_SC_NA_1: 1,
	C_DC_NA_1: 1,
	C_RC_NA_1: 1,
	C_SE_NA_1: 3,
	C_SE_NB_1: 3,
	C_SE_NC_1: 5,
	C_BO_NA_1: 4,
	C_SC_TA_1: 8,
	C_DC_TA_1: 8,
	C_RC_TA_1: 8,
	C_SE_TA_1: 10,
	C_SE_TB_1: 10,
	C_SE_TC_1: 12,
	C_BO_TA_1: 11,
	M_EI_NA_1: 1,
	C_IC_NA_1: 1,
	C_CI_NA_1: 1,
	C_RD_NA_1: 0,
	C_CS_NA_1: 7,
	C_RP_NA_1: 1,
	C_TS_TA_1: 9,
	P_ME_NA_1: 3,
	P_ME_NB_1: 3,
	P_ME_NC_1: 5,
	P_AC_NA_1: 1,
	F_FR_NA_1: 6,
	F_SR_NA_1: 7,
	F_SC_NA_1: 4,
	F_LS_NA_1: 5,
	F_AF_NA_1: 4,
	F_DR_TA_1: 13,
	F_SC_NB_1: 16,
}

// String returns the standard mnemonic, or the numeric ID if unsupported.
func (t TypeID) String() string {
	if s, ok := typeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("TypeID(%d)", t)
}

// ElementSize reports the supported fixed information-element size, excluding IOA.
// Variable-length F_SG_NA_1 returns (0, false); Supported also includes it.
func (t TypeID) ElementSize() (int, bool) { n, ok := elementSizes[t]; return n, ok }

// COT is the six-bit cause; negative confirmation and test are separate flags.
type COT uint8

const (
	// Periodic is cause 1 (per/cyc).
	Periodic COT = 1
	// Background is cause 2 (back).
	Background COT = 2
	// Spontaneous is cause 3 (spont).
	Spontaneous COT = 3
	// Initialized is cause 4 (init).
	Initialized COT = 4
	// Request is cause 5 (req).
	Request COT = 5
	// Activation is cause 6 (act).
	Activation COT = 6
	// ActivationCon is cause 7 (actcon).
	ActivationCon COT = 7
	// Deactivation is cause 8 (deact).
	Deactivation COT = 8
	// DeactivationCon is cause 9 (deactcon).
	DeactivationCon COT = 9
	// ActivationTerm is cause 10 (actterm).
	ActivationTerm COT = 10
	// ReturnRemote is cause 11 (retrem).
	ReturnRemote COT = 11
	// ReturnLocal is cause 12 (retloc).
	ReturnLocal COT = 12
	// FileTransfer is cause 13 (file).
	FileTransfer COT = 13
	// InterrogatedStation is cause 20 (inrogen).
	InterrogatedStation COT = 20
	// CounterStation is cause 37 (reqcogen).
	CounterStation COT = 37
	// UnknownType is cause 44 (unknown_type).
	UnknownType COT = 44
	// UnknownCause is cause 45 (unknown_cause).
	UnknownCause COT = 45
	// UnknownCA is cause 46 (unknown_asdu_address).
	UnknownCA COT = 46
	// UnknownIOA is cause 47 (unknown_object_address).
	UnknownIOA COT = 47
)

var cotNames = map[COT]string{
	Periodic:            "per/cyc",
	Background:          "back",
	Spontaneous:         "spont",
	Initialized:         "init",
	Request:             "req",
	Activation:          "act",
	ActivationCon:       "actcon",
	Deactivation:        "deact",
	DeactivationCon:     "deactcon",
	ActivationTerm:      "actterm",
	ReturnRemote:        "retrem",
	ReturnLocal:         "retloc",
	FileTransfer:        "file",
	InterrogatedStation: "inrogen",
	CounterStation:      "reqcogen",
	UnknownType:         "unknown_type",
	UnknownCause:        "unknown_cause",
	UnknownCA:           "unknown_asdu_address",
	UnknownIOA:          "unknown_object_address",
}

// String returns the conventional cause mnemonic, including interrogation groups.
func (c COT) String() string {
	if s, ok := cotNames[c]; ok {
		return s
	}
	if c >= 21 && c <= 36 {
		return fmt.Sprintf("inro%d", c-20)
	}
	if c >= 38 && c <= 41 {
		return fmt.Sprintf("reqco%d", c-37)
	}
	return fmt.Sprintf("COT(%d)", c)
}

// Supported reports whether typed encoding and decoding are implemented.
func (t TypeID) Supported() bool { _, ok := elementSizes[t]; return ok || t == F_SG_NA_1 }
