// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"testing"
)

type heldFileProvider struct {
	memoryFiles
	entered chan struct{}
	release chan struct{}
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
