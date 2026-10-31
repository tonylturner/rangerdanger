// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// FileOptions contains conservative application-profile choices, not APCI timers.
// Zero limits/durations select defaults; SectionRetries=0 disables retries.
type FileOptions struct {
	// Timeout bounds one application exchange/provider call and idle serving state.
	Timeout time.Duration
	// SectionRetries bounds checksum-driven section recalls; it never retries timeouts.
	SectionRetries int
	// MaxFileSize caps owned content bytes and must fit uint24.
	MaxFileSize uint32
	// DirectoryLimit caps directory entries held by a procedure/provider.
	DirectoryLimit int
	// SegmentSize limits outbound section data bytes per segment. Zero uses the
	// maximum derived from Params and the 249-byte ASDU limit; larger values fail
	// Options validation. It does not restrict decoding incoming segments.
	SegmentSize int
}

// DefaultFileOptions returns 30s application timeout, no section retries,
// 16777215 file bytes and 4096 directory entries.
func DefaultFileOptions() FileOptions {
	return FileOptions{Timeout: 30 * time.Second, MaxFileSize: 0xffffff, DirectoryLimit: 4096}
}
func (o FileOptions) normalized() (FileOptions, error) {
	d := DefaultFileOptions()
	if o.Timeout == 0 {
		o.Timeout = d.Timeout
	}
	if o.MaxFileSize == 0 {
		o.MaxFileSize = d.MaxFileSize
	}
	if o.DirectoryLimit == 0 {
		o.DirectoryLimit = d.DirectoryLimit
	}
	if o.Timeout < 0 || o.SectionRetries < 0 || o.SectionRetries > 10 || o.MaxFileSize > 0xffffff || o.DirectoryLimit < 1 || o.SegmentSize < 0 {
		return o, fmt.Errorf("%w: file options", ErrMalformed)
	}
	return o, nil
}

func (o FileOptions) segmentSize(p Params) int {
	if o.SegmentSize != 0 {
		return o.SegmentSize
	}
	return MaxASDUSize - p.headerSize() - p.IOASize - 4
}

// FileKey supplies explicit addressing; directory requests use Name=0.
type FileKey struct {
	CommonAddress uint16
	Originator    uint8
	Address       uint32
	Name          uint16
}

// FileSection owns one nonempty section; Number is nonzero and unique in a file.
type FileSection struct {
	Number uint8
	Data   []byte
}

// File is an ordered, bounded snapshot of transparent content. No payload format
// (zip, disturbance records or archives) is imposed by the library.
type File struct{ Sections []FileSection }

// FileProvider supplies snapshots, without coupling the library to a filesystem.
// Methods must honor ctx. Returned slices transfer ownership to the serving worker;
// the provider must not mutate them afterwards. Calls on different links may overlap.
type FileProvider interface {
	Directory(context.Context, FileKey) ([]FileDirectory, error)
	Open(context.Context, FileKey) (File, error)
}

var (
	// ErrFileNotFound is a provider convention for unavailable content. It has no
	// special internal branch: all provider errors reject selection with FRQ0x80
	// or a directory call with SCQ memory-unavailable.
	ErrFileNotFound = errors.New("iec104go: file not found")
	// ErrFileChecksum means received content failed the section or file checksum.
	ErrFileChecksum = errors.New("iec104go: file checksum failure")
	// ErrFileAborted means the remote station deactivated the transfer.
	ErrFileAborted = errors.New("iec104go: file transfer aborted")
)

// FileResponseError exposes a verified SCQ/AFQ error nibble or ready rejection.
// Code=0 means no additional error was supplied, not a successful response.
type FileResponseError struct{ Code, Qualifier uint8 }

// Error describes the peer qualifier and its optional error nibble.
func (e *FileResponseError) Error() string {
	return fmt.Sprintf("iec104go: negative file response code=%d qualifier=%02x", e.Code, e.Qualifier)
}

// Unwrap classifies file negatives with the existing procedure negative sentinel.
func (e *FileResponseError) Unwrap() error { return ErrNegativeResponse }
func fileMessage(k FileKey, e Element, cot COT) ASDU {
	return ASDU{Header: Header{Type: e.elementType(), Sequence: e.elementType() == F_DR_TA_1, Cause: cot, CommonAddress: k.CommonAddress, Originator: k.Originator}, Objects: []Object{{Address: k.Address, Element: e}}}
}
func fileMatch(k FileKey, m Message) bool {
	h := m.GetHeader()
	// Directory change announcements are asynchronous station traffic, not a
	// response to whichever file leg happens to be awaiting confirmation.
	if h.Type == F_DR_TA_1 && h.Cause != Request {
		return false
	}
	a, ok := m.(*ASDU)
	return ok && h.CommonAddress == k.CommonAddress && h.Originator == k.Originator && !h.Test && (isFileType(h.Type) || h.Cause >= UnknownType && h.Cause <= UnknownIOA) && len(a.Objects) > 0 && a.Objects[0].Address == k.Address
}
func fileLength(f File, max uint32) (uint32, error) {
	var n uint32
	seen := map[uint8]bool{}
	if len(f.Sections) == 0 {
		return 0, ErrMalformed
	}
	for _, s := range f.Sections {
		if s.Number == 0 || seen[s.Number] || len(s.Data) == 0 || uint64(n)+uint64(len(s.Data)) > uint64(max) {
			return 0, ErrMalformed
		}
		seen[s.Number] = true
		n += uint32(len(s.Data))
	}
	return n, nil
}
func fileSend(ctx context.Context, s *Session, generation *transferGeneration, m Message) error {
	owned, err := cloneMessage(m)
	if err != nil {
		return err
	}
	b, err := owned.MarshalBinary(s.opts.Params)
	if err != nil {
		return err
	}
	for {
		s.mu.Lock()
		ready := s.sendReady
		err := s.admitFileLocked(ctx, generation, b)
		s.mu.Unlock()
		if !errors.Is(err, ErrQueueFull) {
			return err
		}
		select {
		case <-ready:
		case <-generation.ctx.Done():
			return ErrNotStarted
		case <-ctx.Done():
			return ctx.Err()
		case <-s.transportDone:
			return ErrConnectionGone
		}
	}
}
