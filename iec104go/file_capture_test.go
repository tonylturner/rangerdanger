// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"encoding/binary"
	"os"
	"sync"
	"testing"
	"time"
)

// TestFileCapture writes only when explicitly requested. The capture contains
// actual Go-peer APDUs from taps, in a synthetic TCP/Ethernet envelope (not a
// claim to capture kernel TCP). A second stream contains all eight wire goldens.
// This keeps capture generation reproducible without raw sockets or GUI tools.
func TestFileCapture(t *testing.T) {
	var mu sync.Mutex
	var wire []captureAPDU
	var leftID uint64
	opts := fileTestOptions()
	opts.Tap = func(dir Direction, id uint64, _ time.Time, b []byte) {
		if dir != Transmit {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		wire = append(wire, captureAPDU{id, b})
	}
	p := memoryFiles{file: File{[]FileSection{{1, bytes.Repeat([]byte{1, 2, 255}, 180)}, {2, []byte{5, 6, 7}}}}, entries: []FileDirectory{{Name: 9, Length: 543}}}
	c, s, _ := filePair(t, p, opts, nil)
	leftID = c.ID()
	if _, err := c.Directory(testContext(t), testFileKey); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DownloadFile(testContext(t), testFileKey); err != nil {
		t.Fatal(err)
	}
	// Confirm the worker processed final ACK before collecting the final taps.
	if err := c.StopDT(testContext(t)); err != nil {
		t.Fatal(err)
	}
	closeSession(t, c.Session)
	closeSession(t, s)
	mu.Lock()
	defer mu.Unlock()
	capture := newPCAP()
	stream := captureTCP{clientPort: 34001, seq: [2]uint32{101, 501}}
	stream.handshake(capture)
	for _, w := range wire {
		side := 0
		if w.id != leftID {
			side = 1
		}
		stream.packet(capture, side, 0x18, w.b)
	}
	stream = captureTCP{clientPort: 34002, seq: [2]uint32{101, 501}, packets: stream.packets}
	stream.handshake(capture)
	for i, v := range fileGoldens(t) {
		b, err := (Frame{Kind: IFrame, Send: uint16(i), ASDU: fileGoldenASDU(t, v)}).MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		stream.packet(capture, 0, 0x18, b)
	}
	if path := os.Getenv("IEC104_FILE_CAPTURE"); path != "" {
		if err := os.WriteFile(path, capture.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

type captureAPDU struct {
	id uint64
	b  []byte
}
type captureTCP struct {
	clientPort uint16
	seq        [2]uint32
	packets    uint32
}

func newPCAP() *bytes.Buffer {
	b := new(bytes.Buffer)
	for _, v := range []uint32{0xa1b2c3d4, 0x00040002, 0, 0, 65535, 1} {
		_ = binary.Write(b, binary.LittleEndian, v)
	}
	return b
}
func (s *captureTCP) handshake(b *bytes.Buffer) {
	s.seq = [2]uint32{100, 500}
	s.packet(b, 0, 2, nil)
	s.packet(b, 1, 0x12, nil)
	s.packet(b, 0, 0x10, nil)
}
func (s *captureTCP) packet(b *bytes.Buffer, side int, flags byte, payload []byte) {
	ip := make([]byte, 20)
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:], uint16(40+len(payload)))
	ip[8] = 64
	ip[9] = 6
	ip[12], ip[13], ip[14], ip[15] = 192, 0, 2, byte(1+side)
	ip[16], ip[17], ip[18], ip[19] = 192, 0, 2, byte(2-side)
	binary.BigEndian.PutUint16(ip[10:], internetChecksum(ip))
	tcp := make([]byte, 20)
	ports := [2]uint16{s.clientPort, 2404}
	binary.BigEndian.PutUint16(tcp, ports[side])
	binary.BigEndian.PutUint16(tcp[2:], ports[1-side])
	binary.BigEndian.PutUint32(tcp[4:], s.seq[side])
	if flags&0x10 != 0 {
		binary.BigEndian.PutUint32(tcp[8:], s.seq[1-side])
	}
	tcp[12] = 0x50
	tcp[13] = flags
	binary.BigEndian.PutUint16(tcp[14:], 65535)
	pseudo := append([]byte(nil), ip[12:20]...)
	pseudo = append(pseudo, 0, 6, byte((20+len(payload))>>8), byte(20+len(payload)))
	pseudo = append(pseudo, tcp...)
	pseudo = append(pseudo, payload...)
	binary.BigEndian.PutUint16(tcp[16:], internetChecksum(pseudo))
	eth := []byte{0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 1, 8, 0}
	packet := append(append(append(eth, ip...), tcp...), payload...)
	for _, v := range []uint32{1792790000, s.packets * 1000, uint32(len(packet)), uint32(len(packet))} {
		_ = binary.Write(b, binary.LittleEndian, v)
	}
	_, _ = b.Write(packet)
	s.packets++
	s.seq[side] += uint32(len(payload))
	if flags&3 != 0 {
		s.seq[side]++
	}
}
func internetChecksum(b []byte) uint16 {
	var sum uint32
	for len(b) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(b))
		b = b[2:]
	}
	if len(b) > 0 {
		sum += uint32(b[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 65535) + (sum >> 16)
	}
	return ^uint16(sum)
}

func TestFileCaptureFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/file-transfer.pcap")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 24 || binary.LittleEndian.Uint32(b) != 0xa1b2c3d4 {
		t.Fatal("not generated little-endian pcap")
	}
	b = b[24:]
	seen := map[TypeID]bool{}
	apdus := 0
	for len(b) > 0 {
		if len(b) < 16 {
			t.Fatal("truncated record")
		}
		n := int(binary.LittleEndian.Uint32(b[8:]))
		b = b[16:]
		if n < 54 || n > len(b) {
			t.Fatal("bad record length")
		}
		packet := b[:n]
		b = b[n:]
		if n == 54 {
			continue
		}
		f, err := DecodeFrame(packet[54:])
		if err != nil {
			t.Fatal(err)
		}
		apdus++
		if f.Kind == IFrame {
			m, err := DecodeASDU(f.ASDU, DefaultParams())
			if err != nil {
				t.Fatal(err)
			}
			seen[m.GetHeader().Type] = true
			if m.GetHeader().Type == F_DR_TA_1 && m.GetHeader().Cause != Request {
				t.Fatal("positive directory capture must use COT5")
			}
		}
	}
	if apdus < 8 {
		t.Fatal("missing peer exchange")
	}
	for id := TypeID(120); id <= 127; id++ {
		if !seen[id] {
			t.Fatal("capture missing", id)
		}
	}
}
