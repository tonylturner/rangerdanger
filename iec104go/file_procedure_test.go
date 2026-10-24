// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"context"
	"errors"
	"net"
	"reflect"
	"runtime/pprof"
	"sync/atomic"
	"testing"
	"time"
)

type memoryFiles struct {
	file    File
	entries []FileDirectory
	err     error
}

func (p memoryFiles) Directory(context.Context, FileKey) ([]FileDirectory, error) {
	return append([]FileDirectory(nil), p.entries...), p.err
}
func (p memoryFiles) Open(_ context.Context, k FileKey) (File, error) {
	if k.Name != 9 {
		return File{}, ErrFileNotFound
	}
	f := File{}
	for _, s := range p.file.Sections {
		f.Sections = append(f.Sections, FileSection{s.Number, append([]byte(nil), s.Data...)})
	}
	return f, p.err
}

var testFileKey = FileKey{1, 7, 0xffffff, 9}

func filePair(t *testing.T, p FileProvider, opts Options, tweak func(net.Conn) net.Conn) (*Client, *Session, *FileServer) {
	t.Helper()
	fs, err := NewFileServer(p)
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	if tweak != nil {
		b = tweak(b)
	}
	right, err := NewSession(context.Background(), b, ServerRole, opts, fs.Handler)
	if err != nil {
		t.Fatal(err)
	}
	left, err := NewSession(context.Background(), a, ClientRole, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeSession(t, left)
		closeSession(t, right)
		fs.mu.Lock()
		defer fs.mu.Unlock()
		if len(fs.links) != 0 {
			t.Error("file worker retained after Closed")
		}
	})
	if err := left.StartDT(testContext(t)); err != nil {
		t.Fatal(err)
	}
	return &Client{left}, right, fs
}
func fileTestOptions() Options {
	o := DefaultOptions()
	o.Clock = newFakeClock()
	o.Logf = func(string, ...any) {}
	o.Files.QueuePoll = 10 * time.Microsecond
	return o
}
func TestFileProcedureDownloadAndDirectory(t *testing.T) {
	p := memoryFiles{file: File{[]FileSection{{1, bytes.Repeat([]byte{3, 7, 255}, 50000)}, {255, []byte{1, 2, 3, 4}}}}}
	for i := 0; i < 40; i++ {
		p.entries = append(p.entries, FileDirectory{Name: uint16(i + 1), Length: 1})
	}
	opts := fileTestOptions()
	opts.SendQueue = 2
	opts.APCI.K = 1
	opts.APCI.W = 1
	client, _, _ := filePair(t, p, opts, nil)
	entries, err := client.Directory(testContext(t), testFileKey)
	if err != nil || len(entries) != 40 {
		t.Fatalf("directory=%d %v", len(entries), err)
	}
	for i, d := range entries {
		if (d.Status&32 != 0) != (i == 39) {
			t.Fatal("bad LFD")
		}
	}
	file, err := client.DownloadFile(testContext(t), testFileKey)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(file, p.file) {
		t.Fatal("download differs")
	}
	// More than EventQueue segments succeeded without retaining every response.
	if len(file.Sections[0].Data)/236 <= opts.EventQueue {
		t.Fatal("test does not exceed transaction history limit")
	}
	if _, err := client.DownloadFile(testContext(t), testFileKey); err != nil {
		t.Fatal("file helper not reusable", err)
	}
}
func TestFileProcedureProviderNegatives(t *testing.T) {
	for _, tc := range []struct {
		name      string
		p         memoryFiles
		directory bool
	}{
		{"empty directory", memoryFiles{}, true},
		{"zero file", memoryFiles{}, false},
		{"zero section", memoryFiles{file: File{[]FileSection{{1, nil}}}}, false},
		{"duplicate section", memoryFiles{file: File{[]FileSection{{1, []byte{1}}, {1, []byte{2}}}}}, false},
		{"provider unavailable", memoryFiles{err: errors.New("unavailable")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := filePair(t, tc.p, fileTestOptions(), nil)
			var err error
			if tc.directory {
				_, err = c.Directory(testContext(t), testFileKey)
			} else {
				_, err = c.DownloadFile(testContext(t), testFileKey)
			}
			if !errors.Is(err, ErrNegativeResponse) {
				t.Fatal(err)
			}
			if _, err = c.Directory(testContext(t), testFileKey); !errors.Is(err, ErrProcedureUncertain) {
				t.Fatal("failed exchange not fenced", err)
			}
		})
	}
	t.Run("unknown file", func(t *testing.T) {
		c, _, _ := filePair(t, memoryFiles{}, fileTestOptions(), nil)
		k := testFileKey
		k.Name = 10
		_, err := c.DownloadFile(testContext(t), k)
		if !errors.Is(err, ErrNegativeResponse) {
			t.Fatal(err)
		}
	})
}

// Corrupt only content after the server has encoded it; the server's original
// checksums stay independent. This is a transport test double, not a peer source.
type corruptFileConn struct {
	net.Conn
	mode    string
	changed atomic.Bool
}

func (c *corruptFileConn) Write(b []byte) (int, error) {
	if len(b) > 15 && b[2]&1 == 0 {
		id := TypeID(b[6])
		eligible := c.mode == "file-checksum" && id == F_LS_NA_1 && b[18] == 1 || c.mode != "file-checksum" && id == F_SG_NA_1
		if eligible && (!c.changed.Swap(true) || c.mode == "always") {
			b = append([]byte(nil), b...)
			b[len(b)-1]++
		}
	}
	return c.Conn.Write(b)
}
func TestFileProcedureChecksumAndRetry(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		retries int
		want    error
	}{{"once", 0, ErrFileChecksum}, {"once", 1, nil}, {"always", 1, ErrFileChecksum}, {"file-checksum", 0, ErrFileChecksum}} {
		t.Run(tc.mode+string(rune('0'+tc.retries)), func(t *testing.T) {
			opts := fileTestOptions()
			opts.Files.SectionRetries = tc.retries
			p := memoryFiles{file: File{[]FileSection{{3, []byte{250, 1, 2}}, {8, []byte{5, 6}}}}}
			c, _, _ := filePair(t, p, opts, func(n net.Conn) net.Conn { return &corruptFileConn{Conn: n, mode: tc.mode} })
			f, err := c.DownloadFile(testContext(t), testFileKey)
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if err == nil && !reflect.DeepEqual(f, p.file) {
				t.Fatal("retry double-counted content")
			}
		})
	}
}
func fileRequestPeer(t *testing.T, p net.Conn, ns, nr uint16, k FileKey, e Element, cot COT) {
	t.Helper()
	b, err := fileMessage(k, e, cot).MarshalBinary(DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	writeFrame(t, p, Frame{Kind: IFrame, Send: ns, Receive: nr, ASDU: b})
}
func filePeerResponse(t *testing.T, p net.Conn) Element {
	t.Helper()
	for {
		f := readFrame(t, p)
		if f.Kind != IFrame {
			continue
		}
		m, err := DecodeASDU(f.ASDU, DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		return m.(*ASDU).Objects[0].Element
	}
}
func TestFileServerUnknownSectionAndAbort(t *testing.T) {
	fs, err := NewFileServer(memoryFiles{file: File{[]FileSection{{2, []byte{1, 2}}}}})
	if err != nil {
		t.Fatal(err)
	}
	s, p, _ := manualSession(t, ServerRole, fileTestOptions(), fs.Handler)
	startServerSession(t, s, p)
	fileRequestPeer(t, p, 0, 0, testFileKey, FileCall{Name: 9, Qualifier: SelectFile}, FileTransfer)
	if e := filePeerResponse(t, p).(FileReady); e.Qualifier != 0 {
		t.Fatal(e)
	}
	fileRequestPeer(t, p, 1, 1, testFileKey, FileCall{Name: 9, Qualifier: CallFile}, FileTransfer)
	if e := filePeerResponse(t, p).(SectionReady); e.Section != 2 {
		t.Fatal(e)
	}
	fileRequestPeer(t, p, 2, 2, testFileKey, FileCall{9, 3, CallSection}, FileTransfer)
	if e := filePeerResponse(t, p).(FileCall); e.Qualifier != CallSection|FileUnknownSection<<4 {
		t.Fatal(e)
	}
	fileRequestPeer(t, p, 3, 3, testFileKey, FileCall{Name: 9, Qualifier: DeactivateFile}, FileTransfer)
	if e := filePeerResponse(t, p).(FileReady); e.Qualifier != 0 {
		t.Fatal(e)
	}
	fileRequestPeer(t, p, 4, 4, testFileKey, FileCall{Name: 9, Qualifier: CallFile}, FileTransfer)
	if e := filePeerResponse(t, p).(FileCall); e.Qualifier != CallFile|FileUnknownName<<4 {
		t.Fatal(e)
	}
}
func TestFileProcedureRemoteAbortAndUnexpected(t *testing.T) {
	for _, tc := range []struct {
		e    Element
		want error
	}{{FileLast{Name: 9, Qualifier: 2}, ErrFileAborted}, {FileSegment{Name: 9, Data: []byte{1}}, ErrProcedureOrder}, {SectionReady{Name: 9, Section: 1, Length: 1}, ErrProcedureOrder}} {
		t.Run(tc.want.Error()+tc.e.elementType().String(), func(t *testing.T) {
			c, _ := pairedClient(t, func(s *Session, e Event) {
				if e.Kind == ASDUReceived && e.ASDU.(*ASDU).Objects[0].Element.(FileCall).Qualifier == SelectFile {
					if err := s.Send(fileMessage(testFileKey, tc.e, FileTransfer)); err != nil {
						t.Error(err)
					}
				}
			})
			_, err := c.DownloadFile(testContext(t), testFileKey)
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
}
func TestFileServerTimeoutAndUnsupported(t *testing.T) {
	opts := fileTestOptions()
	opts.Files.Timeout = 25 * time.Millisecond
	fs, _ := NewFileServer(memoryFiles{file: File{[]FileSection{{1, []byte{1}}}}})
	s, p, _ := manualSession(t, ServerRole, opts, fs.Handler)
	startServerSession(t, s, p)
	fileRequestPeer(t, p, 0, 0, testFileKey, FileCall{Name: 9, Qualifier: SelectFile}, FileTransfer)
	filePeerResponse(t, p)
	if e := filePeerResponse(t, p).(FileLast); e.Qualifier != 2 {
		t.Fatal("idle timeout didn't deactivate", e)
	}
	// Unsupported delete/select-section/deactivate-section use verified service error.
	for i, q := range []uint8{DeleteFile, SelectSection, DeactivateSection} {
		fileRequestPeer(t, p, uint16(1+i*2), uint16(2+i*2), testFileKey, FileCall{Name: 9, Qualifier: SelectFile}, FileTransfer)
		filePeerResponse(t, p)
		fileRequestPeer(t, p, uint16(2+i*2), uint16(3+i*2), testFileKey, FileCall{Name: 9, Qualifier: q}, FileTransfer)
		if e := filePeerResponse(t, p).(FileCall); e.Qualifier != q|FileUnexpectedService<<4 {
			t.Fatal(e)
		}
		if e := filePeerResponse(t, p).(FileLast); e.Qualifier != 2 {
			t.Fatal(e)
		}
	}
}
func TestFileProcedureTimeoutAndLeakCheck(t *testing.T) {
	var before bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&before, 2)
	p := memoryFiles{file: File{[]FileSection{{1, []byte{1}}}}}
	c, s, fs := filePair(t, p, fileTestOptions(), nil)
	if _, err := c.DownloadFile(testContext(t), testFileKey); err != nil {
		t.Fatal(err)
	}
	closeSession(t, c.Session)
	closeSession(t, s)
	fs.mu.Lock()
	count := len(fs.links)
	fs.mu.Unlock()
	if count != 0 {
		t.Fatal("worker map leak")
	}
	eventually(t, func() bool {
		var after bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&after, 2)
		return bytes.Count(after.Bytes(), []byte("iec104go.(*fileServing).run")) <= bytes.Count(before.Bytes(), []byte("iec104go.(*fileServing).run"))
	})
	client, _ := pairedClient(t, nil)
	client.opts.Files.Timeout = 20 * time.Millisecond
	if _, err := client.DownloadFile(testContext(t), testFileKey); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestFileServerQueryLogRejection(t *testing.T) {
	fs, _ := NewFileServer(memoryFiles{})
	s, p, _ := manualSession(t, ServerRole, fileTestOptions(), fs.Handler)
	startServerSession(t, s, p)
	// Both-zero is a meaningful all-records interval at codec level; the optional
	// transparent-file service does not pretend to implement an archive provider.
	fileRequestPeer(t, p, 0, 0, testFileKey, QueryLog{Name: 9}, FileTransfer)
	if e := filePeerResponse(t, p).(FileReady); e.Name != 9 || e.Qualifier != 128 {
		t.Fatal(e)
	}
	fileRequestPeer(t, p, 1, 1, testFileKey, FileReady{Name: 9, Length: 1}, FileTransfer)
	if e := filePeerResponse(t, p).(FileCall); e.Name != 9 || e.Qualifier != FileUnexpectedService<<4 {
		t.Fatal("upload rejection lost identity", e)
	}
}

type cancelFileProvider struct {
	entered  chan struct{}
	canceled chan struct{}
}

func (p cancelFileProvider) Directory(ctx context.Context, k FileKey) ([]FileDirectory, error) {
	return nil, ErrFileNotFound
}
func (p cancelFileProvider) Open(ctx context.Context, k FileKey) (File, error) {
	close(p.entered)
	<-ctx.Done()
	close(p.canceled)
	return File{}, ctx.Err()
}
func TestFileServerProviderCancellationDoesNotStallAPCI(t *testing.T) {
	p := cancelFileProvider{make(chan struct{}), make(chan struct{})}
	fs, _ := NewFileServer(p)
	s, peer, clock := manualSession(t, ServerRole, fileTestOptions(), fs.Handler)
	startServerSession(t, s, peer)
	fileRequestPeer(t, peer, 0, 0, testFileKey, FileCall{Name: 9, Qualifier: SelectFile}, FileTransfer)
	<-p.entered
	// A blocked provider runs outside the ordinary ordered callback and APCI paths.
	writeFrame(t, peer, Frame{Kind: UFrame, Function: TestFRAct})
	if f := readFrame(t, peer); f.Function != TestFRCon {
		t.Fatal(f)
	}
	clock.Advance(10 * time.Second)
	if f := readFrame(t, peer); f.Kind != SFrame || f.Receive != 1 {
		t.Fatal("provider stalled ACK", f)
	}
	s.RequestClose()
	waitClosed(t, s)
	select {
	case <-p.canceled:
	default:
		t.Fatal("Closed did not join provider")
	}
}
func TestFileOptions(t *testing.T) {
	d := DefaultFileOptions()
	n, err := (FileOptions{}).normalized()
	if err != nil || n != d {
		t.Fatal(n, err)
	}
	for _, o := range []FileOptions{{Timeout: -1}, {QueuePoll: -1}, {SectionRetries: -1}, {SectionRetries: 11}, {MaxFileSize: 0x1000000}, {DirectoryLimit: -1}} {
		if _, err := o.normalized(); err == nil {
			t.Fatal(o)
		}
	}
	if _, err := NewFileServer(nil); err == nil {
		t.Fatal("nil provider accepted")
	}
}

type fileWriteGate struct {
	net.Conn
	nth, seen        int
	entered, release chan struct{}
}

func (c *fileWriteGate) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if b[2]&1 == 0 {
		c.seen++
		if c.seen == c.nth {
			close(c.entered)
			<-c.release
		}
	}
	return n, err
}
func TestFileProcedureEveryLegWaitsForCompleteWrite(t *testing.T) {
	for nth := 1; nth <= 7; nth++ {
		t.Run(string(rune('0'+nth)), func(t *testing.T) {
			a, b := net.Pipe()
			gate := &fileWriteGate{Conn: a, nth: nth, entered: make(chan struct{}), release: make(chan struct{})}
			fs, _ := NewFileServer(memoryFiles{file: File{[]FileSection{{1, []byte{1}}, {2, []byte{2}}}}})
			right, err := NewSession(context.Background(), b, ServerRole, fileTestOptions(), fs.Handler)
			if err != nil {
				t.Fatal(err)
			}
			left, err := NewSession(context.Background(), gate, ClientRole, fileTestOptions(), nil)
			if err != nil {
				t.Fatal(err)
			}
			released := false
			t.Cleanup(func() {
				if !released {
					close(gate.release)
				}
				closeSession(t, left)
				closeSession(t, right)
			})
			if err := left.StartDT(testContext(t)); err != nil {
				t.Fatal(err)
			}
			ctx := testContext(t)
			done := make(chan error, 1)
			go func() { _, err := (&Client{left}).DownloadFile(ctx, testFileKey); done <- err }()
			select {
			case <-gate.entered:
			case <-ctx.Done():
				t.Fatal("leg never written", ctx.Err())
			}
			select {
			case err := <-done:
				t.Fatal("completed before full Write", err)
			case <-time.After(5 * time.Millisecond):
			}
			close(gate.release)
			released = true
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

type wrongSectionOffer struct{ net.Conn }

func (c wrongSectionOffer) Write(b []byte) (int, error) {
	if len(b) > 17 && b[2]&1 == 0 && TypeID(b[6]) == F_SR_NA_1 {
		b = append([]byte(nil), b...)
		b[17]++
	}
	return c.Conn.Write(b)
}
func TestFileProcedureUnknownSection(t *testing.T) {
	p := memoryFiles{file: File{[]FileSection{{2, []byte{1}}}}}
	c, _, _ := filePair(t, p, fileTestOptions(), func(n net.Conn) net.Conn { return wrongSectionOffer{n} })
	_, err := c.DownloadFile(testContext(t), testFileKey)
	var negative *FileResponseError
	if !errors.As(err, &negative) || negative.Code != FileUnknownSection {
		t.Fatal(err)
	}
}
func TestFileProcedureCanceledLegDoesNotAdmit(t *testing.T) {
	s := &Session{opts: DefaultOptions(), accepting: true, wake: make(chan struct{}, 1)}
	c := &Client{s}
	r, err := c.beginFile(testContext(t), testFileKey)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = r.send(ctx, FileCall{Name: 9, Qualifier: SelectFile}, FileTransfer)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(s.sends) != 0 || r.admitted {
		t.Fatal("canceled request admitted")
	}
	if err = r.finish(err); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if s.procedure != nil || s.procedureUncertain {
		t.Fatal("unsent failure left state/fence")
	}
}
