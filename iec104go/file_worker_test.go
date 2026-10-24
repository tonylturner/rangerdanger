// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"context"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type heldFileProvider struct {
	memoryFiles
	entered chan struct{}
	release chan struct{}
}

type transferFileProvider struct {
	memoryFiles
	first    atomic.Bool
	entered  chan struct{}
	canceled chan struct{}
	release  chan struct{}
	late     bool
}

func (p *transferFileProvider) Open(ctx context.Context, _ FileKey) (File, error) {
	if !p.first.Swap(true) {
		close(p.entered)
		<-ctx.Done()
		close(p.canceled)
		if !p.late {
			return File{}, ctx.Err()
		}
		// Model a result already being finalized when cancellation arrives.
		// The test releases it after the next STARTDT, never leaving it stuck.
		<-p.release
	}
	return p.memoryFiles.Open(ctx, testFileKey)
}

func stopFilePeer(t *testing.T, s *Session, peer net.Conn) {
	t.Helper()
	writeFrame(t, peer, Frame{Kind: UFrame, Function: StopDTAct})
	for {
		f := readFrame(t, peer)
		if f.Kind == UFrame && f.Function == StopDTCon {
			break
		}
		if f.Kind != SFrame {
			t.Fatal("unexpected data while stopping", f)
		}
	}
	waitView(t, s, func(v sessionView) bool { return !v.started })
}

func TestFileServerStopCancelsProviderGeneration(t *testing.T) {
	for _, late := range []bool{false, true} {
		name := "canceled"
		if late {
			name = "late success"
		}
		t.Run(name, func(t *testing.T) {
			p := &transferFileProvider{
				memoryFiles: memoryFiles{file: File{[]FileSection{{2, []byte{1}}}}},
				entered:     make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{}), late: late,
			}
			fs, _ := NewFileServer(p)
			s, peer, _ := manualSession(t, ServerRole, fileTestOptions(), fs.Handler)
			// Cleanup registration is LIFO; do not let a held provider block Close.
			released := false
			t.Cleanup(func() {
				if !released {
					close(p.release)
					released = true
				}
			})
			startServerSession(t, s, peer)
			fileRequestPeer(t, peer, 0, 0, testFileKey, FileCall{Name: 9, Qualifier: SelectFile}, FileTransfer)
			select {
			case <-p.entered:
			case <-testContext(t).Done():
				t.Fatal("provider did not enter")
			}
			stopFilePeer(t, s, peer)
			select {
			case <-p.canceled:
			case <-testContext(t).Done():
				t.Fatal("STOPDT did not cancel provider work")
			}
			startServerSession(t, s, peer)
			k := testFileKey
			k.Name = 10
			fileRequestPeer(t, peer, 1, 0, k, FileCall{Name: 10, Qualifier: SelectFile}, FileTransfer)
			close(p.release)
			released = true
			e := filePeerResponse(t, peer)
			if ready, ok := e.(FileReady); !ok || ready.Name != 10 || ready.Qualifier != 0 {
				t.Fatal("stale generation admitted after STARTDT", e)
			}
		})
	}
}

func TestFileServerStopWithBackpressuredSegments(t *testing.T) {
	opts := fileTestOptions()
	opts.APCI.K, opts.SendQueue = 1, 1
	fs, _ := NewFileServer(memoryFiles{file: File{[]FileSection{{2, bytes.Repeat([]byte{1}, 236*8)}}}})
	s, peer, _ := manualSession(t, ServerRole, opts, fs.Handler)
	startServerSession(t, s, peer)
	for i, call := range []FileCall{{9, 0, SelectFile}, {9, 0, CallFile}, {9, 2, CallSection}} {
		fileRequestPeer(t, peer, uint16(i), uint16(i), testFileKey, call, FileTransfer)
		filePeerResponse(t, peer)
	}
	eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.sends) == 1
	})
	writeFrame(t, peer, Frame{Kind: UFrame, Function: StopDTAct})
	// The already-admitted segment drains. No later old-generation segment
	// may enter the queue even if the peer restarts immediately afterwards.
	waitView(t, s, func(v sessionView) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.accepting
	})
	writeFrame(t, peer, Frame{Kind: SFrame, Receive: 3})
	var nr uint16 = 3
	for {
		f := readFrame(t, peer)
		if f.Kind == UFrame && f.Function == StopDTCon {
			break
		}
		if f.Kind == IFrame {
			nr = (f.Send + 1) & 32767
			writeFrame(t, peer, Frame{Kind: SFrame, Receive: nr})
		}
	}
	startServerSession(t, s, peer)
	k := testFileKey
	k.Name = 10
	fileRequestPeer(t, peer, 3, nr, k, FileCall{Name: 10, Qualifier: SelectFile}, FileTransfer)
	if e, ok := filePeerResponse(t, peer).(FileReady); !ok || e.Name != 10 || e.Qualifier != 128 {
		t.Fatal("old segments survived into restarted transfer", e)
	}
}

func fileIdleArmed(t *testing.T, clock *fakeClock, timeout time.Duration) {
	t.Helper()
	eventually(t, func() bool {
		clock.mu.Lock()
		defer clock.mu.Unlock()
		for timer := range clock.timers {
			if timer.active && timer.deadline.Equal(clock.now.Add(timeout)) {
				return true
			}
		}
		return false
	})
}

func TestFileServerIdleUsesClock(t *testing.T) {
	opts := fileTestOptions()
	opts.Files.Timeout = 5 * time.Second
	fs, _ := NewFileServer(memoryFiles{file: File{[]FileSection{{2, []byte{1}}}}})
	s, peer, clock := manualSession(t, ServerRole, opts, fs.Handler)
	startServerSession(t, s, peer)
	fileRequestPeer(t, peer, 0, 0, testFileKey, FileCall{Name: 9, Qualifier: SelectFile}, FileTransfer)
	filePeerResponse(t, peer)
	fileIdleArmed(t, clock, opts.Files.Timeout)
	clock.Advance(opts.Files.Timeout)
	if e, ok := filePeerResponse(t, peer).(FileLast); !ok || e.Qualifier != 2 {
		t.Fatal("injected clock did not expire idle state", e)
	}
}

func TestFileAdmissionWake(t *testing.T) {
	if _, exists := reflect.TypeFor[FileOptions]().FieldByName("QueuePoll"); exists {
		t.Fatal("file admission must not expose a polling interval")
	}
	opts := fileTestOptions()
	opts.APCI.K, opts.SendQueue = 1, 1
	s, peer, _ := manualSession(t, ServerRole, opts, nil)
	startServerSession(t, s, peer)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	readFrame(t, peer)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	ready, generation := s.sendReady, s.generation
	s.mu.Unlock()
	done := make(chan error, 1)
	ctx := testContext(t)
	go func() {
		done <- fileSend(ctx, s, generation, fileMessage(testFileKey, FileReady{Name: 9}, FileTransfer))
	}()
	// One cumulative ACK frees a queue slot. No injected-clock tick (or
	// admission-poll timer) is involved in broadcasting the capacity change.
	writeFrame(t, peer, Frame{Kind: SFrame, Receive: 1})
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("capacity change did not wake waiters")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("admission waiter stuck after capacity changed")
	}
}

func (p heldFileProvider) Open(ctx context.Context, k FileKey) (File, error) {
	close(p.entered)
	select {
	case <-p.release:
		return p.memoryFiles.Open(ctx, k)
	case <-ctx.Done():
		return File{}, ctx.Err()
	}
}

func TestFileServerCallbackMessageOwnership(t *testing.T) {
	p := heldFileProvider{
		memoryFiles: memoryFiles{file: File{[]FileSection{{2, []byte{1, 2}}}}},
		entered:     make(chan struct{}), release: make(chan struct{}),
	}
	fs, _ := NewFileServer(p)
	mutated := make(chan struct{})
	handler := func(ctx context.Context, e Event) {
		fs.Handler(ctx, e)
		if e.Kind == ASDUReceived {
			a := e.ASDU.(*ASDU)
			if a.Objects[0].Element.(FileCall).Qualifier == CallFile {
				// The worker is blocked in Open, so the queued call cannot have
				// been inspected yet. The callback owns and may change its copy.
				a.Header.CommonAddress = 99
				a.Objects[0].Address = 0
				a.Objects[0].Element = FileCall{Name: 42, Qualifier: CallFile}
				close(mutated)
			}
		}
	}
	s, peer, _ := manualSession(t, ServerRole, fileTestOptions(), handler)
	startServerSession(t, s, peer)
	fileRequestPeer(t, peer, 0, 0, testFileKey, FileCall{Name: 9, Qualifier: SelectFile}, FileTransfer)
	select {
	case <-p.entered:
	case <-testContext(t).Done():
		t.Fatal("provider not entered")
	}
	fileRequestPeer(t, peer, 1, 0, testFileKey, FileCall{Name: 9, Qualifier: CallFile}, FileTransfer)
	select {
	case <-mutated:
	case <-testContext(t).Done():
		t.Fatal("callback not delivered")
	}
	close(p.release)
	if e, ok := filePeerResponse(t, peer).(FileReady); !ok || e.Name != 9 {
		t.Fatal("unexpected selection result", e)
	}
	if e, ok := filePeerResponse(t, peer).(SectionReady); !ok || e.Name != 9 || e.Section != 2 {
		t.Fatal("worker observed callback mutation", e)
	}
}
