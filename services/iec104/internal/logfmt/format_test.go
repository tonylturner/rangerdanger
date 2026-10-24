// SPDX-License-Identifier: Apache-2.0

package logfmt

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
)

func TestCommonGoldens(t *testing.T) {
	stamp := time.Date(2026, 10, 23, 12, 34, 56, 789999999, time.UTC)
	tag := iec.CP56FromTime(stamp, true)
	cp, err := CP56(tag)
	if err != nil || cp != "2026-10-23T12:34:56.789Z(IV)" {
		t.Fatal(cp, err)
	}
	for _, tc := range []struct {
		q    iec.Quality
		want string
	}{
		{0, "GOOD"}, {iec.Invalid | iec.NotTopical, "IV|NT"}, {0xf1, "IV|NT|SB|BL|OV"},
		{iec.Overflow, "OV"}, {2, ""},
	} {
		if got := Quality(tc.q); got != tc.want {
			t.Fatalf("quality %x: %q", tc.q, got)
		}
	}
	vti, err := iec.NewVTI(-12, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		element iec.Element
		want    string
	}{
		{"sp", iec.SinglePoint{SIQ: 1}, "value=ON q=GOOD"},
		{"sp-off", iec.SinglePoint{SIQ: 0xc0}, "value=OFF q=IV|NT"},
		{"dp-intermediate", iec.DoublePoint{DIQ: 0}, "value=INTERMEDIATE q=GOOD"},
		{"dp-off", iec.DoublePoint{DIQ: 1}, "value=OFF q=GOOD"},
		{"dp-on", iec.DoublePoint{DIQ: 2}, "value=ON q=GOOD"},
		{"dp-indeterminate", iec.DoublePoint{DIQ: 3}, "value=INDETERMINATE q=GOOD"},
		{"timed-sp", iec.Timed{Value: iec.SinglePoint{SIQ: 1}, Time: tag}, "value=ON q=GOOD time=2026-10-23T12:34:56.789Z(IV)"},
		{"timed-dp", iec.Timed{Value: iec.DoublePoint{DIQ: 0x82}, Time: tag}, "value=ON q=IV time=2026-10-23T12:34:56.789Z(IV)"},
		{"float", iec.Float{Value: 12.3456, Quality: iec.NotTopical}, "value=12.346 q=NT"},
		{"float-zero", iec.Float{}, "value=0.000 q=GOOD"},
		{"float-nan", iec.Float{Value: math.Float32frombits(0x7fc00000)}, "value=nan q=GOOD"},
		{"float-inf", iec.Float{Value: float32(math.Inf(1))}, "value=inf q=GOOD"},
		{"float-negative-inf", iec.Float{Value: float32(math.Inf(-1))}, "value=-inf q=GOOD"},
		{"step", iec.StepPosition{Value: vti, Quality: iec.Invalid}, "value=-12 transient=1 q=IV"},
		{"timed-step", iec.Timed{Value: iec.StepPosition{Value: vti}, Time: tag}, "value=-12 transient=1 q=GOOD"},
		{"dc-select", iec.DoubleCommand{Qualifier: 0x82}, "dcs=2(ON) qu=0 se=SELECT"},
		{"dc-execute", iec.DoubleCommand{Qualifier: 0x05}, "dcs=1(OFF) qu=1 se=EXECUTE"},
		{"sc-select", iec.SingleCommand{Qualifier: 0x81}, "scs=ON qu=0 se=SELECT"},
		{"sc-execute", iec.SingleCommand{Qualifier: 0}, "scs=OFF qu=0 se=EXECUTE"},
		{"rc", iec.RegulatingCommand{Qualifier: 2}, "rcs=2 qu=0 se=EXECUTE"},
		{"gi", iec.Interrogation{QOI: 20}, "qoi=20"},
		{"unknown", iec.Bitstring32{}, "(no decoder for this type)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Describe(tc.element)
			if err != nil || got != tc.want {
				t.Fatalf("got %q (%v), want %q", got, err, tc.want)
			}
		})
	}
	var out bytes.Buffer
	l := NewLogger(&out, "iec104-rtu", func() time.Time { return stamp })
	if err := l.Printf("peer=%s", "127.0.0.1:2404"); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "2026-10-23T12:34:56.789Z iec104-rtu peer=127.0.0.1:2404\n" {
		t.Fatal(got)
	}
	if _, err := CP56(iec.CP56Time2a{}); err == nil {
		t.Fatal("invalid calendar")
	}
}

func TestASDULines(t *testing.T) {
	a := iec.ASDU{Header: iec.Header{Type: iec.M_DP_NA_1, Cause: iec.InterrogatedStation, Originator: 1, CommonAddress: 1},
		Objects: []iec.Object{{Address: 1001, Element: iec.DoublePoint{DIQ: 2}}}}
	want := []string{"rx ASDU type=M_DP_NA_1(3) cot=inrogen(20) neg=0 test=0 oa=1 ca=1 objects=1", "rx   ioa=1001 value=ON q=GOOD"}
	for _, m := range []iec.Message{a, &a} {
		got, err := ASDULines("rx", m)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(got, err)
		}
	}
	raw := iec.RawASDU{Header: iec.Header{Type: 200, Cause: iec.UnknownType, Negative: true, Test: true, Count: 1}}
	got, err := ASDULines("rx", raw)
	if err != nil || !reflect.DeepEqual(got, []string{
		"rx ASDU type=TypeID(200)(200) cot=unknown_type(44) neg=1 test=1 oa=0 ca=0 objects=1",
		"rx   object 0 does not decode",
	}) {
		t.Fatal(got, err)
	}
}

func TestEndpointAndInteger(t *testing.T) {
	for _, tc := range []struct {
		s, host string
		port    int
		good    bool
	}{
		{"host", "host", 2404, true}, {"127.0.0.1:65535", "127.0.0.1", 65535, true},
		{"host:+1", "host", 1, true}, {"host: 2404", "host", 2404, true},
		{"", "", 0, false}, {":2404", "", 0, false}, {"host:0", "", 0, false},
		{"host:65536", "", 0, false}, {"host:2404x", "", 0, false}, {"host:2404 ", "", 0, false},
		{strings.Repeat("x", 64), "", 0, false},
	} {
		t.Run(tc.s, func(t *testing.T) {
			h, p, err := ParseEndpoint(tc.s)
			if (err == nil) != tc.good || (tc.good && (h != tc.host || p != tc.port)) {
				t.Fatal(h, p, err)
			}
		})
	}
	for _, s := range []string{"", "1x", "1 ", "9223372036854775808", "1_0", "0x10"} {
		if _, err := ParseInt(s, 0, 100); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if n, err := ParseInt(" \t+42", 0, 100); err != nil || n != 42 {
		t.Fatal(n, err)
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
func TestLoggerFailure(t *testing.T) {
	if err := NewLogger(failedWriter{}, "rtu", nil).Printf("test"); err == nil {
		t.Fatal("ignored log failure")
	}
}
