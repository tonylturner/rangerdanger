// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
)

// All transaction fields are guarded by transaction.mu. Only the initiating
// helper changes called/cleanEnd; the atomic finalizer records uncertain.
type fileTransaction struct {
	called, cleanEnd, uncertain bool
}

// fileOutcome runs under the observer/finalizer locks. Buffered traffic after a
// purported clean end is an ordering violation, and recorded overflow always wins.
func (t *transaction) fileOutcome(err error) (error, bool) {
	if t.err != nil {
		return t.err, true
	}
	if len(t.responses) != 0 && (err == nil || t.file.cleanEnd) {
		return ErrProcedureOrder, true
	}
	if err == nil {
		return nil, false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrProcedureOrder) {
		return err, true
	}
	return err, !t.file.cleanEnd && t.file.called
}

func (r *fileReceiver) cleanEnd(err error) error {
	r.t.mu.Lock()
	r.t.file.cleanEnd = true
	r.t.mu.Unlock()
	return err
}

func (r *fileReceiver) correlatedNegative(e Element) bool {
	name, ok := fileElementName(e)
	if !ok || name != r.key.Name {
		return false
	}
	switch v := e.(type) {
	case FileCall:
		switch last := r.last.(type) {
		case FileCall:
			return v.Section == last.Section && v.Qualifier&15 == last.Qualifier&15
		case FileACK:
			return v.Section == 0 && v.Qualifier&15 == 0 // F16 non-SCQ rejection
		}
	case FileReady:
		last, ok := r.last.(FileCall)
		return ok && last.Qualifier == SelectFile
	case SectionReady:
		last, ok := r.last.(FileCall)
		return ok && last.Qualifier == CallFile
	case FileACK:
		last, ok := r.last.(FileACK)
		return ok && last.Section == v.Section && (v.Qualifier&15 == last.Qualifier || v.Qualifier&15 == last.Qualifier+1)
	case FileLast:
		return v.Qualifier == 2
	}
	return false
}

func (r *fileReceiver) headerNegative(m Message, err error) error {
	a, ok := m.(*ASDU)
	if !ok || len(a.Objects) != 1 || !r.correlatedNegative(a.Objects[0].Element) {
		return ErrProcedureOrder
	}
	return r.cleanEnd(err)
}

func (r *fileReceiver) next(ctx context.Context) (Element, error) {
	m, err := r.t.next(ctx)
	if err != nil {
		return nil, err
	}
	if err = responseError(m); err != nil {
		return nil, r.headerNegative(m, err)
	}
	a := m.(*ASDU)
	if a.Header.Cause != FileTransfer || len(a.Objects) != 1 {
		return nil, ErrProcedureOrder
	}
	e := a.Objects[0].Element
	name, ok := fileElementName(e)
	if !ok || name != r.key.Name {
		return nil, ErrProcedureOrder
	}
	var negative error
	switch v := e.(type) {
	case FileCall:
		if v.Qualifier>>4 != 0 {
			negative = &FileResponseError{v.Qualifier >> 4, v.Qualifier}
		}
	case FileACK:
		if v.Qualifier&15 == NegativeFileACK || v.Qualifier&15 == NegativeSectionACK {
			negative = &FileResponseError{v.Qualifier >> 4, v.Qualifier}
		}
	case FileReady:
		if v.Qualifier&128 != 0 {
			negative = &FileResponseError{Qualifier: v.Qualifier}
		}
	case SectionReady:
		if v.Qualifier&128 != 0 {
			negative = &FileResponseError{Qualifier: v.Qualifier}
		}
	case FileLast:
		if v.Qualifier == 2 {
			return nil, r.cleanEnd(ErrFileAborted)
		}
		if v.Qualifier == 4 {
			return nil, ErrFileAborted // section abort alone does not deactivate the file
		}
	}
	if negative != nil {
		if !r.correlatedNegative(e) {
			return nil, ErrProcedureOrder
		}
		return nil, r.cleanEnd(negative)
	}
	return e, nil
}
