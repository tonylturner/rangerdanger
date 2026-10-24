// SPDX-License-Identifier: Apache-2.0

// Package controlcentre implements the lab's persistent controlling station.
package controlcentre

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

// Logger records complete evidence lines.
type Logger interface{ Printf(string, ...any) error }

// Options supplies deterministic time and transport seams. Callbacks must be concurrency safe.
type Options struct {
	Now    func() time.Time
	Epoch  string
	Logger Logger
	Dial   DialFunc
}
type cached struct {
	has                     bool
	value                   any
	actual                  int
	q                       iec.Quality
	transient               bool
	stale, source, received string
	invalid                 bool
	at                      time.Time
	typ                     iec.TypeID
	cot                     iec.COT
}
type link struct {
	state, since, lastRx                             string
	lastAt                                           time.Time
	seq                                              int
	gi, started, completed, trigger, pending, nextAt string
	start, next                                      time.Time
	failed, reconnect, down, shutdown                bool
	reason                                           string
}
type entry struct {
	seq      uint64
	at, kind string
	detail   json.RawMessage
}
type tracker struct {
	active                                    bool
	index, expected, seq                      int
	phase                                     string
	deadline                                  time.Time
	selectCon, executeCon                     int
	selected, executed, con, term, feedbackAt string
	terminated, negative, feedback            bool
	feedbackValue                             any
}

// Centre owns the cache, journal, and single command transaction.
type Centre struct {
	mu                                  sync.Mutex
	changed                             chan struct{}
	pm                                  *pointmap.Map
	peer, target, epoch                 string
	opts                                Options
	points                              []cached
	link                                link
	tracker                             tracker
	journal                             []entry
	journalSeq, snapshotSeq, commandSeq uint64
	session                             Transport
	sendFailures, logFailures           atomic.Uint64
	historian                           bool
	queue                               []json.RawMessage
	lastOK                              string
	failures, dropped                   uint64
}

// New constructs a station from an immutable validated map.
func New(pm *pointmap.Map, peer string, opts Options) (*Centre, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Dial == nil {
		opts.Dial = dial
	}
	if opts.Epoch == "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("cannot read /dev/urandom for the cache epoch: %w", err)
		}
		opts.Epoch = hex.EncodeToString(b)
	}
	c := &Centre{pm: pm, peer: truncate(peer, 63), target: peer, epoch: opts.Epoch, opts: opts, changed: make(chan struct{}), points: make([]cached, len(pm.Points))}
	c.link = link{state: "disconnected", since: c.stamp(), gi: "none"}
	for i := range c.points {
		c.points[i] = cached{q: iec.Invalid | iec.NotTopical, stale: "never_received"}
	}
	return c, nil
}

// Epoch is the immutable per-boot cache identity.
func (c *Centre) Epoch() string { return c.epoch }
func stamp(t time.Time) string  { return t.UTC().Format(time.RFC3339Nano) }
func (c *Centre) stamp() string { return stamp(c.opts.Now()) }
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ordered preserves the insertion-order compact JSON of the C API. Values are
// application-owned JSON types; wire floats are sanitized before reaching here.
func ordered(kv ...any) json.RawMessage {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < len(kv); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(jsonValue(kv[i].(string)))
		b.WriteByte(':')
		b.Write(jsonValue(kv[i+1]))
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

// cJSON leaves UTF-8 and HTML characters literal, unlike encoding/json.
func jsonValue(value any) []byte {
	switch v := value.(type) {
	case json.RawMessage:
		return v
	case []json.RawMessage:
		var b strings.Builder
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(item)
		}
		b.WriteByte(']')
		return []byte(b.String())
	case string:
		var b strings.Builder
		b.WriteByte('"')
		for _, ch := range []byte(v) {
			switch ch {
			case '"', '\\':
				b.WriteByte('\\')
				b.WriteByte(ch)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				if ch < 32 {
					fmt.Fprintf(&b, `\u%04x`, ch)
				} else {
					b.WriteByte(ch)
				}
			}
		}
		b.WriteByte('"')
		return []byte(b.String())
	default:
		b, err := json.Marshal(value)
		if err != nil {
			panic(err)
		}
		return b
	}
}
func floatValue(f float32) any {
	if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
		return nil
	}
	for p := 6; p <= 9; p++ {
		s := strconv.FormatFloat(float64(f), 'g', p, 64)
		v, err := strconv.ParseFloat(s, 32)
		if err == nil && float32(v) == f {
			return json.RawMessage(s)
		}
	}
	panic("float32 round trip")
}
func (c *Centre) printf(f string, a ...any) {
	if c.opts.Logger != nil {
		if err := c.opts.Logger.Printf(f, a...); err != nil {
			c.logFailures.Add(1)
		}
	}
}

// Counters returns failed send admissions and evidence writes.
func (c *Centre) Counters() (uint64, uint64) { return c.sendFailures.Load(), c.logFailures.Load() }
func (c *Centre) notify()                    { close(c.changed); c.changed = make(chan struct{}) }
func (c *Centre) add(kind string, detail json.RawMessage) {
	c.journalSeq++
	e := entry{c.journalSeq, c.stamp(), kind, detail}
	if len(c.journal) == 500 {
		copy(c.journal, c.journal[1:])
		c.journal[499] = e
	} else {
		c.journal = append(c.journal, e)
	}
	c.printf("journal seq=%d kind=%s detail=%s", e.seq, kind, detail)
}
func (c *Centre) pointEvent(i int) {
	p := c.points[i]
	if p.stale == "" {
		c.add("point_good", ordered("point", c.pm.Points[i].ID, "value", p.value))
	} else {
		c.add("point_stale", ordered("point", c.pm.Points[i].ID, "stale_reason", p.stale, "value", p.value))
	}
}
func (c *Centre) linkDetail() json.RawMessage {
	return ordered("peer", c.peer, "connection_seq", c.link.seq)
}
func (c *Centre) ended() {
	if !c.link.shutdown {
		if c.link.state == "started" || !c.link.down {
			reason := c.link.reason
			if c.link.state != "started" {
				reason = "connect_failed"
			} else if reason == "" {
				reason = "peer_closed"
				if c.link.lastAt.IsZero() || c.opts.Now().Sub(c.link.lastAt) >= time.Duration(c.pm.Timers.T1)*time.Second {
					reason = "t1_timeout"
				}
			}
			c.add("link_down", ordered("peer", c.peer, "connection_seq", c.link.seq, "reason", reason))
			c.link.down = true
		}
		if c.link.state == "started" {
			for i := range c.points {
				p := &c.points[i]
				p.q |= iec.Invalid | iec.NotTopical
				if p.has && p.stale != "link_lost" {
					p.stale = "link_lost"
					c.pointEvent(i)
				}
			}
		}
	}
	c.link.state = "disconnected"
	c.link.since = c.stamp()
	c.link.gi = "none"
	c.link.started = ""
	c.link.completed = ""
	c.link.trigger = ""
	c.link.pending = ""
	c.link.nextAt = ""
	c.link.next = time.Time{}
	c.notify()
}
func (c *Centre) tickGI(now time.Time) string {
	if c.link.state != "started" || c.link.gi == "running" {
		return ""
	}
	trigger := c.link.pending
	if trigger == "" && !c.link.next.IsZero() && !now.Before(c.link.next) {
		trigger = "periodic"
	}
	if trigger == "" {
		return ""
	}
	c.link.pending = ""
	c.link.gi = "running"
	c.link.failed = false
	c.link.trigger = trigger
	c.link.start = now
	c.link.next = now.Add(time.Duration(c.pm.GIIntervalMS) * time.Millisecond)
	c.link.started = c.stamp()
	c.link.nextAt = stamp(c.opts.Now().Truncate(time.Millisecond).Add(time.Duration(c.pm.GIIntervalMS) * time.Millisecond))
	c.add("gi_started", ordered("connection_seq", c.link.seq, "trigger", trigger))
	return trigger
}
func (c *Centre) watchdog(now time.Time) {
	if c.link.state != "started" {
		return
	}
	for i := range c.points {
		p := &c.points[i]
		pp := c.pm.Points[i]
		if pp.Kind == pointmap.Float && p.has && p.stale == "" && p.q == 0 && now.Sub(p.at) > time.Duration(3*pp.CyclicMS)*time.Millisecond {
			p.q |= iec.NotTopical
			p.stale = "not_updated"
			c.pointEvent(i)
		}
	}
}
func (c *Centre) giResponse(cot iec.COT, negative bool) {
	if c.link.gi != "running" {
		return
	}
	if cot == 10 && !negative {
		c.link.gi = "complete"
		c.link.completed = c.stamp()
		n := 0
		for _, p := range c.points {
			if p.has && p.stale == "" && p.q == 0 {
				n++
			}
		}
		c.add("gi_complete", ordered("connection_seq", c.link.seq, "points_good", n, "trigger", c.link.trigger))
	} else if cot != 7 || negative {
		c.link.failed = true
	}
	c.notify()
}
