// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestFileSegmentSizeOption(t *testing.T) {
	for cot := 1; cot <= 2; cot++ {
		for ca := 1; ca <= 2; ca++ {
			for ioa := 1; ioa <= 3; ioa++ {
				p := Params{cot, ca, ioa}
				max := MaxASDUSize - p.headerSize() - ioa - 4
				for _, size := range []int{-1, 0, 1, 7, max, max + 1} {
					t.Run(fmt.Sprintf("%v/%d", p, size), func(t *testing.T) {
						o := DefaultOptions()
						o.Params = p
						o.Files.SegmentSize = size
						n, err := o.normalized()
						if size < 0 || size > max {
							if err == nil {
								t.Fatal("invalid segment limit accepted", size, max)
							}
							return
						}
						if err != nil {
							t.Fatal(err)
						}
						want := size
						if want == 0 {
							want = max
						}
						if got := n.Files.segmentSize(p); got != want {
							t.Fatal("wrong derived segment limit", got, want)
						}
					})
				}
			}
		}
	}
}

func TestFileProcedureReducedSegments(t *testing.T) {
	opts := fileTestOptions()
	opts.Files.SegmentSize = 7
	var mu sync.Mutex
	var lengths []int
	opts.Tap = func(dir Direction, _ uint64, _ time.Time, b []byte) {
		if dir == Transmit && len(b) >= 19 && b[2]&1 == 0 && TypeID(b[6]) == F_SG_NA_1 {
			mu.Lock()
			lengths = append(lengths, int(b[18]))
			mu.Unlock()
		}
	}
	p := memoryFiles{file: File{[]FileSection{{2, bytes.Repeat([]byte{1}, 20)}, {3, []byte{1, 2, 3, 4, 5, 6}}}}}
	c, _, _ := filePair(t, p, opts, nil)
	file, err := c.DownloadFile(testContext(t), testFileKey)
	if err != nil || !reflect.DeepEqual(file, p.file) {
		t.Fatal("reduced segments changed content", file, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(lengths, []int{7, 7, 6, 6}) {
		t.Fatal("configured segment size not used", lengths)
	}
}
