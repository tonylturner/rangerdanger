// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"testing"
)

func TestFileProcedureFinalizationPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		called, cleanEnd, buffered bool
		err, recorded, want        error
		fence                      bool
	}{
		{"unadmitted call queue full", false, false, false, ErrQueueFull, nil, ErrQueueFull, false},
		{"admitted call queue full", true, false, false, ErrQueueFull, nil, ErrQueueFull, true},
		{"select timeout", false, false, false, context.DeadlineExceeded, nil, context.DeadlineExceeded, true},
		{"select canceled", false, false, false, context.Canceled, nil, context.Canceled, true},
		{"select order error", false, false, false, ErrProcedureOrder, nil, ErrProcedureOrder, true},
		{"negative after call", true, true, false, ErrNegativeResponse, nil, ErrNegativeResponse, false},
		{"remote deactivated", true, true, false, ErrFileAborted, nil, ErrFileAborted, false},
		{"section only aborted", true, false, false, ErrFileAborted, nil, ErrFileAborted, true},
		{"overflow before call", false, false, false, nil, ErrQueueFull, ErrQueueFull, true},
		{"overflow beats negative", false, true, true, ErrNegativeResponse, ErrQueueFull, ErrQueueFull, true},
		{"traffic after clean end", true, true, true, ErrNegativeResponse, nil, ErrProcedureOrder, true},
		{"traffic after success", true, false, true, nil, nil, ErrProcedureOrder, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{opts: DefaultOptions(), accepting: true, wake: make(chan struct{}, 1)}
			c := &Client{s}
			r, err := c.beginFile(testContext(t), testFileKey)
			if err != nil {
				t.Fatal(err)
			}
			r.admitted = true
			r.t.file.called, r.t.file.cleanEnd = tc.called, tc.cleanEnd
			r.t.err = tc.recorded
			if tc.buffered {
				a := fileMessage(testFileKey, FileReady{Name: 9}, FileTransfer)
				r.t.responses = []Message{&a}
			}
			err = r.finish(tc.err)
			if !errors.Is(err, tc.want) || s.procedure != nil || s.procedureUncertain != tc.fence {
				t.Fatalf("err=%v, detached=%v, fenced=%v", err, s.procedure == nil, s.procedureUncertain)
			}
			wantSends := 0
			if tc.fence {
				wantSends = 1
			}
			if len(s.sends) != wantSends {
				t.Fatal("unexpected cleanup admission", len(s.sends))
			}
		})
	}
}

func TestFileProcedureUncorrelatedNegativeFences(t *testing.T) {
	c, _ := fileClientPeer(t, func(_ context.Context, e Event) {
		if e.Kind != ASDUReceived {
			return
		}
		if call, ok := e.ASDU.(*ASDU).Objects[0].Element.(FileCall); ok && call.Qualifier == SelectFile {
			// The file identity matches, but this is not a rejection of select.
			sendFileResponse(t, e.Session, FileCall{9, 2, CallSection | FileUnknownSection<<4}, FileTransfer)
		}
	}, nil)
	if _, err := c.DownloadFile(testContext(t), testFileKey); !errors.Is(err, ErrProcedureOrder) {
		t.Fatal(err)
	}
	if _, err := c.Interrogate(testContext(t), 1, 7, 20); !errors.Is(err, ErrProcedureUncertain) {
		t.Fatal("uncorrelated negative was treated as clean", err)
	}
}
