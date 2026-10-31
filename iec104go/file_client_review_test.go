// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func fileClientPeer(t *testing.T, handler Handler, observer Handler) (*Client, *Session) {
	t.Helper()
	a, b := net.Pipe()
	right, err := NewSession(context.Background(), b, ServerRole, fileTestOptions(), handler)
	if err != nil {
		t.Fatal(err)
	}
	left, err := NewSession(context.Background(), a, ClientRole, fileTestOptions(), observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSession(t, left); closeSession(t, right) })
	if err := left.StartDT(testContext(t)); err != nil {
		t.Fatal(err)
	}
	return &Client{left}, right
}

func TestFileProcedureCleanNegatives(t *testing.T) {
	for _, tc := range []struct {
		name      string
		leg       uint8
		directory bool
		response  Element
		want      error
	}{
		{"directory", 0, true, FileCall{0, 0, FileMemoryUnavailable << 4}, ErrNegativeResponse},
		{"select ready", SelectFile, false, FileReady{Name: 9, Qualifier: 128}, ErrNegativeResponse},
		{"select SCQ", SelectFile, false, FileCall{9, 0, SelectFile | FileUnknownName<<4}, ErrNegativeResponse},
		{"call file SCQ", CallFile, false, FileCall{9, 0, CallFile | FileUnexpectedService<<4}, ErrNegativeResponse},
		{"call section SCQ", CallSection, false, FileCall{9, 2, CallSection | FileUnknownSection<<4}, ErrNegativeResponse},
		{"peer deactivated", CallSection, false, FileLast{Name: 9, Qualifier: 2}, ErrFileAborted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var deactivated atomic.Int32
			c, _ := fileClientPeer(t, func(_ context.Context, e Event) {
				if e.Kind != ASDUReceived {
					return
				}
				if e.ASDU.GetHeader().Type == C_IC_NA_1 {
					reply(t, e.Session, e.ASDU, ActivationCon, false)
					reply(t, e.Session, e.ASDU, ActivationTerm, false)
					return
				}
				call, ok := e.ASDU.(*ASDU).Objects[0].Element.(FileCall)
				if !ok {
					return
				}
				if call.Qualifier == DeactivateFile {
					deactivated.Add(1)
					return
				}
				if call.Qualifier == tc.leg {
					k := testFileKey
					if tc.directory {
						k.Name = 0
					}
					if err := e.Session.Send(fileMessage(k, tc.response, e.ASDU.GetHeader().Cause)); err != nil {
						t.Error(err)
					}
				} else if call.Qualifier == SelectFile {
					sendFileResponse(t, e.Session, FileReady{Name: 9, Length: 1}, FileTransfer)
				} else if call.Qualifier == CallFile {
					sendFileResponse(t, e.Session, SectionReady{9, 2, 1, 0}, FileTransfer)
				}
			}, nil)
			var err error
			if tc.directory {
				_, err = c.Directory(testContext(t), testFileKey)
			} else {
				_, err = c.DownloadFile(testContext(t), testFileKey)
			}
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			// A real unrelated procedure proves detach and admission, not just
			// a private flag. Its callback follows any erroneous deactivate.
			if _, err := c.Interrogate(testContext(t), 1, 7, 20); err != nil {
				t.Fatal("clean negative fenced later procedure", err)
			}
			if n := deactivated.Load(); n != 0 {
				t.Fatal("clean negative caused deactivation", n)
			}
		})
	}
}

type fileEndSectionConn struct {
	net.Conn
	section uint8
}

func (c fileEndSectionConn) Write(b []byte) (int, error) {
	if len(b) >= 20 && b[2]&1 == 0 && TypeID(b[6]) == F_LS_NA_1 && b[18] == 1 {
		b = append([]byte(nil), b...)
		b[17] = c.section
	}
	return c.Conn.Write(b)
}

func TestFileProcedureAcceptsAnyFileEndSection(t *testing.T) {
	for _, section := range []uint8{0, 255} {
		t.Run(fmt.Sprint(section), func(t *testing.T) {
			opts := fileTestOptions()
			var acknowledged atomic.Uint32
			opts.Tap = func(dir Direction, _ uint64, _ time.Time, b []byte) {
				if dir == Transmit && len(b) >= 19 && b[2]&1 == 0 && TypeID(b[6]) == F_AF_NA_1 && b[18] == PositiveFileACK {
					acknowledged.Store(uint32(b[17]))
				}
			}
			p := memoryFiles{file: File{[]FileSection{{2, []byte{7}}}}}
			c, _, _ := filePair(t, p, opts, func(conn net.Conn) net.Conn {
				return fileEndSectionConn{conn, section}
			})
			for i := 0; i < 2; i++ {
				if _, err := c.DownloadFile(testContext(t), testFileKey); err != nil {
					t.Fatal("arbitrary file-end NOS rejected", err)
				}
				if acknowledged.Load() != 2 {
					t.Fatal("ACK must emit last section number", acknowledged.Load())
				}
			}
		})
	}
}

func sendFileResponse(t *testing.T, s *Session, e Element, cause COT) {
	t.Helper()
	if err := s.Send(fileMessage(testFileKey, e, cause)); err != nil {
		t.Error(err)
	}
}

func TestFileProcedureIgnoresDirectoryAnnouncements(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "download"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			var observed atomic.Int32
			c, _ := fileClientPeer(t, func(_ context.Context, e Event) {
				if e.Kind != ASDUReceived {
					return
				}
				s := e.Session
				announce := func() {
					sendFileResponse(t, s, FileDirectory{Name: 77, Status: 32}, Spontaneous)
					sendFileResponse(t, s, FileDirectory{Name: 78, Status: 32}, Background)
				}
				announce()
				switch v := e.ASDU.(*ASDU).Objects[0].Element.(type) {
				case FileCall:
					switch v.Qualifier {
					case 0:
						sendFileResponse(t, s, FileDirectory{Name: 9, Length: 1, Status: 32}, Request)
					case SelectFile:
						sendFileResponse(t, s, FileReady{Name: 9, Length: 1}, FileTransfer)
					case CallFile:
						sendFileResponse(t, s, SectionReady{9, 2, 1, 0}, FileTransfer)
					case CallSection:
						sendFileResponse(t, s, FileSegment{9, 2, []byte{7}}, FileTransfer)
						sendFileResponse(t, s, FileLast{9, 2, 3, 7}, FileTransfer)
					}
				case FileACK:
					if v.Qualifier == PositiveSectionACK {
						sendFileResponse(t, s, FileLast{9, 2, 1, 7}, FileTransfer)
					}
				}
				// These straddle the end of a leg and the next request's write.
				announce()
			}, func(_ context.Context, e Event) {
				if e.Kind == ASDUReceived && e.ASDU.GetHeader().Type == F_DR_TA_1 && e.ASDU.GetHeader().Cause != Request {
					observed.Add(1)
				}
			})
			if directory {
				if _, err := c.Directory(testContext(t), testFileKey); err != nil {
					t.Fatal(err)
				}
			} else if _, err := c.DownloadFile(testContext(t), testFileKey); err != nil {
				t.Fatal(err)
			}
			want := int32(20) // select, call-file, call-section, section ACK, file ACK
			if directory {
				want = 4
			}
			eventually(t, func() bool { return observed.Load() == want })
		})
	}
}
