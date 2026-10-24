// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// FileServer is an opt-in controlled-station transparent-file handler. It owns no
// filesystem resources. Use Handler as (or within) the session/server callback.
// One bounded worker per connection isolates provider calls from APCI/callbacks;
// Closed cancels and joins that worker. Providers and loggers must honor cancellation.
// Positive QueryLog processing, uploads and structured archives remain application policy.
type FileServer struct {
	provider FileProvider
	mu       sync.Mutex
	links    map[uint64]*fileServing
}

// NewFileServer creates a reusable handler; a nonnil provider is required.
func NewFileServer(p FileProvider) (*FileServer, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: nil file provider", ErrMalformed)
	}
	return &FileServer{provider: p, links: map[uint64]*fileServing{}}, nil
}

// Handler preserves normal ASDU delivery when composed with another handler. It
// never generates COT44..47. It conservatively rejects QueryLog with FRQ not-ready;
// applications implementing positive QueryLog must route type127 to their own handler.
func (f *FileServer) Handler(ctx context.Context, e Event) {
	f.mu.Lock()
	w := f.links[e.ConnID]
	if e.Kind == Opened && e.Session.role == ServerRole {
		child, cancel := context.WithCancel(ctx)
		w = &fileServing{s: e.Session, provider: f.provider, ctx: child, cancel: cancel, in: make(chan Event, e.Session.opts.EventQueue), done: make(chan struct{})}
		f.links[e.ConnID] = w
		go w.run()
	}
	if e.Kind == Closed {
		delete(f.links, e.ConnID)
	}
	f.mu.Unlock()
	if w == nil {
		return
	}
	if e.Kind == Closed {
		w.cancel()
		<-w.done
		return
	}
	if e.Kind == Stopped || e.Kind == ASDUReceived && isFileType(e.ASDU.GetHeader().Type) {
		if e.Kind == ASDUReceived {
			// Composition returns the original to application code immediately;
			// the asynchronous worker must own its message before that happens.
			owned, err := cloneMessage(e.ASDU)
			if err != nil {
				w.report(err)
				w.s.RequestClose()
				return
			}
			e.ASDU = owned
		}
		select {
		case w.in <- e:
		case <-w.done:
		default:
			w.s.opts.Logf("iec104go: file worker queue full conn=%d", e.ConnID)
			w.s.RequestClose()
		}
	}
}

type fileServing struct {
	s        *Session
	provider FileProvider
	ctx      context.Context
	cancel   context.CancelFunc
	in       chan Event
	done     chan struct{}
	key      FileKey
	file     File
	total    uint32
	index    int
	state    uint8
	retries  int
}

// states: 0 idle, 1 selected, 2 section offered, 3 waiting section ACK,
// 4 waiting file ACK. A checksum NACK returns to 2 for an explicit recall.
func (w *fileServing) run() {
	defer close(w.done)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-timer.C:
			ctx, cancel := context.WithTimeout(w.ctx, w.s.opts.Files.Timeout)
			err := w.abort(ctx)
			cancel()
			if err != nil {
				w.report(err)
				w.s.RequestClose()
				return
			}
		case e := <-w.in:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if e.Kind == Stopped {
				w.reset()
				continue
			}
			ctx, cancel := context.WithTimeout(w.ctx, w.s.opts.Files.Timeout)
			err := w.handle(ctx, e.ASDU)
			cancel()
			if err != nil {
				w.report(err)
				w.s.RequestClose()
				return
			}
			if w.state != 0 {
				timer.Reset(w.s.opts.Files.Timeout)
			}
		}
	}
}
func (w *fileServing) report(err error) {
	w.s.opts.Logf("iec104go: file service conn=%d: %v", w.s.ID(), err)
}
func (w *fileServing) reset() { w.file = File{}; w.state = 0; w.index = 0; w.retries = 0; w.total = 0 }
func (w *fileServing) send(ctx context.Context, k FileKey, e Element, cot COT) error {
	return fileSend(ctx, w.s, fileMessage(k, e, cot))
}
func (w *fileServing) abort(ctx context.Context) error {
	if w.state == 0 {
		return nil
	}
	k := w.key
	section := uint8(0)
	if len(w.file.Sections) > 0 {
		section = w.file.Sections[w.index].Number
	}
	w.reset()
	return w.send(ctx, k, FileLast{k.Name, section, 2, 0}, FileTransfer)
}
func (w *fileServing) negative(ctx context.Context, k FileKey, call FileCall, code uint8, cot COT) error {
	call.Name = k.Name
	call.Qualifier = call.Qualifier&15 | code<<4
	return w.send(ctx, k, call, cot)
}
func (w *fileServing) handle(ctx context.Context, m Message) error {
	a, ok := m.(*ASDU)
	if !ok {
		return nil
	}
	h := a.Header
	name, _ := fileElementName(a.Objects[0].Element)
	k := FileKey{h.CommonAddress, h.Originator, a.Objects[0].Address, name}
	if h.Test || h.Negative {
		w.report(ErrProcedureOrder)
		return nil
	}
	// This opt-in transparent-file policy declines archive processing without
	// inventing a positive QueryLog exchange or a generic COT44 response.
	if h.Type == F_SC_NB_1 {
		return w.send(ctx, k, FileReady{Name: k.Name, Qualifier: 128}, FileTransfer)
	}
	if h.Type == F_SC_NA_1 {
		call := a.Objects[0].Element.(FileCall)
		k.Name = call.Name
		if h.Cause == Request {
			if w.state != 0 || call.Qualifier != 0 {
				return w.negative(ctx, k, call, FileUnexpectedService, Request)
			}
			return w.directory(ctx, k)
		}
		if h.Cause != FileTransfer || call.Qualifier>>4 != 0 {
			return w.negative(ctx, k, call, FileUnexpectedService, FileTransfer)
		}
		switch call.Qualifier {
		case SelectFile, CallFile, CallSection, DeactivateFile:
		default:
			return w.negative(ctx, k, call, FileUnexpectedService, FileTransfer)
		}
		if call.Qualifier == SelectFile && w.state == 0 {
			return w.selectFile(ctx, k, call)
		}
		if call.Qualifier == SelectFile {
			return w.negative(ctx, k, call, FileUnexpectedService, FileTransfer)
		}
		if w.state == 0 || k != w.key {
			return w.negative(ctx, k, call, FileUnknownName, FileTransfer)
		}
		switch call.Qualifier {
		case DeactivateFile:
			// FRQ explicitly confirms deactivation; no fabricated ACT-CON exchange.
			w.reset()
			return w.send(ctx, k, FileReady{Name: k.Name}, FileTransfer)
		case CallFile:
			if w.state != 1 || call.Section != 0 {
				break
			}
			w.state = 2
			return w.offer(ctx)
		case CallSection:
			if call.Section != w.file.Sections[w.index].Number {
				return w.negative(ctx, k, call, FileUnknownSection, FileTransfer)
			}
			if w.state != 2 {
				break
			}
			return w.section(ctx)
		}
		return w.negative(ctx, k, call, FileUnexpectedService, FileTransfer)
	}
	if h.Type == F_AF_NA_1 && h.Cause == FileTransfer {
		ack := a.Objects[0].Element.(FileACK)
		k.Name = ack.Name
		if w.state == 0 || k != w.key {
			return w.negative(ctx, k, FileCall{}, FileUnknownName, FileTransfer)
		}
		if ack.Section != w.file.Sections[w.index].Number {
			return w.negative(ctx, k, FileCall{}, FileUnknownSection, FileTransfer)
		}
		if w.state == 3 && ack.Qualifier == PositiveSectionACK {
			if w.index+1 < len(w.file.Sections) {
				w.index++
				w.retries = 0
				w.state = 2
				return w.offer(ctx)
			}
			var sum uint8
			for _, s := range w.file.Sections {
				sum += FileChecksum(s.Data)
			}
			w.state = 4
			return w.send(ctx, k, FileLast{k.Name, ack.Section, 1, sum}, FileTransfer)
		}
		if w.state == 4 && ack.Qualifier == PositiveFileACK {
			w.reset()
			return nil
		}
		if w.state == 3 && ack.Qualifier == NegativeSectionACK|FileChecksumFailure<<4 && w.retries < w.s.opts.Files.SectionRetries {
			w.retries++
			w.state = 2
			return nil
		}
		if w.state == 3 && ack.Qualifier&15 == NegativeSectionACK || w.state == 4 && ack.Qualifier&15 == NegativeFileACK {
			return w.abort(ctx)
		}
		return w.negative(ctx, k, FileCall{}, FileUnexpectedService, FileTransfer)
	}
	// Incoming offers/data constitute unsupported uploads, not download state.
	// A verified SCQ unexpected-service status rejects these without guessing an
	// upload state machine or automatically answering generic COT44..47.
	return w.negative(ctx, k, FileCall{}, FileUnexpectedService, FileTransfer)
}
func (w *fileServing) selectFile(ctx context.Context, k FileKey, call FileCall) error {
	if call.Section != 0 {
		return w.negative(ctx, k, call, FileUnknownSection, FileTransfer)
	}
	f, err := w.provider.Open(ctx, k)
	if err == nil {
		w.total, err = fileLength(f, w.s.opts.Files.MaxFileSize)
	}
	if err != nil {
		return w.send(ctx, k, FileReady{Name: k.Name, Qualifier: 128}, FileTransfer)
	}
	w.key = k
	w.file = f
	w.index = 0
	w.state = 1
	w.retries = 0
	return w.send(ctx, k, FileReady{Name: k.Name, Length: w.total}, FileTransfer)
}
func (w *fileServing) offer(ctx context.Context) error {
	s := w.file.Sections[w.index]
	return w.send(ctx, w.key, SectionReady{w.key.Name, s.Number, uint32(len(s.Data)), 0}, FileTransfer)
}
func (w *fileServing) section(ctx context.Context) error {
	s := w.file.Sections[w.index]
	limit := MaxASDUSize - w.s.opts.Params.headerSize() - w.s.opts.Params.IOASize - 4
	for off := 0; off < len(s.Data); {
		end := off + limit
		if end > len(s.Data) {
			end = len(s.Data)
		}
		if err := w.send(ctx, w.key, FileSegment{w.key.Name, s.Number, s.Data[off:end]}, FileTransfer); err != nil {
			return err
		}
		off = end
	}
	w.state = 3
	return w.send(ctx, w.key, FileLast{w.key.Name, s.Number, 3, FileChecksum(s.Data)}, FileTransfer)
}
func (w *fileServing) directory(ctx context.Context, k FileKey) error {
	entries, err := w.provider.Directory(ctx, k)
	if err != nil || len(entries) == 0 || len(entries) > w.s.opts.Files.DirectoryLimit {
		return w.negative(ctx, k, FileCall{}, FileMemoryUnavailable, Request)
	}
	for _, d := range entries {
		if d.Length > w.s.opts.Files.MaxFileSize {
			return w.negative(ctx, k, FileCall{}, FileMemoryUnavailable, Request)
		}
	}
	h := Header{Type: F_DR_TA_1, Sequence: true, Cause: Request, CommonAddress: k.CommonAddress, Originator: k.Originator}
	b, err := NewBuilder(w.s.opts.Params, h)
	if err != nil {
		return err
	}
	for i, d := range entries {
		d.Status &^= 0x20
		if i == len(entries)-1 {
			d.Status |= 0x20
		}
		o := Object{k.Address, d}
		err = b.Append(o)
		if errors.Is(err, ErrFull) {
			if err = fileSend(ctx, w.s, b.ASDU()); err != nil {
				return err
			}
			b, err = NewBuilder(w.s.opts.Params, h)
			if err != nil {
				return err
			}
			err = b.Append(o)
		}
		if err != nil {
			return err
		}
	}
	return fileSend(ctx, w.s, b.ASDU())
}
