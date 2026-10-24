// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"errors"
	"fmt"
)

// ErrFull means the next object would exceed 249 ASDU bytes or the type count
// limit (127 generally, one for non-directory file ASDUs).
// A builder leaves its contents unchanged on this or any other append error.
var ErrFull = errors.New("iec104go: ASDU full")

// Builder packs homogeneous objects until the ASDU is full. It is not safe for
// concurrent mutation. An empty builder is not itself a transmittable ASDU.
type Builder struct {
	params Params
	asdu   ASDU
	size   int
}

// NewBuilder validates the profile and envelope, ignoring Header.Count because
// the builder derives it from accepted objects. Unsupported types cannot be
// built with typed objects; use RawASDU when mirroring one instead.
func NewBuilder(p Params, h Header) (*Builder, error) {
	h.Count = 1
	if _, err := encodeHeader(p, h); err != nil {
		return nil, err
	}
	if !h.Type.Supported() {
		return nil, ErrUnsupportedType
	}
	if err := fileStructure(h); err != nil {
		return nil, err
	}
	h.Count = 0
	return &Builder{params: p, asdu: ASDU{Header: h}, size: p.headerSize()}, nil
}

// Append validates and adds an object. On ErrFull the caller can send ASDU(),
// create another builder and retry the same object, without losing any data.
func (b *Builder) Append(o Object) error {
	id, payload, err := encodeElement(o.Element)
	if err != nil {
		return err
	}
	if id != b.asdu.Header.Type {
		return fmt.Errorf("%w: mixed element types", ErrMalformed)
	}
	if o.Address > maxAddress(b.params.IOASize) {
		return errFieldRange
	}
	count := len(b.asdu.Objects)
	if b.asdu.Header.Sequence && count > 0 && o.Address != objectAddress(id, b.asdu.Objects[0].Address, count) {
		return fmt.Errorf("%w: SQ address discontinuity", ErrMalformed)
	}
	size := len(payload)
	if !b.asdu.Header.Sequence || count == 0 {
		size += b.params.IOASize
	}
	if count == 127 || (isFileType(id) && id != F_DR_TA_1 && count == 1) || b.size+size > MaxASDUSize {
		return ErrFull
	}
	b.asdu.Objects = append(b.asdu.Objects, cloneObject(o))
	b.size += size
	return nil
}

// ASDU returns an independently owned snapshot; later appends cannot change it.
func (b *Builder) ASDU() ASDU {
	a := b.asdu
	a.Objects = cloneObjects(a.Objects)
	a.Header = a.GetHeader()
	return a
}
