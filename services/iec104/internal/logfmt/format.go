// SPDX-License-Identifier: Apache-2.0

// Package logfmt preserves the stations' consumer-visible common.c text.
package logfmt

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	iec "github.com/tonylturner/iec104go"
)

// Timestamp renders UTC with exactly millisecond precision.
func Timestamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// CP56 renders a calendar and the independent time-invalid suffix. Invalid
// calendars are reported to the caller rather than replaced with host time.
func CP56(t iec.CP56Time2a) (string, error) {
	utc, ok := t.Time()
	if !ok {
		return "", fmt.Errorf("invalid CP56 calendar")
	}
	s := Timestamp(utc)
	if t.Invalid {
		s += "(IV)"
	}
	return s, nil
}

// Quality renders quality flags in the frozen IV|NT|SB|BL|OV order.
func Quality(q iec.Quality) string {
	if q == 0 {
		return "GOOD"
	}
	var parts []string
	for _, f := range []struct {
		bit  iec.Quality
		name string
	}{
		{iec.Invalid, "IV"}, {iec.NotTopical, "NT"}, {iec.Substituted, "SB"},
		{iec.Blocked, "BL"}, {iec.Overflow, "OV"},
	} {
		if q&f.bit != 0 {
			parts = append(parts, f.name)
		}
	}
	return strings.Join(parts, "|")
}

// DPI renders the reference's uppercase double-point state.
func DPI(value uint8) string {
	switch value {
	case 0:
		return "INTERMEDIATE"
	case 1:
		return "OFF"
	case 2:
		return "ON"
	default:
		return "INDETERMINATE"
	}
}
func onoff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}
func se(q iec.CommandQualifier) string {
	if q.Select() {
		return "SELECT"
	}
	return "EXECUTE"
}
func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Describe formats the application's supported object kinds. Timed step
// positions intentionally omit the time, exactly as common.c does.
func Describe(element iec.Element) (string, error) {
	if t, ok := element.(iec.Timed); ok {
		s, err := Describe(t.Value)
		if err != nil {
			return "", err
		}
		switch t.Value.(type) {
		case iec.SinglePoint, iec.DoublePoint:
			stamp, err := CP56(t.Time)
			if err != nil {
				return "", err
			}
			return s + " time=" + stamp, nil
		case iec.StepPosition:
			return s, nil
		default:
			return "(no decoder for this type)", nil
		}
	}
	switch v := element.(type) {
	case iec.SinglePoint:
		return fmt.Sprintf("value=%s q=%s", onoff(v.SIQ.State()), Quality(v.SIQ.Quality())), nil
	case iec.DoublePoint:
		return fmt.Sprintf("value=%s q=%s", DPI(v.DIQ.State()), Quality(v.DIQ.Quality())), nil
	case iec.Float:
		return fmt.Sprintf("value=%s q=%s", floatText(v.Value), Quality(v.Quality)), nil
	case iec.StepPosition:
		return fmt.Sprintf("value=%d transient=%d q=%s", v.Value.Position(), bit(v.Value.Transient()), Quality(v.Quality)), nil
	case iec.DoubleCommand:
		return fmt.Sprintf("dcs=%d(%s) qu=%d se=%s", v.Qualifier.State(), DPI(v.Qualifier.State()), v.Qualifier.QU(), se(v.Qualifier)), nil
	case iec.SingleCommand:
		return fmt.Sprintf("scs=%s qu=%d se=%s", onoff(v.Qualifier.State()&1 != 0), v.Qualifier.QU(), se(v.Qualifier)), nil
	case iec.RegulatingCommand:
		return fmt.Sprintf("rcs=%d qu=%d se=%s", v.Qualifier.State(), v.Qualifier.QU(), se(v.Qualifier)), nil
	case iec.Interrogation:
		return fmt.Sprintf("qoi=%d", v.QOI), nil
	default:
		return "(no decoder for this type)", nil
	}
}

func floatText(v float32) string {
	f := float64(v)
	if math.IsNaN(f) {
		if math.Signbit(f) {
			return "-nan"
		}
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	return fmt.Sprintf("%.3f", f)
}

// ASDULines returns the ASDU header followed by configuration-order objects.
func ASDULines(direction string, m iec.Message) ([]string, error) {
	h := m.GetHeader()
	lines := []string{fmt.Sprintf("%s ASDU type=%s(%d) cot=%s(%d) neg=%d test=%d oa=%d ca=%d objects=%d",
		direction, h.Type, h.Type, h.Cause, h.Cause, bit(h.Negative), bit(h.Test), h.Originator, h.CommonAddress, h.Count)}
	var objects []iec.Object
	switch a := m.(type) {
	case *iec.ASDU:
		objects = a.Objects
	case iec.ASDU:
		objects = a.Objects
	}
	if objects != nil {
		for _, o := range objects {
			text, err := Describe(o.Element)
			if err != nil {
				return nil, err
			}
			lines = append(lines, fmt.Sprintf("%s   ioa=%d %s", direction, o.Address, text))
		}
	} else {
		for i := 0; i < int(h.Count); i++ {
			lines = append(lines, fmt.Sprintf("%s   object %d does not decode", direction, i))
		}
	}
	return lines, nil
}

// Logger serializes whole lines, uses a caller-owned stream and reports I/O
// errors. Choosing stdout vs stderr is the executable's responsibility.
type Logger struct {
	mu      sync.Mutex
	stream  io.Writer
	program string
	now     func() time.Time
}

// NewLogger constructs a logger; nil now uses time.Now.
func NewLogger(stream io.Writer, program string, now func() time.Time) *Logger {
	if now == nil {
		now = time.Now
	}
	return &Logger{stream: stream, program: program, now: now}
}

// Printf writes one timestamped evidence line.
func (l *Logger) Printf(format string, args ...any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := fmt.Sprintf(format, args...)
	if len(text) > 1023 {
		text = text[:1023]
	}
	_, err := fmt.Fprintf(l.stream, "%s %s %s\n", Timestamp(l.now()), l.program, text)
	return err
}

// ParseInt accepts the reference strtol decimal grammar, including a leading
// sign or ASCII whitespace, but no trailing whitespace or junk.
func ParseInt(s string, low, high int64) (int64, error) {
	s = strings.TrimLeft(s, " \t\r\n\v\f")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < low || n > high {
		return 0, fmt.Errorf("invalid integer")
	}
	return n, nil
}

// ParseEndpoint splits at the last colon, defaults to port 2404, and bounds
// the host like the reference's C buffer of hostBuf bytes (including NUL):
// the host must be shorter than hostBuf. The reference callers use 64
// (iec104cmd, CC --http) and 128 (CC --target, historian). It is not an IPv6
// URL parser.
func ParseEndpoint(s string, hostBuf int) (string, int, error) {
	host, port := s, 2404
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		host = s[:i]
		n, err := ParseInt(s[i+1:], 1, 65535)
		if err != nil {
			return "", 0, err
		}
		port = int(n)
	}
	if len(host) == 0 || len(host) >= hostBuf {
		return "", 0, fmt.Errorf("invalid host")
	}
	return host, port, nil
}
