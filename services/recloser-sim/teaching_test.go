package main

import (
	"context"
	"log"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/tonylturner/dnp3go"
)

// The production outstation binds :20000 and has no shutdown API. Keep it
// inside a child test process so the listener cannot leak into other tests.
func TestRecloserTripAndFaultRecovery(t *testing.T) {
	if os.Getenv("RECLOSER_TEACHING_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecloserTripAndFaultRecovery$", "-test.v")
		cmd.Env = append(os.Environ(), "RECLOSER_TEACHING_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("production recloser checks: %v\n%s", err, out)
		}
		t.Logf("%s", out)
		return
	}

	ready := make(chan string, 1)
	log.SetOutput(&outstationReadyLog{ready: ready})
	go startDNP3Server()
	select {
	case message := <-ready:
		if !strings.Contains(message, "listening on :20000") {
			t.Fatalf("cannot start isolated production outstation: %s", message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("production outstation did not start")
	}
	// Connect only after our own listener confirms ownership. If port 20000
	// is occupied, fail above rather than sending commands to another service.
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			resetRecloserState()
			state.mu.Lock()
			state.RecloseEnabled = enabled
			state.mu.Unlock()
			conn, err := net.DialTimeout("tcp", "127.0.0.1:20000", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			// G12V1, one CROB, binary output 0, Direct Operate trip.
			apdu := []byte{0xc0, dnp3go.FCDirectOperate, 12, 1, 0x28, 1, 0, 0, 0,
				dnp3go.CROBTripPulse, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
			if err := dnp3go.WriteLinkFrame(conn, &dnp3go.LinkFrame{
				Control: 0xc4, Dest: 2, Source: 100, Payload: dnp3go.WrapTransport(apdu, 0),
			}); err != nil {
				t.Fatal(err)
			}
			response, err := dnp3go.ReadLinkFrame(conn)
			if err != nil {
				t.Fatal(err)
			}
			transport := &dnp3go.TransportReader{}
			parsed, err := dnp3go.ParseAPDU(transport.ProcessSegment(response.Payload))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.FuncCode != dnp3go.FCResponse || len(parsed.Objects) != 1 || len(parsed.Objects[0].PrefixedData) != 1 {
				t.Fatalf("unexpected trip response: %+v", parsed)
			}
			object := parsed.Objects[0].PrefixedData[0]
			if object.Index != 0 || len(object.Data) != 11 || object.Data[10] != dnp3go.CROBStatusSuccess {
				t.Fatalf("trip CROB response: %+v", object)
			}
			assertRecloserPosition(t, false)
			time.Sleep(2600 * time.Millisecond)
			assertRecloserPosition(t, false)
			if state.snapshot()["shot_count"] != 0 {
				t.Fatal("remote trip incorrectly counted as a fault")
			}

			ts := httptest.NewServer(newRecloserMux())
			defer ts.Close()
			if result := postRecloserCmd(ts, "close"); result["result"] != "executed" {
				t.Fatalf("close = %v", result)
			}
			if result := postRecloserCmd(ts, "inject_fault"); result["result"] != "executed" {
				t.Fatalf("inject_fault = %v", result)
			}
			assertRecloserPosition(t, false)
			if enabled {
				deadline := time.Now().Add(2400 * time.Millisecond)
				for state.snapshot()["closed"] != true && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				assertRecloserPosition(t, true)
				// Clear the fault during the first reclose, then let the
				// pending persistent-fault check finish before resetting state.
				if result := postRecloserCmd(ts, "clear_fault"); result["result"] != "executed" {
					t.Fatalf("clear_fault = %v", result)
				}
				time.Sleep(600 * time.Millisecond)
				assertRecloserPosition(t, true)
			} else {
				time.Sleep(2600 * time.Millisecond)
				assertRecloserPosition(t, false)
			}
			if state.snapshot()["shot_count"] != 1 {
				t.Fatal("injected fault must count as one shot")
			}
		})
	}
}

type outstationReadyLog struct{ ready chan<- string }

func (w *outstationReadyLog) Write(p []byte) (int, error) {
	message := string(p)
	if strings.Contains(message, "listening on :20000") || strings.Contains(message, "DNP3 server failed") {
		w.ready <- message
	}
	return os.Stderr.Write(p)
}

func assertRecloserPosition(t *testing.T, closed bool) {
	t.Helper()
	if got := state.snapshot()["closed"]; got != closed {
		t.Fatalf("closed = %v, want %v", got, closed)
	}
}
