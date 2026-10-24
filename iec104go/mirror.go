// SPDX-License-Identifier: Apache-2.0

package iec104go

import "fmt"

// Mirror returns an independently owned response, changing only Cause and
// Negative. OA, CA, type, SQ, test, addresses, values and raw bytes are retained.
// No rejection policy or station behavior is implied by constructing a mirror.
func Mirror(m Message, cot COT, negative bool) (Message, error) {
	if cot > 63 {
		return nil, errFieldRange
	}
	copy, err := cloneMessage(m)
	if err != nil {
		return nil, err
	}
	switch a := copy.(type) {
	case *ASDU:
		a.Header.Cause = cot
		a.Header.Negative = negative
	case *RawASDU:
		a.Header.Cause = cot
		a.Header.Negative = negative
	}
	return copy, nil
}
func cloneMessage(m Message) (Message, error) {
	switch a := m.(type) {
	case ASDU:
		return cloneASDU(a)
	case *ASDU:
		if a != nil {
			return cloneASDU(*a)
		}
	case RawASDU:
		a.Payload = append([]byte(nil), a.Payload...)
		return &a, nil
	case *RawASDU:
		if a != nil {
			b := *a
			b.Payload = append([]byte(nil), a.Payload...)
			return &b, nil
		}
	}
	return nil, fmt.Errorf("%w: message must be ASDU or RawASDU", ErrMalformed)
}
func cloneASDU(a ASDU) (Message, error) {
	for _, o := range a.Objects {
		if _, _, err := encodeElement(o.Element); err != nil {
			return nil, err
		}
	}
	a.Objects = cloneObjects(a.Objects)
	a.Header = a.GetHeader()
	return &a, nil
}

func cloneObject(o Object) Object {
	if e, ok := o.Element.(FileSegment); ok {
		e.Data = append([]byte(nil), e.Data...)
		o.Element = e
	}
	return o
}
func cloneObjects(in []Object) []Object {
	out := make([]Object, len(in))
	for i, o := range in {
		out[i] = cloneObject(o)
	}
	return out
}
