// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
)

func TestMonitorGoldens(t *testing.T) {
	for _, linkLoss := range []bool{false, true} {
		t.Run(fmt.Sprint(linkLoss), func(t *testing.T) {
			out, errout := newCapture(), newCapture()
			addr := peer(t, func(ctx context.Context, e iec.Event) {
				if e.Kind != iec.ASDUReceived {
					return
				}
				a := e.ASDU.(*iec.ASDU)
				send(t, e.Session, echo(a, iec.ActivationCon, false))
				send(t, e.Session, &iec.ASDU{
					Header: iec.Header{Type: iec.M_DP_TB_1, Cause: iec.ReturnRemote, CommonAddress: 1},
					Objects: []iec.Object{{Address: 1001, Element: iec.Timed{
						Value: iec.DoublePoint{DIQ: 2},
						Time:  iec.CP56Time2a{Year: 26, Month: 10, Day: 23, Hour: 12, Minute: 34, Milliseconds: 56789, Invalid: true},
					}}},
				})
				send(t, e.Session, echo(a, iec.ActivationTerm, false))
				if linkLoss {
					waitCtx, cancel := context.WithTimeout(ctx, time.Second)
					defer cancel()
					if !out.await(waitCtx, `"event":"gi_complete"`) {
						t.Error("initial GI not consumed")
						return
					}
					e.Session.RequestClose()
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			code := run(ctx, []string{addr, "monitor", "--hold", "1", "--json"}, out, errout, testRuntime())
			want := fmt.Sprintf("{\"event\":\"connected\",\"at\":\"%s\",\"local\":null,\"peer\":\"%s\"}\n", stamp, addr) +
				`{"event":"asdu","at":"` + stamp + `","type":31,"cot":11,"ioa":1001,"value":"closed","quality":"GOOD","time":"2026-10-23T12:34:56.789Z(IV)"}` + "\n" +
				`{"event":"gi_complete","at":"` + stamp + `"}` + "\n"
			wantErr := logs("connecting to "+addr+" oa=1 ca=1", "link opened", "link startdt-con", "link started", "send station GI",
				"rx ASDU type=C_IC_NA_1(100) cot=actcon(7) neg=0 test=0 oa=1 ca=1 objects=1", "rx   ioa=0 qoi=20",
				"rx ASDU type=M_DP_TB_1(31) cot=retrem(11) neg=0 test=0 oa=0 ca=1 objects=1", "rx   ioa=1001 value=ON q=GOOD time=2026-10-23T12:34:56.789Z(IV)",
				"rx ASDU type=C_IC_NA_1(100) cot=actterm(10) neg=0 test=0 oa=1 ca=1 objects=1", "rx   ioa=0 qoi=20")
			wantCode := 0
			if linkLoss {
				wantCode = 3
				want += `{"event":"link_down","at":"` + stamp + `","reason":"peer_closed"}` + "\n"
				wantErr += logs("link closed", "link down during monitor hold")
			} else {
				want += `{"event":"held","at":"` + stamp + `"}` + "\n"
				wantErr += logs("hold complete", "link closed")
			}
			if code != wantCode || out.String() != want || errout.String() != wantErr {
				t.Fatalf("exit=%d want=%d\nstdout:\n%s\nwant:\n%s\nstderr:\n%s\nwant:\n%s", code, wantCode, out.String(), want, errout.String(), wantErr)
			}
		})
	}
}

func TestMonitorPeriodicGIAfterTimeoutSameLink(t *testing.T) {
	out, errout := newCapture(), newCapture()
	var mu sync.Mutex
	var sessions []uint64
	count := 0
	addr := peer(t, func(_ context.Context, e iec.Event) {
		if e.Kind != iec.ASDUReceived {
			return
		}
		mu.Lock()
		count++
		n := count
		sessions = append(sessions, e.ConnID)
		mu.Unlock()
		if n == 1 {
			return // timeout must not put the session in procedure-uncertain state
		}
		a := e.ASDU.(*iec.ASDU)
		// Like the C monitor, require only ACT-TERM, not ACT-CON.
		send(t, e.Session, echo(a, iec.ActivationTerm, false))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	code := run(ctx, []string{addr, "monitor", "--hold", "2", "--gi-every", "1", "--json"}, out, errout, testRuntime())
	mu.Lock()
	defer mu.Unlock()
	if count < 2 || sessions[0] != sessions[1] {
		t.Fatalf("GI requests=%d sessions=%v", count, sessions)
	}
	want := fmt.Sprintf("{\"event\":\"connected\",\"at\":\"%s\",\"local\":null,\"peer\":\"%s\"}\n", stamp, addr) +
		`{"event":"gi_complete","at":"` + stamp + `"}` + "\n" +
		`{"event":"held","at":"` + stamp + `"}` + "\n"
	wantErr := logs("connecting to "+addr+" oa=1 ca=1", "link opened", "link startdt-con", "link started", "send station GI",
		"rx ASDU type=C_IC_NA_1(100) cot=actterm(10) neg=0 test=0 oa=1 ca=1 objects=1", "rx   ioa=0 qoi=20", "hold complete", "link closed")
	if code != 0 || out.String() != want || errout.String() != wantErr {
		t.Fatalf("exit=%d stdout=%q want=%q stderr=%q want=%q", code, out.String(), want, errout.String(), wantErr)
	}
}

func TestResponseEnvelopeMatching(t *testing.T) {
	for _, term := range []bool{false, true} {
		for _, field := range []string{"type", "cot", "ca", "ioa"} {
			t.Run(fmt.Sprintf("term=%t/%s", term, field), func(t *testing.T) {
				out, errout := newCapture(), newCapture()
				addr := peer(t, func(ctx context.Context, e iec.Event) {
					if e.Kind != iec.ASDUReceived {
						return
					}
					a := e.ASDU.(*iec.ASDU)
					cause := iec.ActivationCon
					if term {
						send(t, e.Session, echo(a, iec.ActivationCon, false))
						waitCtx, cancel := context.WithTimeout(ctx, time.Second)
						defer cancel()
						if !errout.await(waitCtx, "execute confirmed\n") {
							t.Error("confirmation not consumed")
							return
						}
						cause = iec.ActivationTerm
					}
					bad := echo(a, cause, false)
					switch field {
					case "type":
						bad.Header.Type = iec.C_SC_NA_1
						bad.Objects[0].Element = iec.SingleCommand{Qualifier: 1}
					case "cot":
						bad.Header.Cause = iec.Spontaneous
					case "ca":
						bad.Header.CommonAddress++
					case "ioa":
						bad.Objects[0].Address++
					}
					send(t, e.Session, bad)
				})
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				code := run(ctx, []string{addr, "dc", "3001", "open", "--json"}, out, errout, testRuntime())
				needle := "no execute confirmation"
				if term {
					needle = "no execute termination (ACT-TERM)"
				}
				if code != 1 || out.String() != result("dc", "failed") || !strings.Contains(errout.String(), needle) {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errout.String())
				}
			})
		}
	}
}

func TestMonitoringBurst(t *testing.T) {
	out, errout := newCapture(), newCapture()
	addr := peer(t, func(ctx context.Context, e iec.Event) {
		if e.Kind != iec.ASDUReceived {
			return
		}
		// Admit all 80 timed/untimed pairs without application-level pacing.
		for i := 0; i < 160; i++ {
			typ := iec.M_SP_NA_1
			var element iec.Element = iec.SinglePoint{SIQ: 1}
			if i%2 == 0 {
				typ = iec.M_SP_TB_1
				element = iec.Timed{
					Value: element,
					Time:  iec.CP56Time2a{Year: 26, Month: 10, Day: 23},
				}
			}
			send(t, e.Session, &iec.ASDU{
				Header:  iec.Header{Type: typ, Cause: iec.Spontaneous, CommonAddress: 1},
				Objects: []iec.Object{{Address: uint32(i/2 + 1), Element: element}},
			})
		}
		a := e.ASDU.(*iec.ASDU)
		send(t, e.Session, echo(a, iec.ActivationTerm, false))
		waitCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if !out.await(waitCtx, `"event":"gi_complete"`) {
			t.Error("monitor GI stalled")
		}
		e.Session.RequestClose()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code := run(ctx, []string{addr, "monitor", "--hold", "1", "--json"}, out, errout, testRuntime())
	if code != 3 || strings.Count(out.String(), `"event":"asdu"`) != 160 || !strings.Contains(out.String(), `"event":"gi_complete"`) {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), errout.String())
	}
}
