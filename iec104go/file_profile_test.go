// SPDX-License-Identifier: Apache-2.0

package iec104go

import "testing"

func TestFileServerDirectoryPlaceholders(t *testing.T) {
	fs, _ := NewFileServer(memoryFiles{entries: []FileDirectory{{Name: 9, Length: 1}}})
	s, peer, _ := manualSession(t, ServerRole, fileTestOptions(), fs.Handler)
	startServerSession(t, s, peer)
	fileRequestPeer(t, peer, 0, 0, testFileKey, FileCall{Name: 0xffff, Section: 255}, Request)
	if e, ok := filePeerResponse(t, peer).(FileDirectory); !ok || e.Name != 9 || e.Status&32 == 0 {
		t.Fatal("directory placeholders rejected", e)
	}
}

func TestFileDirectoryZeroLength(t *testing.T) {
	c, _, _ := filePair(t, memoryFiles{entries: []FileDirectory{{Name: 9, Status: 64}}}, fileTestOptions(), nil)
	entries, err := c.Directory(testContext(t), testFileKey)
	if err != nil || len(entries) != 1 || entries[0].Length != 0 || entries[0].Status != 96 {
		t.Fatal("zero-length directory entry rejected", entries, err)
	}
}

func TestFileServerNegativeIdentityAndService(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected bool
		request  Element
		want     FileCall
	}{
		{"idle ack", false, FileACK{9, 2, PositiveSectionACK}, FileCall{9, 0, FileUnknownName << 4}},
		{"unknown ack file", true, FileACK{10, 2, PositiveSectionACK}, FileCall{10, 0, FileUnknownName << 4}},
		{"unknown ack section", true, FileACK{9, 3, PositiveSectionACK}, FileCall{9, 0, FileUnknownSection << 4}},
		{"out of order ack", true, FileACK{9, 2, PositiveSectionACK}, FileCall{9, 0, FileUnexpectedService << 4}},
		{"select while busy", true, FileCall{10, 0, SelectFile}, FileCall{10, 0, SelectFile | FileUnexpectedService<<4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, _ := NewFileServer(memoryFiles{file: File{[]FileSection{{2, []byte{1}}}}})
			s, peer, _ := manualSession(t, ServerRole, fileTestOptions(), fs.Handler)
			startServerSession(t, s, peer)
			var n uint16
			if tc.selected {
				fileRequestPeer(t, peer, n, n, testFileKey, FileCall{Name: 9, Qualifier: SelectFile}, FileTransfer)
				filePeerResponse(t, peer)
				n++
			}
			fileRequestPeer(t, peer, n, n, testFileKey, tc.request, FileTransfer)
			got := filePeerResponse(t, peer)
			if got != tc.want {
				t.Fatalf("response=%#v, want SCQ %#v", got, tc.want)
			}
		})
	}
}
