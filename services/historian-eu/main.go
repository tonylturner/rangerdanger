// historian-eu archives CC observations; it has no device or CC client.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type config struct {
	ingest string
	reads  []string
}

func loadConfig(getenv func(string) (string, bool)) (config, error) {
	value := func(name, fallback string) string {
		if v, ok := getenv(name); ok {
			return v
		}
		return fallback
	}
	c := config{
		ingest: value("HISTORIAN_INGEST_LISTEN", "10.70.70.40:8081"),
		reads:  strings.Split(value("HISTORIAN_READ_LISTEN", "10.70.70.40:8080,10.99.99.40:8080"), ","),
	}
	seen := make(map[string]bool)
	for _, address := range append([]string{c.ingest}, c.reads...) {
		host, port, err := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		n, numberErr := strconv.Atoi(port)
		if err != nil || ip == nil || ip.IsUnspecified() || numberErr != nil || n < 1 || n > 65535 {
			return config{}, fmt.Errorf("listener %q must be an explicit IP and nonzero port", address)
		}
		key := net.JoinHostPort(ip.String(), strconv.Itoa(n))
		if seen[key] {
			return config{}, fmt.Errorf("duplicate listener %q", address)
		}
		seen[key] = true
	}
	return c, nil
}

func serve(ctx context.Context, c config, s *store) error {
	var listeners []net.Listener
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	// Bind every interface before serving; partial startup must fail closed.
	for _, address := range append([]string{c.ingest}, c.reads...) {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		listeners = append(listeners, listener)
	}
	servers := make([]*http.Server, 0, len(listeners))
	errs := make(chan error, len(listeners))
	for i, listener := range listeners {
		handler := s.readHandler()
		if i == 0 {
			handler = s.ingestHandler()
		}
		server := &http.Server{
			Handler: handler, ReadHeaderTimeout: 2 * time.Second,
			ReadTimeout: 2 * time.Second, WriteTimeout: 10 * time.Second,
			IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192,
		}
		servers = append(servers, server)
		go func() { errs <- server.Serve(listener) }()
	}
	var err error
	select {
	case <-ctx.Done():
	case err = <-errs:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, server := range servers {
		if shutdownErr := server.Shutdown(shutdown); shutdownErr != nil {
			_ = server.Close()
			if err == nil {
				err = shutdownErr
			}
		}
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func main() {
	c, err := loadConfig(os.LookupEnv)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, c, newStore(time.Now)); err != nil {
		log.Fatal(err)
	}
}
