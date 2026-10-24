// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
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
