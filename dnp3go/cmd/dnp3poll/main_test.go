package main

import (
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonylturner/dnp3go"
)

func TestPollCLIReportsPersistentRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "dnp3poll")
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cfg := &dnp3go.OutstationConfig{
		OutstationAddr: 1, AcceptAnyMaster: true, Logger: func(string, ...any) {},
		BinaryInputs: []dnp3go.BinaryPoint{{Index: 0, Read: func() bool { return true }}},
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			c, err := ln.Accept()
			if err != nil {
				done <- err
				return
			}
			c.SetDeadline(time.Now().Add(5 * time.Second))
			dnp3go.ServeConn(c, cfg)
		}
		done <- nil
	}()
	for i := 0; i < 2; i++ {
		output, err := exec.CommandContext(ctx, binary, "-a", "1", ln.Addr().String()).CombinedOutput()
		if err != nil || !strings.Contains(string(output), "IIN: 0x8000") ||
			!strings.Contains(string(output), "[ 0] = ON") {
			t.Fatalf("poll %d: error=%v\n%s", i, err, output)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
