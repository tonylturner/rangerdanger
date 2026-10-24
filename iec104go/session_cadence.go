// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"slices"
	"time"
)

// ReceiveFilter declares a server's static application receive profile. Each
// empty list accepts every value. Clients ignore the filter. Options takes a
// copy of both lists; subsequent caller mutations cannot change a live profile.
// Library file-service types are outside ApplicationTypes; CA filtering still
// applies to them. U/S control frames are not ASDUs and never enter this filter.
type ReceiveFilter struct {
	CommonAddresses  []uint16
	ApplicationTypes []TypeID
}

func (f ReceiveFilter) reject(h Header) COT {
	// The sealed reference checks CA before application type (receive-filter).
	if len(f.CommonAddresses) != 0 && !slices.Contains(f.CommonAddresses, h.CommonAddress) {
		return UnknownCA
	}
	if !isFileType(h.Type) && len(f.ApplicationTypes) != 0 && !slices.Contains(f.ApplicationTypes, h.Type) {
		return UnknownType
	}
	return 0
}

func (st *sessionState) sendI(b []byte, leg *procedureLeg) error {
	if err := st.emitLeg(Frame{Kind: IFrame, Send: st.sent, Receive: st.received, ASDU: b}, false, leg); err != nil {
		return err
	}
	if st.outstanding() == 0 {
		st.t1 = st.s.opts.Clock.Now().Add(st.s.opts.APCI.T1)
	}
	st.sent = (st.sent + 1) & 32767
	st.pending = 0
	st.t2 = time.Time{}
	return nil
}

func (st *sessionState) reject(request []byte, cause COT) error {
	// Preserve VSQ, OA, Test, CA, IOAs and opaque payload byte for byte. Only
	// the cause/P-N octet changes. Rejection is not an application Send.
	mirror := append([]byte(nil), request...)
	mirror[2] = request[2]&0x80 | 0x40 | byte(cause)
	if st.outstanding() < st.s.opts.APCI.K {
		return st.sendI(mirror, nil)
	}
	// No send-window credit: release the transport ACK now, then retain the
	// mirror in bounded admission until the peer acknowledges outstanding data.
	if err := st.ack(false); err != nil {
		return err
	}
	return st.s.admit(mirror)
}
