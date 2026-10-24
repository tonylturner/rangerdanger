// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"fmt"
)

type fileReceiver struct {
	c        *Client
	t        *transaction
	key      FileKey
	admitted bool
	last     Element
}

func (c *Client) beginFile(ctx context.Context, k FileKey) (*fileReceiver, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, fmt.Errorf("%w: file context requires deadline", ErrMalformed)
	}
	t, err := c.begin(func(m Message) bool { return fileMatch(k, m) })
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.streaming = true
	t.file = &fileTransaction{}
	t.mu.Unlock()
	return &fileReceiver{c: c, t: t, key: k}, nil
}
func (r *fileReceiver) finish(err error) error {
	_, err = r.c.complete(r.t, err, r.admitted)
	if r.t.file.uncertain {
		// Only unresolved dialogues need cleanup. A correlated negative or
		// file-deactivated indication is already a clean end, not another request.
		if e := r.c.Send(fileMessage(r.key, FileCall{Name: r.key.Name, Qualifier: DeactivateFile}, FileTransfer)); e != nil {
			r.c.RequestClose()
		}
	}
	return err
}
func (r *fileReceiver) send(ctx context.Context, e Element, cot COT) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := r.c.sendProcedure(r.t, fileMessage(r.key, e, cot), func(m Message) bool { return fileMatch(r.key, m) })
	if err != nil {
		return err
	}
	r.admitted = true
	r.last = e
	r.t.mu.Lock()
	if call, ok := e.(FileCall); ok && (call.Qualifier == CallFile || call.Qualifier == CallSection) {
		r.t.file.called = true
	}
	r.t.mu.Unlock()
	// Every new leg must reach its complete Write before success is possible,
	// including final ACKs which do not receive another application confirmation.
	for {
		r.t.mu.Lock()
		err = r.t.err
		written := r.t.leg.written
		r.t.mu.Unlock()
		if err != nil {
			return err
		}
		if written {
			return nil
		}
		select {
		case <-r.t.wake:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Directory calls the directory at an explicit CA/OA/IOA. The profile sends a
// zero NOF/NOS/SCQ tuple, and completes only on SOF.LFD, never an ASDU boundary.
func (c *Client) Directory(ctx context.Context, k FileKey) (entries []FileDirectory, err error) {
	k.Name = 0
	r, err := c.beginFile(ctx, k)
	if err != nil {
		return nil, err
	}
	defer func() { err = r.finish(err) }()
	ctx, cancel := context.WithTimeout(ctx, c.opts.Files.Timeout)
	defer cancel()
	if err = r.send(ctx, FileCall{}, Request); err != nil {
		return nil, err
	}
	for {
		m, e := r.t.next(ctx)
		if e != nil {
			return nil, e
		}
		if e = responseError(m); e != nil {
			return nil, r.headerNegative(m, e)
		}
		a := m.(*ASDU)
		if a.Header.Type == F_SC_NA_1 && a.Header.Cause == Request {
			q := a.Objects[0].Element.(FileCall).Qualifier
			if q>>4 != 0 {
				call := a.Objects[0].Element.(FileCall)
				if !r.correlatedNegative(call) {
					return nil, ErrProcedureOrder
				}
				return nil, r.cleanEnd(&FileResponseError{q >> 4, q})
			}
		}
		if a.Header.Type != F_DR_TA_1 || a.Header.Cause != Request {
			return nil, ErrProcedureOrder
		}
		for i, o := range a.Objects {
			d := o.Element.(FileDirectory)
			entries = append(entries, d)
			if len(entries) > c.opts.Files.DirectoryLimit {
				return nil, ErrQueueFull
			}
			if d.Status&0x20 != 0 {
				if i != len(a.Objects)-1 {
					return nil, ErrProcedureOrder
				}
				return entries, nil
			}
		}
	}
}

// DownloadFile selects/calls a transparent file, verifies section and whole-file
// checksums, and acknowledges its final indication. Uncertain failures require
// reconnect; clean correlated negatives do not. Memory is capped by
// Options.Files.MaxFileSize.
func (c *Client) DownloadFile(ctx context.Context, k FileKey) (file File, err error) {
	r, err := c.beginFile(ctx, k)
	if err != nil {
		return file, err
	}
	defer func() { err = r.finish(err) }()
	if err = r.exchange(ctx, FileCall{Name: k.Name, Qualifier: SelectFile}); err != nil {
		return file, err
	}
	e, err := r.wait(ctx)
	if err != nil {
		return file, err
	}
	ready, ok := e.(FileReady)
	if !ok {
		return file, ErrProcedureOrder
	}
	if ready.Qualifier != 0 || ready.Length == 0 || ready.Length > c.opts.Files.MaxFileSize {
		return file, ErrProcedureOrder
	}
	if err = r.exchange(ctx, FileCall{Name: k.Name, Qualifier: CallFile}); err != nil {
		return file, err
	}
	var total uint32
	var sum uint8
	seen := map[uint8]bool{}
	for {
		e, err = r.wait(ctx)
		if err != nil {
			return file, err
		}
		switch v := e.(type) {
		case SectionReady:
			if v.Qualifier != 0 || v.Section == 0 || seen[v.Section] || v.Length == 0 || uint64(total)+uint64(v.Length) > uint64(ready.Length) {
				return file, ErrProcedureOrder
			}
			data, e := r.section(ctx, v)
			if e != nil {
				return file, e
			}
			seen[v.Section] = true
			file.Sections = append(file.Sections, FileSection{v.Section, data})
			total += uint32(len(data))
			sum += FileChecksum(data)
			if err = r.exchange(ctx, FileACK{k.Name, v.Section, PositiveSectionACK}); err != nil {
				return file, err
			}
		case FileLast:
			if v.Qualifier != 1 || len(file.Sections) == 0 || total != ready.Length {
				return file, ErrProcedureOrder
			}
			last := file.Sections[len(file.Sections)-1].Number
			if v.Checksum != sum {
				if err := r.exchange(ctx, FileACK{k.Name, last, NegativeFileACK | FileChecksumFailure<<4}); err != nil {
					return file, err
				}
				return file, ErrFileChecksum
			}
			err = r.exchange(ctx, FileACK{k.Name, last, PositiveFileACK})
			return file, err
		default:
			return file, ErrProcedureOrder
		}
	}
}

// Each application exchange has its own timeout, independent of TCP/APCI timers.
func (r *fileReceiver) exchange(ctx context.Context, e Element) error {
	ctx, cancel := context.WithTimeout(ctx, r.c.opts.Files.Timeout)
	defer cancel()
	return r.send(ctx, e, FileTransfer)
}
func (r *fileReceiver) wait(ctx context.Context) (Element, error) {
	ctx, cancel := context.WithTimeout(ctx, r.c.opts.Files.Timeout)
	defer cancel()
	return r.next(ctx)
}
func (r *fileReceiver) section(ctx context.Context, ready SectionReady) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		if err := r.exchange(ctx, FileCall{r.key.Name, ready.Section, CallSection}); err != nil {
			return nil, err
		}
		data, err := r.sectionContent(ctx, ready)
		if err != ErrFileChecksum {
			return data, err
		}
		if err := r.exchange(ctx, FileACK{r.key.Name, ready.Section, NegativeSectionACK | FileChecksumFailure<<4}); err != nil {
			return nil, err
		}
		if attempt >= r.c.opts.Files.SectionRetries {
			return nil, ErrFileChecksum
		}
	}
}

func (r *fileReceiver) sectionContent(ctx context.Context, ready SectionReady) ([]byte, error) {
	var data []byte
	for {
		e, err := r.wait(ctx)
		if err != nil {
			return nil, err
		}
		switch v := e.(type) {
		case FileSegment:
			if v.Section != ready.Section || len(v.Data) == 0 || uint64(len(data))+uint64(len(v.Data)) > uint64(ready.Length) {
				return nil, ErrProcedureOrder
			}
			data = append(data, v.Data...)
		case FileLast:
			if v.Section != ready.Section || v.Qualifier != 3 || uint32(len(data)) != ready.Length {
				return nil, ErrProcedureOrder
			}
			if FileChecksum(data) != v.Checksum {
				return nil, ErrFileChecksum
			}
			return data, nil
		default:
			return nil, ErrProcedureOrder
		}
	}
}
