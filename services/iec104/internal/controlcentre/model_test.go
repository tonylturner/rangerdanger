// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

const at = "2026-10-15T12:34:56.789Z"

func model(t *testing.T) (*Centre, *time.Time) {
	t.Helper()
	pm, err := pointmap.Load("../../check/points.test.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 12, 34, 56, 789000000, time.UTC)
	c, err := New(pm, "test:2404", Options{Epoch: "0123456789abcdef", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return c, &now
}
func equal(t *testing.T, got []byte, want string) {
	t.Helper()
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
func receive(c *Centre, typ iec.TypeID, cot iec.COT, ioa int, e iec.Element) {
	c.handle(context.Background(), iec.Event{Kind: iec.ASDUReceived, ASDU: iec.ASDU{Header: iec.Header{Type: typ, Cause: cot, CommonAddress: 1}, Objects: []iec.Object{{Address: uint32(ioa), Element: e}}}})
}
func started(c *Centre) {
	c.handle(context.Background(), iec.Event{Kind: iec.Opened})
	c.handle(context.Background(), iec.Event{Kind: iec.Started})
}
func TestPointGoldens(t *testing.T) {
	// Pin all 14 keys, exact key order/nulls, all four kinds through the full lifecycle.
	cases := []struct {
		index           int
		typ, timed      iec.TypeID
		element         iec.Element
		head, value, ov string
	}{
		{0, 3, 31, iec.DoublePoint{DIQ: 2}, `"device":"feeder-breaker","limits":null,"id":"feeder-breaker-position","ioa":1001,"kind":"double-point"`, `"closed"`, `null`},
		{1, 1, 30, iec.SinglePoint{SIQ: 1}, `"device":"feeder-breaker","limits":null,"id":"feeder-protection-trip","ioa":1002,"kind":"single-point"`, `true`, `null`},
		{18, 5, 32, iec.StepPosition{Value: 3}, `"device":"oltc-1","limits":null,"id":"oltc-1-tap","ioa":1301,"kind":"step-position"`, `{"value":3,"transient":false}`, `false`},
		{2, 13, 0, iec.Float{Value: 28.9}, `"device":"feeder-breaker","limits":null,"id":"feeder-current","ioa":2001,"kind":"float"`, `28.9`, `false`},
	}
	for _, tc := range cases {
		t.Run(tc.typ.String(), func(t *testing.T) {
			c, _ := model(t)
			reporting := "null"
			if tc.typ == 13 {
				reporting = `{"cyclic_ms":2000,"deadband":0.5}`
			}
			prefix := "{" + tc.head + `,"asdu":`
			mid := `,"reporting":` + reporting + `,"value":`
			quality := func(iv, nt bool) string {
				return fmt.Sprintf(`,"quality":{"iv":%t,"nt":%t,"sb":false,"bl":false,"ov":%s}`, iv, nt, tc.ov)
			}
			tail := `,"source_time":null,"source_time_invalid":null,"received_at":null,"cot":null}`
			equal(t, c.pointJSON(tc.index), prefix+"null"+mid+"null"+quality(true, true)+`,"stale_reason":"never_received"`+tail)
			started(c)
			ioa := c.pm.Points[tc.index].IOA
			receive(c, tc.typ, 20, ioa, tc.element)
			normal := prefix + `"` + tc.typ.String() + `"` + mid + tc.value + quality(false, false) + `,"stale_reason":null,"source_time":null,"source_time_invalid":null,"received_at":"` + at + `","cot":20}`
			equal(t, c.pointJSON(tc.index), normal)
			if tc.timed != 0 {
				tag := iec.CP56FromTime(c.opts.Now(), false)
				tag.Invalid = true
				receive(c, tc.timed, 3, ioa, iec.Timed{Value: tc.element, Time: tag})
				equal(t, c.pointJSON(tc.index), prefix+`"`+tc.timed.String()+`"`+mid+tc.value+quality(false, false)+`,"stale_reason":null,"source_time":"`+at+`","source_time_invalid":true,"received_at":"`+at+`","cot":3}`)
				receive(c, tc.typ, 20, ioa, tc.element)
				equal(t, c.pointJSON(tc.index), normal)
			}
			c.mu.Lock()
			c.ended()
			c.mu.Unlock()
			lost := strings.Replace(normal, quality(false, false), quality(true, true), 1)
			lost = strings.Replace(lost, `"stale_reason":null`, `"stale_reason":"link_lost"`, 1)
			equal(t, c.pointJSON(tc.index), lost)
		})
	}
}
func TestFloatGoldens(t *testing.T) {
	for _, tc := range []struct {
		f    float32
		want string
	}{{28.9, "28.9"}, {0, "0"}, {1.234567, "1.234567"}, {math.Float32frombits(0x3f800001), "1.0000001"}, {math.MaxFloat32, "3.4028235e+38"}, {float32(math.Inf(1)), "null"}, {float32(math.NaN()), "null"}} {
		b, err := json.Marshal(floatValue(tc.f))
		if err != nil {
			t.Fatal(err)
		}
		equal(t, b, tc.want)
	}
}
func TestSchemaJournalHealthGoldens(t *testing.T) {
	c, _ := model(t)
	c.pm.Points = nil
	c.points = nil
	equal(t, c.stateJSON(), `{"schema":3,"snapshot_seq":1,"rendered_at":"`+at+`","cache_epoch":"0123456789abcdef","link":{"state":"disconnected","peer":"test:2404","since":"`+at+`","last_rx_at":null,"connection_seq":0,"gi":{"state":"none","started_at":null,"trigger":null,"last_complete_at":null,"next_due_at":null}},"points":[]}`)
	equal(t, c.journalJSON(0), `{"cache_epoch":"0123456789abcdef","entries":[],"latest_seq":null,"oldest_seq":null}`)
	equal(t, c.healthJSON(), `{"status":"ok","link":"disconnected","historian":{"last_ok_at":null,"failures":0,"dropped_snapshots":0}}`)
	c.failures = 2
	c.dropped = 3
	equal(t, c.api(context.Background(), request{method: "GET", path: "/api/health"}).body, `{"status":"ok","link":"disconnected","historian":{"last_ok_at":null,"failures":2,"dropped_snapshots":3}}`)
	c.ended()
	c.ended()
	equal(t, c.journalJSON(0), `{"cache_epoch":"0123456789abcdef","entries":[{"seq":1,"at":"`+at+`","kind":"link_down","detail":{"peer":"test:2404","connection_seq":0,"reason":"connect_failed"}}],"latest_seq":1,"oldest_seq":1}`)
	for i := 0; i < 505; i++ {
		c.add("test", ordered("n", i))
	}
	if len(c.journal) != 500 || c.journal[0].seq != 7 {
		t.Fatal("ring retention")
	}
	var j struct{ Entries []json.RawMessage }
	if err := json.Unmarshal(c.journalJSON(500), &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Entries) != 6 {
		t.Fatal("after filter")
	}
}
func TestModelAssertions(t *testing.T) {
	c, now := model(t)
	post := func(path string) response { return c.api(context.Background(), request{method: "POST", path: path}) }
	if post("/api/gi").status != 503 {
		t.Fatal("down GI")
	}
	started(c)
	if r := c.command(context.Background(), 0, 2, "close"); r.status != 409 {
		t.Fatal("integrity admission")
	}
	start := *now
	if c.tickGI(start) != "connect" || c.tickGI(start.Add(time.Minute)) != "" {
		t.Fatal("connect reservation")
	}
	c.giResponse(10, false)
	complete := c.link.completed
	receive(c, 3, 20, 1001, iec.DoublePoint{DIQ: 2})
	receive(c, 13, 20, 2001, iec.Float{Value: 28.9})
	p := c.points[2]
	c.watchdog(p.at.Add(6 * time.Second))
	if c.points[2].stale != "" {
		t.Fatal("inclusive boundary")
	}
	c.watchdog(p.at.Add(6*time.Second + time.Millisecond))
	if c.points[2].stale != "not_updated" || c.points[2].q != iec.NotTopical || c.points[2].value.(json.RawMessage)[0] != '2' {
		t.Fatal("watchdog retention")
	}
	if c.points[0].stale != "" {
		t.Fatal("quiet status")
	}
	receive(c, 13, 1, 2001, iec.Float{Value: 28.9})
	receive(c, 13, 3, 2001, iec.Float{Value: 28.9, Quality: iec.Invalid})
	c.watchdog(start.Add(time.Hour))
	if c.points[2].stale != "measurement_invalid" {
		t.Fatal("reason precedence")
	}
	c.points[0].at = time.Time{}
	if c.command(context.Background(), 0, 2, "close").status != 200 {
		t.Fatal("quiet GOOD admission")
	}
	for _, q := range []iec.Quality{iec.Invalid, iec.NotTopical} {
		c.points[0].q = q
		if c.command(context.Background(), 0, 2, "close").status != 409 {
			t.Fatal("stale admitted")
		}
	}
	c.points[0].q = 0
	if c.tickGI(start.Add(59999*time.Millisecond)) != "" || c.tickGI(start.Add(time.Minute)) != "periodic" || c.link.completed != complete {
		t.Fatal("periodic boundary / prior integrity")
	}
	if c.command(context.Background(), 0, 2, "close").status != 200 || post("/api/gi").status != 409 {
		t.Fatal("GI running admission")
	}
	c.giResponse(10, false)
	results := make(chan response, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- post("/api/gi") }()
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for r := range results {
		counts[r.status]++
	}
	if counts[202] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	if c.tickGI(start.Add(60001*time.Millisecond)) != "operator" || !c.link.next.Equal(start.Add(120001*time.Millisecond)) {
		t.Fatal("operator interval")
	}
	c.points[0].q = iec.Substituted
	c.giResponse(10, false)
	equal(t, c.journal[len(c.journal)-1].detail, `{"connection_seq":1,"points_good":0,"trigger":"operator"}`)
	c.points[0].q = 0
	c.tracker.active = true
	if post("/api/reconnect").status != 409 {
		t.Fatal("command exclusion")
	}
	c.tracker.active = false
	if post("/api/reconnect").status != 202 || post("/api/reconnect").status != 409 || c.command(context.Background(), 0, 2, "close").status != 409 {
		t.Fatal("reconnect reservation")
	}
	c.link.reconnect = false
	before := c.journalSeq
	tag := iec.CP56FromTime(*now, false)
	receive(c, 31, 3, 1001, iec.Timed{Value: iec.DoublePoint{DIQ: 1}, Time: tag})
	receive(c, 31, 11, 1001, iec.Timed{Value: iec.DoublePoint{DIQ: 1}, Time: tag})
	if c.journalSeq != before+1 {
		t.Fatal("status duplicate")
	}
	equal(t, c.journal[len(c.journal)-1].detail, `{"id":"feeder-breaker-position","ioa":1001,"asdu":"M_DP_TB_1","cot":3,"value":"open","source_time":"`+at+`","source_time_invalid":false}`)
	c.link.lastAt = *now
	c.ended()
	if c.link.completed != "" || c.link.trigger != "" || c.link.pending != "" {
		t.Fatal("loss reset")
	}
	c.watchdog(now.Add(time.Hour))
	if c.points[2].stale != "link_lost" {
		t.Fatal("loss precedence")
	}
}
func TestJSONStringsAndLimits(t *testing.T) {
	equal(t, ordered("text", "<>&\u2028\u2029\n\t\b\f\r\x01\"\\"), "{\"text\":\"<>&\u2028\u2029\\n\\t\\b\\f\\r\\u0001\\\"\\\\\"}")
	c, _ := model(t)
	equal(t, c.pointJSON(3), `{"device":"feeder-breaker","limits":{"low":19,"high":21},"id":"feeder-voltage","ioa":2002,"kind":"float","asdu":null,"reporting":{"cyclic_ms":2000,"deadband":0.05},"value":null,"quality":{"iv":true,"nt":true,"sb":false,"bl":false,"ov":false},"stale_reason":"never_received","source_time":null,"source_time_invalid":null,"received_at":null,"cot":null}`)
}
func TestLateFeedbackCannotComplete(t *testing.T) {
	c, now := model(t)
	c.tracker = tracker{active: true, index: 0, phase: "execute", executeCon: 1, terminated: true, expected: 2, deadline: now.Add(time.Second)}
	receive(c, 3, 11, 1001, iec.DoublePoint{DIQ: 2})
	if c.tracker.feedback {
		t.Fatal("feedback after ACT-TERM accepted")
	}
}

func TestAll42PointsQualityAndRecovery(t *testing.T) {
	c, now := model(t)
	started(c)
	c.tickGI(*now)
	for _, p := range c.pm.Points {
		typ, e := element(p)
		receive(c, typ, 20, p.IOA, e)
	}
	c.giResponse(10, false)
	equal(t, c.journal[len(c.journal)-1].detail, `{"connection_seq":1,"points_good":42,"trigger":"connect"}`)
	if c.journal[0].kind != "link_up" || c.journal[1].kind != "gi_started" {
		t.Fatal("startup ordering")
	}
	for i, p := range c.pm.Points {
		typ, e := element(p)
		switch v := e.(type) {
		case iec.DoublePoint:
			v.DIQ |= 0xc0
			e = v
		case iec.SinglePoint:
			v.SIQ |= 0xc0
			e = v
		case iec.StepPosition:
			v.Quality = iec.Invalid | iec.NotTopical
			e = v
		case iec.Float:
			v.Quality = iec.Invalid | iec.NotTopical
			e = v
		}
		receive(c, typ, 3, p.IOA, e)
		if c.points[i].stale != "device_unreachable" || c.points[i].q != iec.Invalid|iec.NotTopical || !c.points[i].has {
			t.Fatal("device loss", p.ID)
		}
		typ, e = element(p)
		receive(c, typ, 1, p.IOA, e)
	}
	for i, p := range c.pm.Points {
		if p.Kind != pointmap.Float {
			continue
		}
		receive(c, 13, 3, p.IOA, iec.Float{Value: 28.9, Quality: iec.Invalid})
		want := "measurement_invalid"
		if c.pm.Devices[p.DeviceIndex].HasHeartbeat {
			want = "heartbeat"
		}
		if c.points[i].stale != want || c.points[i].q != iec.Invalid {
			t.Fatal(p.ID, c.points[i].stale)
		}
	}
	if c.points[0].stale != "" || c.points[1].stale != "" || c.link.state != "started" {
		t.Fatal("measurement invalid changed status/link")
	}
	for _, p := range c.pm.Points {
		typ, e := element(p)
		receive(c, typ, 1, p.IOA, e)
	}
	before := append([]cached(nil), c.points...)
	seq := c.journalSeq
	c.link.lastAt = *now
	c.ended()
	equal(t, c.journal[len(c.journal)-43].detail, `{"peer":"test:2404","connection_seq":1,"reason":"peer_closed"}`)
	for i, p := range c.points {
		if p.stale != "link_lost" || p.q != iec.Invalid|iec.NotTopical || p.received != before[i].received || p.source != before[i].source || p.typ != before[i].typ || p.cot != before[i].cot {
			t.Fatal("loss discarded truth")
		}
	}
	if c.journalSeq != seq+43 {
		t.Fatal("one down then one stale per point required")
	}
	seq = c.journalSeq
	c.ended()
	if c.journalSeq != seq {
		t.Fatal("duplicate disconnect evidence")
	}
	*now = now.Add(time.Second)
	started(c)
	c.tickGI(*now)
	for _, p := range c.pm.Points {
		typ, e := element(p)
		receive(c, typ, 20, p.IOA, e)
	}
	c.giResponse(10, false)
	equal(t, c.journal[len(c.journal)-1].detail, `{"connection_seq":2,"points_good":42,"trigger":"connect"}`)
	equal(t, c.api(context.Background(), request{method: "POST", path: "/api/gi"}).body, `{"trigger":"operator","requested_at":"2026-10-15T12:34:57.789Z"}`)
}
