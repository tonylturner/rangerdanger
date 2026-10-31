// SPDX-License-Identifier: Apache-2.0

// iec104-rtu runs the lab's controlled station and Modbus gateway.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/tturner/rangerdanger/services/iec104/internal/logfmt"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
	"github.com/tturner/rangerdanger/services/iec104/internal/rtu"
)

const usage = "Usage: iec104-rtu --config PATH [--bind ADDR] [--port N]\n\n" +
	"IEC 104 controlled station (server). The point map at --config supplies\n" +
	"CA, timers, the Modbus device, the monitored points and the command set.\n"

type arguments struct {
	config, bind string
	port         int
}

func parse(args []string) (arguments, bool) {
	a := arguments{bind: "0.0.0.0", port: 2404}
	hasConfig := false
	for i := 0; i < len(args); i += 2 {
		if i+1 == len(args) {
			return a, false
		}
		switch args[i] {
		case "--config":
			a.config = args[i+1]
			hasConfig = true
		case "--bind":
			a.bind = args[i+1]
		case "--port":
			n, err := logfmt.ParseInt(args[i+1], 1, 65535)
			if err != nil {
				return a, false
			}
			a.port = int(n)
		default:
			return a, false
		}
	}
	return a, hasConfig
}
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	a, ok := parse(args)
	if !ok {
		fmt.Fprint(stderr, usage)
		return 2
	}
	logger := logfmt.NewLogger(stdout, "rtu", nil)
	pm, err := pointmap.Load(a.config)
	if err != nil {
		text := err.Error()
		if len(text) > 255 {
			text = text[:255]
		}
		if logger.Printf("config error: %s", text) != nil {
			return 1
		}
		return 1
	}
	station, err := rtu.New(pm, rtu.Options{Logger: logger})
	if err != nil {
		logger.Printf("cannot initialize station: %v", err)
		return 1
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(a.bind, strconv.Itoa(a.port)))
	if err != nil {
		logger.Printf("cannot listen on %s:%d", a.bind, a.port)
		return 1
	}
	if logger.Printf("listening %s:%d ca=%d points=%d devices=%d select-timeout=%dms", a.bind, a.port, pm.CA, len(pm.Points), len(pm.Devices), pm.SelectTimeoutMS) != nil {
		listener.Close()
		return 1
	}
	if err := station.Serve(ctx, listener); err != nil {
		logger.Printf("station failed: %v", err)
		return 1
	}
	return 0
}
func main() {
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
