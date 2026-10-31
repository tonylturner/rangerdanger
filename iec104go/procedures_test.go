// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

func pairedClient(t *testing.T, handler func(*Session, Event)) (*Client, *Session) {
	t.Helper()
	a, b := net.Pipe()
	opts := DefaultOptions()
	opts.Clock = newFakeClock()
	right, err := NewSession(context.Background(), b, ServerRole, opts, func(_ context.Context, e Event) {
		if handler != nil {
			handler(e.Session, e)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	left, err := NewSession(context.Background(), a, ClientRole, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSession(t, left); closeSession(t, right) })
	if err := left.StartDT(testContext(t)); err != nil {
		t.Fatal(err)
	}
	return &Client{left}, right
}
func reply(t *testing.T, s *Session, m Message, cot COT, negative bool) {
	t.Helper()
	mirror, err := Mirror(m, cot, negative)
	if err != nil {
		t.Error(err)
		return
	}
	if err := s.Send(mirror); err != nil {
		t.Error(err)
	}
}
func TestClientGIFromHandlerLargerThanK(t *testing.T) {
	client, _ := pairedClient(t, func(s *Session, e Event) {
		if e.Kind != ASDUReceived {
			return
		}
		reply(t, s, e.ASDU, ActivationCon, false)
		h := e.ASDU.GetHeader()
		for i := 0; i < 40; i++ {
			a := request(h.CommonAddress, h.Originator, M_ME_NC_1, uint32(i+1), Float{Value: float32(i)})
			a.Header.Cause = InterrogatedStation
			if err := s.Send(a); err != nil {
				t.Error(err)
			}
		}
		reply(t, s, e.ASDU, ActivationTerm, false)
	})
	responses, err := client.Interrogate(testContext(t), 1, 7, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 42 {
		t.Fatalf("GI responses=%d", len(responses))
	}
	for _, m := range responses {
		if m.GetHeader().Originator != 7 {
			t.Fatal("OA changed")
		}
	}
}
func TestClientSBOAndProcedures(t *testing.T) {
	var mu sync.Mutex
	var selections []bool
	client, _ := pairedClient(t, func(s *Session, e Event) {
		if e.Kind != ASDUReceived {
			return
		}
		a := e.ASDU.(*ASDU)
		switch a.Header.Type {
		case C_DC_NA_1:
			selecting := a.Objects[0].Element.(DoubleCommand).Qualifier.Select()
			mu.Lock()
			selections = append(selections, selecting)
			mu.Unlock()
			reply(t, s, a, ActivationCon, false)
			if !selecting {
				reply(t, s, a, ActivationTerm, false)
			}
		case C_IC_NA_1, C_CI_NA_1:
			reply(t, s, a, ActivationCon, false)
			reply(t, s, a, ActivationTerm, false)
		case C_RD_NA_1:
			data := request(a.Header.CommonAddress, a.Header.Originator, M_SP_NA_1, a.Objects[0].Address, SinglePoint{SIQ: 1})
			data.Header.Cause = Request
			if err := s.Send(data); err != nil {
				t.Error(err)
			}
		default:
			reply(t, s, a, ActivationCon, false)
		}
	})
	req := request(1, 3, C_DC_NA_1, 3001, DoubleCommand{Qualifier: 2})
	responses, err := client.SelectExecute(testContext(t), req)
	if err != nil || len(responses) != 3 {
		t.Fatalf("SBO=%d %v", len(responses), err)
	}
	mu.Lock()
	if !reflect.DeepEqual(selections, []bool{true, false}) {
		t.Errorf("selection=%v", selections)
	}
	mu.Unlock()
	tag := CP56FromTime(time.Now(), false)
	for _, tc := range []struct {
		name  string
		run   func(context.Context) ([]Message, error)
		count int
	}{
		{"group", func(ctx context.Context) ([]Message, error) { return client.Interrogate(ctx, 1, 3, 36) }, 2},
		{"counter", func(ctx context.Context) ([]Message, error) { return client.CounterInterrogate(ctx, 1, 3, 5) }, 2},
		{"read", func(ctx context.Context) ([]Message, error) { return client.Read(ctx, 1, 3, 42) }, 1},
		{"clock", func(ctx context.Context) ([]Message, error) { return client.SyncClock(ctx, 1, 3, tag) }, 1},
		{"test", func(ctx context.Context) ([]Message, error) { return client.Test(ctx, 1, 3, 1234, tag) }, 1},
		{"reset", func(ctx context.Context) ([]Message, error) { return client.Reset(ctx, 1, 3, 1) }, 1},
		{"direct", func(ctx context.Context) ([]Message, error) { return client.Command(ctx, req) }, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := tc.run(testContext(t))
			if err != nil || len(m) != tc.count {
				t.Fatalf("responses=%d %v", len(m), err)
			}
		})
	}
	if req.Objects[0].Element.(DoubleCommand).Qualifier != 2 {
		t.Fatal("SBO mutated caller")
	}
}
func TestClientProcedureErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cause    COT
		negative bool
		want     error
	}{
		{"negative", ActivationCon, true, ErrNegativeResponse},
		{"unknown-cause", UnknownCause, false, ErrNegativeResponse},
		{"termination-before-confirmation", ActivationTerm, false, ErrProcedureOrder},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := pairedClient(t, func(s *Session, e Event) {
				if e.Kind == ASDUReceived {
					reply(t, s, e.ASDU, tc.cause, tc.negative)
				}
			})
			m, err := client.Interrogate(testContext(t), 1, 0, 20)
			if !errors.Is(err, tc.want) || len(m) != 1 {
				t.Fatalf("responses=%d err=%v", len(m), err)
			}
			if !client.IsStarted() {
				t.Fatal("procedure policy closed transport")
			}
		})
	}
	t.Run("deadline-and-busy", func(t *testing.T) {
		received := make(chan struct{}, 1)
		client, _ := pairedClient(t, func(s *Session, e Event) {
			if e.Kind == ASDUReceived {
				received <- struct{}{}
			}
		})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := client.Interrogate(ctx, 1, 0, 20); done <- err }()
		<-received
		if _, err := client.Interrogate(testContext(t), 1, 0, 20); !errors.Is(err, ErrProcedureBusy) {
			t.Fatalf("busy=%v", err)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := client.Interrogate(context.Background(), 1, 0, 20); err == nil {
			t.Fatal("unbounded helper accepted")
		}
	})
}
func TestMirrorRawAndTyped(t *testing.T) {
	for _, m := range []Message{sampleMessage(), &RawASDU{Header: Header{Type: 255, Count: 1, Cause: Activation, Originator: 7, CommonAddress: 9}, Payload: []byte{1, 2, 3}, DecodeError: ErrUnsupportedType}} {
		mirrored, err := Mirror(m, UnknownIOA, true)
		if err != nil {
			t.Fatal(err)
		}
		if h := mirrored.GetHeader(); h.Cause != UnknownIOA || !h.Negative || h.Originator != m.GetHeader().Originator || h.CommonAddress != m.GetHeader().CommonAddress {
			t.Fatalf("mirror=%+v", h)
		}
		switch a := mirrored.(type) {
		case *ASDU:
			a.Objects[0].Address = 99
		case *RawASDU:
			a.Payload[0] = 99
		}
		if m.GetHeader().Cause != Activation {
			t.Fatal("mirror changed source header")
		}
	}
	if _, err := Mirror((*ASDU)(nil), ActivationCon, false); err == nil {
		t.Fatal("nil mirror")
	}
}

func TestClientCommandSelectionMatrix(t *testing.T) {
	tag := CP56FromTime(time.Now(), false)
	for _, element := range []Element{SingleCommand{Qualifier: 1}, DoubleCommand{Qualifier: 2}, RegulatingCommand{Qualifier: 1}, SetpointNormalized{Value: 123, Qualifier: 7}, SetpointScaled{Value: -456, Qualifier: 6}, SetpointFloat{Value: 1.25, Qualifier: 5}} {
		for _, timed := range []bool{false, true} {
			e := element
			if timed {
				e = Timed{Value: e, Time: tag}
			}
			id, payload, err := encodeElement(e)
			if err != nil {
				t.Fatal(err)
			}
			req := request(1, 3, id, 9, e)
			selected, err := commandSelection(req, true)
			if err != nil {
				t.Fatal(err)
			}
			executed, err := commandSelection(selected, false)
			if err != nil {
				t.Fatal(err)
			}
			_, round, err := encodeElement(executed.Objects[0].Element)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(round, payload) {
				t.Fatal("select/execute changed value, qualifier or timestamp")
			}
		}
	}
	for _, e := range []Element{BitstringCommand{Value: 0x12345678}, Timed{Value: BitstringCommand{Value: 0x12345678}, Time: tag}} {
		id, _, err := encodeElement(e)
		if err != nil {
			t.Fatal(err)
		}
		req := request(1, 0, id, 1, e)
		if _, err := commandSelection(req, false); err != nil {
			t.Fatal(err)
		}
		if _, err := commandSelection(req, true); err == nil {
			t.Fatal("bitstring has no select bit")
		}
	}
}
func TestClientCanceledProcedureDoesNotSend(t *testing.T) {
	client, _ := pairedClient(t, func(s *Session, e Event) {
		if e.Kind == ASDUReceived {
			if e.ASDU.GetHeader().Type == C_DC_NA_1 {
				t.Error("sent a pre-canceled command")
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := request(1, 0, C_DC_NA_1, 1, DoubleCommand{Qualifier: 2})
	if _, err := client.Command(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if view(t, client.Session).sent != 0 {
		t.Fatal("canceled command admitted")
	}
	ctx, finish := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer finish()
	if _, err := client.Interrogate(ctx, 1, 0, 20); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline=%v", err)
	}
	if _, err := client.Interrogate(testContext(t), 1, 0, 20); !errors.Is(err, ErrProcedureUncertain) {
		t.Fatalf("late-response ambiguity not fenced: %v", err)
	}
}
