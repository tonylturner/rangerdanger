// SPDX-License-Identifier: Apache-2.0

// iec104-cc runs the lab's persistent controlling station and raw HTTP API.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tturner/rangerdanger/services/iec104/internal/controlcentre"
	"github.com/tturner/rangerdanger/services/iec104/internal/logfmt"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

const usage = "Usage: iec104-cc --config PATH --bind ADDR --target HOST[:PORT] --http ADDR:PORT\n" +
	"                 [--historian-url URL --historian-bind ADDR]\n\n" +
	"IEC 104 controlling station. Holds one session to the RTU at --target from the\n" +
	"local address --bind, caches the monitored points of the point map at --config,\n" +
	"and serves /api/health, /api/state, /api/commands and /api/journal on --http.\n"

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	values := map[string]string{}
	for i := 0; i < len(args); i += 2 {
		if i+1 == len(args) {
			fmt.Fprint(stderr, usage)
			return 2
		}
		switch args[i] {
		case "--config", "--bind", "--target", "--http", "--historian-url", "--historian-bind":
			values[args[i]] = args[i+1]
		default:
			fmt.Fprint(stderr, usage)
			return 2
		}
	}
	for _, key := range []string{"--config", "--bind", "--target", "--http"} {
		if _, ok := values[key]; !ok {
			fmt.Fprint(stderr, usage)
			return 2
		}
	}
	host, port, err := logfmt.ParseEndpoint(values["--target"], 128)
	httpHost, httpPort, httpErr := logfmt.ParseEndpoint(values["--http"], 64)
	if err != nil || httpErr != nil || !strings.Contains(values["--http"], ":") {
		fmt.Fprint(stderr, usage)
		return 2
	}
	logger := logfmt.NewLogger(stdout, "cc", nil)
	fatal := func(err error) int { logger.Printf("fatal: %s", err); return 1 }
	pm, err := pointmap.Load(values["--config"])
	if err != nil {
		text := err.Error()
		if len(text) > 255 {
			text = text[:255]
		}
		logger.Printf("config error: %s", text)
		return 1
	}
	peer := truncate(host+":"+strconv.Itoa(port), 63)
	cc, err := controlcentre.New(pm, net.JoinHostPort(host, strconv.Itoa(port)), controlcentre.Options{Logger: logger})
	if err != nil {
		return fatal(err)
	}
	if err := controlcentre.CheckBind(values["--bind"]); err != nil {
		return fatal(err)
	}
	ip := net.ParseIP(httpHost)
	if ip == nil || ip.To4() == nil || strings.Contains(httpHost, ":") {
		return fatal(fmt.Errorf("--http %s is not an IPv4 address", httpHost))
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(httpHost, strconv.Itoa(httpPort)))
	if err != nil {
		return fatal(fmt.Errorf("cannot listen on %s:%d: %w", httpHost, httpPort, err))
	}
	defer listener.Close()
	_, hasURL := values["--historian-url"]
	_, hasBind := values["--historian-bind"]
	if (hasURL || hasBind) && (values["--historian-url"] == "" || values["--historian-bind"] == "") {
		return fatal(fmt.Errorf("invalid or unbindable --historian-url / --historian-bind"))
	}
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	publisher, err := controlcentre.NewPublisher(startup, values["--historian-url"], values["--historian-bind"])
	cancel()
	if err != nil {
		return fatal(err)
	}
	if err := logger.Printf("control centre target=%s bind=%s http=%s:%d ca=%d points=%d commands=%d cache_epoch=%s", peer, values["--bind"], httpHost, httpPort, pm.CA, len(pm.Points), len(pm.Commands), cc.Epoch()); err != nil {
		return 1
	}
	if err := cc.Serve(ctx, listener, values["--bind"], publisher); err != nil {
		return fatal(err)
	}
	return 0
}
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
func main() {
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
