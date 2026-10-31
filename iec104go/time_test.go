// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"testing"
	"time"
)

func TestCP56(t *testing.T) {
	utc := time.Date(2024, 2, 29, 23, 59, 59, 999999999, time.UTC)
	local := utc.In(time.FixedZone("test", -7*3600))
	for _, invalid := range []bool{false, true} {
		c := CP56FromTime(local, invalid)
		if c.Invalid != invalid || c.DayOfWeek != 0 || c.SummerTime || c.Hour != 23 || c.Year != 24 || c.Milliseconds != 59999 {
			t.Fatalf("from time: %+v", c)
		}
		got, ok := c.Time()
		if !ok || !got.Equal(utc.Truncate(time.Millisecond)) {
			t.Fatalf("calendar=%v %v", got, ok)
		}
	}
	for _, wire := range []string{"00000000000000", "ffffffffffffff", "3412c5e6a7b8d9", "5fea3b171d0218"} {
		b := unhex(t, wire)
		c, err := DecodeCP56Time2a(b)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := c.MarshalBinary()
		if err != nil || !bytes.Equal(b, encoded) {
			t.Fatalf("raw flags: %x %v", encoded, err)
		}
	}
	raw, err := DecodeCP56Time2a(unhex(t, "3412c5e6a7b8d9"))
	if err != nil {
		t.Fatal(err)
	}
	if raw.Milliseconds != 0x1234 || raw.Minute != 5 || !raw.RES1 || !raw.Invalid || raw.Hour != 6 || raw.HourReserved != 0x60 || !raw.SummerTime || raw.Day != 7 || raw.DayOfWeek != 5 || raw.Month != 8 || raw.MonthReserved != 0xb0 || raw.Year != 89 || !raw.YearReserved {
		t.Fatalf("bit layout: %+v", raw)
	}
	base := CP56FromTime(utc, false)
	for _, change := range []func(*CP56Time2a){
		func(c *CP56Time2a) { c.Milliseconds = 60000 }, func(c *CP56Time2a) { c.Minute = 60 }, func(c *CP56Time2a) { c.Hour = 24 },
		func(c *CP56Time2a) { c.Day = 0 }, func(c *CP56Time2a) { c.Month = 0 }, func(c *CP56Time2a) { c.Month = 13 },
		func(c *CP56Time2a) { c.Year = 100 }, func(c *CP56Time2a) { c.Year = 23 }, func(c *CP56Time2a) { c.Month = 4; c.Day = 31 },
	} {
		c := base
		change(&c)
		if _, ok := c.Time(); ok {
			t.Fatalf("normalized invalid calendar %+v", c)
		}
	}
	if c := CP56FromTime(time.Date(2107, 1, 1, 0, 0, 0, 0, time.UTC), false); c.Year != 7 {
		t.Fatalf("year modulo: %+v", c)
	}
	for _, c := range []CP56Time2a{{Minute: 64}, {Hour: 32}, {HourReserved: 1}, {Day: 32}, {DayOfWeek: 8}, {Month: 16}, {MonthReserved: 1}, {Year: 128}} {
		if _, err := c.MarshalBinary(); err == nil {
			t.Fatalf("field wrapped: %+v", c)
		}
	}
	for n := 0; n <= 8; n++ {
		if n == 7 {
			continue
		}
		if _, err := DecodeCP56Time2a(make([]byte, n)); err == nil {
			t.Fatalf("length=%d", n)
		}
	}
}

func TestCP16(t *testing.T) {
	for _, value := range []CP16Time2a{0, 1, 0x1234, 65535} {
		b, err := value.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if len(b) != 2 || b[0] != byte(value) || b[1] != byte(value>>8) {
			t.Fatalf("CP16=%x", b)
		}
		decoded, err := DecodeCP16Time2a(b)
		if err != nil || decoded != value {
			t.Fatalf("CP16=%v %v", decoded, err)
		}
	}
	for _, b := range [][]byte{nil, {1}, {1, 2, 3}} {
		if _, err := DecodeCP16Time2a(b); err == nil {
			t.Fatal("CP16 length")
		}
	}
}
