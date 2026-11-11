// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"context"
	"net"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

// A thaw can report all 44 points stale and then GOOD before the peer drains
// its k window. The last status reports have no cyclic refresh: advancing the
// shared baseline after a failed admission strands that peer until its next GI.
func TestReportingOverflowReconnectsForFreshGI(t *testing.T) {
	r, now, _ := newModel(t, 44)
	for i := range r.pm.Points {
		r.pm.Points[i].Kind = pointmap.SinglePoint
	}
	r.started = nil
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	newSession := func() (*iec.Session, net.Conn) {
		t.Helper()
		conn, peer := net.Pipe()
		s, err := iec.NewSession(ctx, conn, iec.ServerRole, iec.DefaultOptions(), r.handle)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			peer.Close()
			closeCtx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if err := s.Close(closeCtx); err != nil {
				t.Error(err)
			}
		})
		if err := peer.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		return s, peer
	}
	write := func(peer net.Conn, f iec.Frame) {
		t.Helper()
		b, err := f.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := peer.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	read := func(peer net.Conn) iec.Frame {
		t.Helper()
		f, err := iec.NewAPDUReader(peer).ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	start := func(s *iec.Session, peer net.Conn) {
		t.Helper()
		write(peer, iec.Frame{Kind: iec.UFrame, Function: iec.StartDTAct})
		if f := read(peer); f.Kind != iec.UFrame || f.Function != iec.StartDTCon {
			t.Fatalf("STARTDT: %+v", f)
		}
		deadline := time.Now().Add(time.Second)
		for {
			r.mu.Lock()
			registered := r.connection(s.ID()) != nil
			r.mu.Unlock()
			if registered {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("STARTDT callback not delivered")
			}
			time.Sleep(time.Millisecond)
		}
	}
	s, peer := newSession()
	start(s, peer)

	// Fill the real default k window, reading frames but withholding S ACKs.
	// No sleeps or scheduler assumptions decide whether the send queue fills.
	r.mu.Lock()
	for i := 0; i < iec.DefaultAPCI().K; i++ {
		r.reportPoint(0, iec.Spontaneous)
	}
	r.mu.Unlock()
	for i := 0; i < iec.DefaultAPCI().K; i++ {
		if f := read(peer); f.Kind != iec.IFrame || int(f.Send) != i {
			t.Fatalf("window frame %d: %+v", i, f)
		}
	}

	r.mu.Lock()
	*now += 4000
	r.evaluate(*now) // 44 IV+NT reports
	r.devices[0].lastOK = *now
	r.evaluate(*now) // 44 GOOD reports; 88 > the default 64-send queue
	failures := r.Counters().SendFailures
	baseline := r.points[len(r.points)-1].repQuality
	r.mu.Unlock()
	if failures == 0 || baseline != 0 {
		t.Fatalf("overflow not exercised: failures=%d last baseline=%v", failures, baseline)
	}
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("lost status report left the session alive with an advanced baseline")
	}
	r.mu.Lock()
	retained := len(r.started)
	r.mu.Unlock()
	if retained != 0 {
		t.Fatal("overflowed recipient not retired")
	}

	// The replacement session's GI must restore every point, including the
	// tail status reports rejected by the old session's admission queue.
	fresh, peer := newSession()
	start(fresh, peer)
	gi := iec.ASDU{Header: iec.Header{Type: iec.C_IC_NA_1, Cause: iec.Activation, CommonAddress: 1},
		Objects: []iec.Object{{Element: iec.Interrogation{QOI: 20}}}}
	b, err := gi.MarshalBinary(iec.DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	write(peer, iec.Frame{Kind: iec.IFrame, ASDU: b})
	seen := make(map[uint32]bool)
	var cots []iec.COT
	for {
		f := read(peer)
		if f.Kind != iec.IFrame {
			t.Fatalf("unexpected GI frame: %+v", f)
		}
		m, err := iec.DecodeASDU(f.ASDU, iec.DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		cot := m.GetHeader().Cause
		cots = append(cots, cot)
		if cot == iec.InterrogatedStation {
			for _, o := range m.(*iec.ASDU).Objects {
				if seen[o.Address] || o.Element.(iec.SinglePoint).SIQ.Quality() != 0 {
					t.Fatalf("duplicate or stale GI point: %+v", o)
				}
				seen[o.Address] = true
			}
		}
		if cot == iec.ActivationTerm {
			break
		}
	}
	if len(seen) != 44 || len(cots) != 3 || cots[0] != iec.ActivationCon || cots[1] != iec.InterrogatedStation {
		t.Fatalf("incomplete recovery: points=%d cots=%v", len(seen), cots)
	}
}
