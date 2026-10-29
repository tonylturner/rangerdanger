package main

import (
	"bytes"
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonylturner/dnp3go"
)

func TestAnalogRequestVector(t *testing.T) {
	want := []byte{0xc1, 4, 41, 3, 0x17, 1, 0, 0, 0, 0x80, 0xc1, 0}
	if got := buildAnalogAPDU(4, 1, 0, -16); !bytes.Equal(got, want) {
		t.Fatalf("got %x want %x", got, want)
	}
}

func TestAnalogStatusVector(t *testing.T) {
	for _, status := range []byte{0, 4, 7} {
		client, server := net.Pipe()
		client.SetDeadline(time.Now().Add(time.Second))
		done := make(chan error, 1)
		go func() {
			defer server.Close()
			// Both replies retain Device Restart: not a command failure.
			done <- dnp3go.WriteLinkFrame(server, &dnp3go.LinkFrame{
				Control: 0x44, Dest: 100, Source: 3,
				Payload: []byte{0xc0, 0xc0, 0x81, 0x80, 0, 41, 3, 0x17, 1, 0, 0, 0, 0x80, 0xc1, status},
			})
		}()
		got, err := readControlStatus(client)
		client.Close()
		if writeErr := <-done; writeErr != nil {
			t.Fatal(writeErr)
		}
		if err != nil || got != controlStatus(status) {
			t.Fatalf("status=%d got=%d error=%v", status, got, err)
		}
	}
}

func TestAnalogCLISubprocess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "dnp3cmd")
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, mode := range []string{"direct", "-sbo"} {
		t.Run(mode, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			values := make(chan float32, 2)
			cfg := &dnp3go.OutstationConfig{
				OutstationAddr: 3, AcceptAnyMaster: true, Logger: func(string, ...any) {},
				AnalogOutputs: []dnp3go.AnalogOutputPoint{{Index: 0, Operate: func(v float32) uint8 {
					values <- v
					return 0
				}}},
			}
			done := make(chan error, 1)
			go func() {
				c, err := ln.Accept()
				if err != nil {
					done <- err
					return
				}
				c.SetDeadline(time.Now().Add(5 * time.Second))
				dnp3go.ServeConn(c, cfg)
				done <- nil
			}()
			args := []string{ln.Addr().String(), "-a", "3"}
			if mode == "-sbo" {
				args = append(args, mode)
			}
			args = append(args, "analog", "0", "-16")
			output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
			if err != nil || !strings.Contains(string(output), "Result: SUCCESS") {
				t.Fatalf("CLI error=%v\n%s", err, output)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			select {
			case value := <-values:
				if value != -16 {
					t.Fatalf("callback value=%g", value)
				}
			default:
				t.Fatal("CLI did not reach OPERATE")
			}
			if len(values) != 0 {
				t.Fatal("CLI operated more than once")
			}
		})
	}
}
