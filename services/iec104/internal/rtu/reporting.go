// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"errors"
	"math"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/logfmt"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func pointQuality(s pointState, p pointmap.Point) iec.Quality {
	var q iec.Quality
	if s.unreachable {
		q |= iec.Invalid | iec.NotTopical
	}
	if s.valueInvalid || s.heartbeatStale || (p.Measurement && s.measurementInvalid) {
		q |= iec.Invalid
	}
	return q
}

func (r *RTU) element(i int, timed bool) (iec.TypeID, iec.Element, error) {
	p, s := r.pm.Points[i], r.points[i]
	q := pointQuality(s, p)
	var typ iec.TypeID
	var e iec.Element
	switch p.Kind {
	case pointmap.SinglePoint:
		typ = iec.M_SP_NA_1
		v := byte(q)
		if s.sp {
			v |= 1
		}
		e = iec.SinglePoint{SIQ: iec.SIQ(v)}
	case pointmap.DoublePoint:
		typ, e = iec.M_DP_NA_1, iec.DoublePoint{DIQ: iec.DIQ(byte(q) | s.dp)}
	case pointmap.StepPosition:
		v, err := iec.NewVTI(int8(s.step), s.transient)
		if err != nil {
			return 0, nil, err
		}
		typ, e = iec.M_ST_NA_1, iec.StepPosition{Value: v, Quality: q}
	case pointmap.Float:
		return iec.M_ME_NC_1, iec.Float{Value: s.f, Quality: q}, nil
	}
	if timed {
		utc, synced := r.opts.Clock.Now()
		e = iec.Timed{Value: e, Time: iec.CP56FromTime(utc, !synced)}
		switch typ {
		case iec.M_SP_NA_1:
			typ = iec.M_SP_TB_1
		case iec.M_DP_NA_1:
			typ = iec.M_DP_TB_1
		case iec.M_ST_NA_1:
			typ = iec.M_ST_TB_1
		}
	}
	return typ, e, nil
}

func (r *RTU) logASDU(direction string, m iec.Message) {
	lines, err := logfmt.ASDULines(direction, m)
	if err != nil {
		r.printf("ASDU log error: %v", err)
		return
	}
	for _, line := range lines {
		r.printf("%s", line)
	}
}

func (r *RTU) send(conn connection, m iec.Message) {
	if err := conn.Send(m); err != nil {
		r.sendFailed.Add(1)
		h := m.GetHeader()
		_, peer := endpointText(conn.RemoteAddr())
		r.printf("send failed conn=%d peer=%s type=%d cot=%d error=%v", conn.ID(), peer, h.Type, h.Cause, err)
	}
}

// The started registry is protected through every admission, including
// lifecycle callbacks. One failed recipient never closes or starves another.
func (r *RTU) broadcast(m iec.Message) {
	r.logASDU("tx-all", m)
	for _, p := range r.started {
		r.send(p.conn, m)
	}
}

func (r *RTU) reportPoint(i int, cot iec.COT) {
	typ, e, err := r.element(i, true)
	if err != nil {
		r.buildError(err)
		return
	}
	m := iec.ASDU{Header: iec.Header{Type: typ, Cause: cot, CommonAddress: uint16(r.pm.CA)},
		Objects: []iec.Object{{Address: uint32(r.pm.Points[i].IOA), Element: e}}}
	r.broadcast(m)
	s := &r.points[i]
	s.repQuality = pointQuality(*s, r.pm.Points[i])
	s.repDP, s.repSP, s.repF = s.dp, s.sp, s.f
	s.repStep, s.repTransient = s.step, s.transient
}

func (r *RTU) evaluate(now int64) {
	for d, dev := range r.pm.Devices {
		s := &r.devices[d]
		s.unreachable = s.lastOK == 0 || now-s.lastOK > int64(dev.StaleAfterMS)
	}
	for i, p := range r.pm.Points {
		s := &r.points[i]
		d, dev := r.devices[p.DeviceIndex], r.pm.Devices[p.DeviceIndex]
		s.unreachable = d.unreachable
		s.heartbeatStale = dev.HasHeartbeat && (d.heartbeatChanged == 0 || now-d.heartbeatChanged >= int64(dev.HeartbeatStaleMS))
		qChanged := pointQuality(*s, p) != s.repQuality
		report := qChanged
		switch p.Kind {
		case pointmap.SinglePoint:
			report = report || s.sp != s.repSP
		case pointmap.DoublePoint:
			report = report || s.dp != s.repDP
		case pointmap.StepPosition:
			report = report || s.step != s.repStep || s.transient != s.repTransient
		case pointmap.Float:
			report = report || (now-s.lastValueEvent >= 1000 && s.f != s.repF && math.Abs(float64(s.f)-float64(s.repF)) >= p.Deadband)
		}
		if report {
			r.reportPoint(i, iec.Spontaneous)
			if p.Kind == pointmap.Float && !qChanged {
				s.lastValueEvent = now
			}
		}
	}
	r.buildError(r.reportImage(nil, 0, true, now))
}

func (r *RTU) reportImage(conn connection, oa uint8, cyclic bool, now int64) error {
	for _, kind := range []pointmap.Kind{pointmap.SinglePoint, pointmap.DoublePoint, pointmap.StepPosition, pointmap.Float} {
		var b *iec.Builder
		flush := func() {
			if b != nil {
				if conn == nil {
					r.broadcast(b.ASDU())
				} else {
					r.logASDU("tx-gi", b.ASDU())
					r.send(conn, b.ASDU())
				}
			}
		}
		for i, p := range r.pm.Points {
			if p.Kind != kind || (cyclic && kind != pointmap.Float) {
				continue
			}
			s := &r.points[i]
			if cyclic {
				if s.nextCyclic == 0 {
					s.nextCyclic = now + int64(p.CyclicMS)
				}
				if now < s.nextCyclic {
					continue
				}
			}
			typ, e, err := r.element(i, false)
			if err != nil {
				return err
			}
			h := iec.Header{Type: typ, Cause: iec.InterrogatedStation, Originator: oa, CommonAddress: uint16(r.pm.CA)}
			if cyclic {
				h.Cause = iec.Periodic
			}
			if b == nil {
				b, err = iec.NewBuilder(iec.DefaultParams(), h)
				if err != nil {
					return err
				}
			}
			o := iec.Object{Address: uint32(p.IOA), Element: e}
			if err = b.Append(o); errors.Is(err, iec.ErrFull) {
				flush()
				b, err = iec.NewBuilder(iec.DefaultParams(), h)
				if err == nil {
					err = b.Append(o)
				}
			}
			if err != nil {
				return err
			}
			if cyclic {
				s.repF, s.repQuality = s.f, pointQuality(*s, p)
				s.nextCyclic = now + int64(p.CyclicMS)
			}
		}
		flush()
	}
	return nil
}
