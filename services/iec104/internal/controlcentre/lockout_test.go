// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"fmt"
	"strings"
	"testing"

	iec "github.com/tonylturner/iec104go"
)

func TestLockoutStateAndJournal(t *testing.T) {
	for _, tc := range []struct {
		id  string
		ioa int
	}{{"feeder-lockout", 1003}, {"f2-lockout", 1503}} {
		t.Run(tc.id, func(t *testing.T) {
			c, now := model(t)
			started(c)
			index := -1
			for i, p := range c.pm.Points {
				if p.ID == tc.id {
					index = i
				}
			}
			if index < 0 {
				t.Fatal("lockout missing")
			}
			receive(c, iec.M_SP_NA_1, iec.InterrogatedStation, tc.ioa, iec.SinglePoint{})
			for _, value := range []bool{true, false} {
				siq := iec.SIQ(0)
				if value {
					siq = 1
				}
				receive(c, iec.M_SP_TB_1, iec.Spontaneous, tc.ioa,
					iec.Timed{Value: iec.SinglePoint{SIQ: siq}, Time: iec.CP56FromTime(*now, false)})
				if body := string(c.pointJSON(index)); !strings.Contains(body, fmt.Sprintf(`"value":%t`, value)) ||
					!strings.Contains(body, `"stale_reason":null`) {
					t.Fatal(body)
				}
				last := c.journal[len(c.journal)-1]
				if last.kind != "status_event" {
					t.Fatal(last.kind)
				}
				equal(t, last.detail, fmt.Sprintf(`{"id":%q,"ioa":%d,"asdu":"M_SP_TB_1","cot":3,"value":%t,"source_time":%q,"source_time_invalid":false}`, tc.id, tc.ioa, value, at))
			}
		})
	}
}
