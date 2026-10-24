// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	iec "github.com/tonylturner/iec104go"
)

func TestMonitoringObjectGoldens(t *testing.T) {
	tag := iec.CP56Time2a{Year: 26, Month: 10, Day: 23, Hour: 12, Minute: 34, Milliseconds: 56789}
	tests := []struct {
		name    string
		typ     iec.TypeID
		element iec.Element
		value   string
		q       string
		ts      string
		text    string
	}{
		{"sp", iec.M_SP_NA_1, iec.SinglePoint{SIQ: 1}, "true", "GOOD", "null", "value=ON q=GOOD"},
		{"sp-invalid", iec.M_SP_NA_1, iec.SinglePoint{SIQ: 0xc0}, "false", "IV|NT", "null", "value=OFF q=IV|NT"},
		{"dp-intermediate", iec.M_DP_NA_1, iec.DoublePoint{DIQ: 0}, `"intermediate"`, "GOOD", "null", "value=INTERMEDIATE q=GOOD"},
		{"dp-open", iec.M_DP_NA_1, iec.DoublePoint{DIQ: 1}, `"open"`, "GOOD", "null", "value=OFF q=GOOD"},
		{"dp-closed", iec.M_DP_NA_1, iec.DoublePoint{DIQ: 2}, `"closed"`, "GOOD", "null", "value=ON q=GOOD"},
		{"dp-indeterminate", iec.M_DP_NA_1, iec.DoublePoint{DIQ: 3}, `"indeterminate"`, "GOOD", "null", "value=INDETERMINATE q=GOOD"},
		{"step", iec.M_ST_NA_1, iec.StepPosition{Value: 0xc0, Quality: 0xf1}, `{"value":-64,"transient":true}`, "IV|NT|SB|BL|OV", "null", "value=-64 transient=1 q=IV|NT|SB|BL|OV"},
		{"step-max", iec.M_ST_NA_1, iec.StepPosition{Value: 63}, `{"value":63,"transient":false}`, "GOOD", "null", "value=63 transient=0 q=GOOD"},
		{"float-zero", iec.M_ME_NC_1, iec.Float{}, "0.000", "GOOD", "null", "value=0.000 q=GOOD"},
		{"float", iec.M_ME_NC_1, iec.Float{Value: 1.23456, Quality: 0x10}, "1.235", "BL", "null", "value=1.235 q=BL"},
		{"sp-timed", iec.M_SP_TB_1, iec.Timed{Value: iec.SinglePoint{SIQ: 1}, Time: tag}, "true", "GOOD", `"2026-10-23T12:34:56.789Z"`, "value=ON q=GOOD time=2026-10-23T12:34:56.789Z"},
		{"dp-timed", iec.M_DP_TB_1, iec.Timed{Value: iec.DoublePoint{DIQ: 1}, Time: tag}, `"open"`, "GOOD", `"2026-10-23T12:34:56.789Z"`, "value=OFF q=GOOD time=2026-10-23T12:34:56.789Z"},
		{"step-timed", iec.M_ST_TB_1, iec.Timed{Value: iec.StepPosition{Value: 127}, Time: tag}, `{"value":-1,"transient":false}`, "GOOD", `"2026-10-23T12:34:56.789Z"`, "value=-1 transient=0 q=GOOD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errout bytes.Buffer
			o := output{out: &out, log: &errout, now: testRuntime().now}
			o.asdu(&iec.ASDU{Header: iec.Header{Type: tt.typ, Cause: iec.InterrogatedStation, CommonAddress: 1},
				Objects: []iec.Object{{Address: 1001, Element: tt.element}}}, true)
			want := `{"event":"asdu","at":"` + stamp + `","type":` + strconv.Itoa(int(tt.typ)) +
				`,"cot":20,"ioa":1001,"value":` + tt.value + `,"quality":"` + tt.q + `","time":` + tt.ts + "}\n"
			if out.String() != want {
				t.Errorf("stdout=%q want=%q", out.String(), want)
			}
			if !strings.HasSuffix(errout.String(), logs("rx   ioa=1001 "+tt.text)) {
				t.Errorf("stderr=%q want suffix=%q", errout.String(), tt.text)
			}
		})
	}
}
