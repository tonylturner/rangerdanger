// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"encoding/binary"
	"fmt"
	"time"
)

// CP16Time2a is an unsigned elapsed interval in milliseconds, not a calendar.
type CP16Time2a uint16

// MarshalBinary encodes the two interval octets.
func (c CP16Time2a) MarshalBinary() ([]byte, error) {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, uint16(c))
	return b, nil
}

// DecodeCP16Time2a requires exactly two octets.
func DecodeCP16Time2a(b []byte) (CP16Time2a, error) {
	if len(b) != 2 {
		return 0, fmt.Errorf("%w: CP16 length", ErrMalformed)
	}
	return CP16Time2a(binary.LittleEndian.Uint16(b)), nil
}

// CP56Time2a preserves every wire field, including invalid calendars and
// reserved bits. HourReserved and MonthReserved are masks in their original
// octet positions (0x60 and 0xf0). Time validity is separate from Invalid.
type CP56Time2a struct {
	Milliseconds  uint16
	Minute        uint8
	Invalid       bool
	RES1          bool
	Hour          uint8
	SummerTime    bool
	HourReserved  uint8
	Day           uint8
	DayOfWeek     uint8
	Month         uint8
	MonthReserved uint8
	Year          uint8
	YearReserved  bool
}

// MarshalBinary preserves raw fields, rejecting only values wider than the
// wire field. Calendar validation belongs to Time, not ASDU decoding.
func (c CP56Time2a) MarshalBinary() ([]byte, error) {
	if c.Minute > 63 || c.Hour > 31 || c.HourReserved & ^uint8(0x60) != 0 || c.Day > 31 || c.DayOfWeek > 7 || c.Month > 15 || c.MonthReserved & ^uint8(0xf0) != 0 || c.Year > 127 {
		return nil, fmt.Errorf("%w: CP56 field width", ErrMalformed)
	}
	b := make([]byte, 7)
	binary.LittleEndian.PutUint16(b, c.Milliseconds)
	b[2] = c.Minute
	if c.Invalid {
		b[2] |= 0x80
	}
	if c.RES1 {
		b[2] |= 0x40
	}
	b[3] = c.Hour | c.HourReserved
	if c.SummerTime {
		b[3] |= 0x80
	}
	b[4] = c.Day | c.DayOfWeek<<5
	b[5] = c.Month | c.MonthReserved
	b[6] = c.Year
	if c.YearReserved {
		b[6] |= 0x80
	}
	return b, nil
}

// DecodeCP56Time2a requires exactly seven octets; all bit patterns are retained.
func DecodeCP56Time2a(b []byte) (CP56Time2a, error) {
	if len(b) != 7 {
		return CP56Time2a{}, fmt.Errorf("%w: CP56 length", ErrMalformed)
	}
	return CP56Time2a{Milliseconds: binary.LittleEndian.Uint16(b), Minute: b[2] & 63, Invalid: b[2]&0x80 != 0, RES1: b[2]&0x40 != 0, Hour: b[3] & 31, SummerTime: b[3]&0x80 != 0, HourReserved: b[3] & 0x60, Day: b[4] & 31, DayOfWeek: b[4] >> 5, Month: b[5] & 15, MonthReserved: b[5] & 0xf0, Year: b[6] & 127, YearReserved: b[6]&0x80 != 0}, nil
}

// CP56FromTime emits UTC with unspecified weekday, no summer-time flag, and
// year modulo 100. Sub-millisecond precision is truncated.
func CP56FromTime(t time.Time, invalid bool) CP56Time2a {
	t = t.UTC()
	return CP56Time2a{Milliseconds: uint16(t.Second()*1000 + t.Nanosecond()/1e6), Minute: uint8(t.Minute()), Invalid: invalid, Hour: uint8(t.Hour()), Day: uint8(t.Day()), Month: uint8(t.Month()), Year: uint8((t.Year()%100 + 100) % 100)}
}

// Time interprets the calendar as UTC in 2000..2099. False means an out-of-range
// calendar; Invalid, summer time and reserved flags remain independently
// available and are not silently applied as timezone adjustments.
func (c CP56Time2a) Time() (time.Time, bool) {
	if c.Milliseconds >= 60000 || c.Minute > 59 || c.Hour > 23 || c.Day < 1 || c.Day > 31 || c.Month < 1 || c.Month > 12 || c.Year > 99 || c.DayOfWeek > 7 {
		return time.Time{}, false
	}
	t := time.Date(2000+int(c.Year), time.Month(c.Month), int(c.Day), int(c.Hour), int(c.Minute), int(c.Milliseconds)/1000, int(c.Milliseconds%1000)*1e6, time.UTC)
	if t.Day() != int(c.Day) || t.Month() != time.Month(c.Month) {
		return time.Time{}, false
	}
	return t, true
}
