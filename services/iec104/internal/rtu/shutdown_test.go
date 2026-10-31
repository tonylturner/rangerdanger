// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	iec "github.com/tonylturner/iec104go"
)

// Delay the real stack's Serve until Shutdown has already closed admission.
// This forces the startup/stop race independently of goroutine scheduling.
type startAfterShutdown struct {
	stationServer
	resume chan struct{}
}

func (s *startAfterShutdown) Serve(ctx context.Context, l net.Listener) error {
	<-s.resume
	return s.stationServer.Serve(ctx, l)
}

func (s *startAfterShutdown) Shutdown(ctx context.Context) error {
	err := s.stationServer.Shutdown(ctx)
	close(s.resume)
	return err
}

type closeTrackedListener struct {
	net.Listener
	closed atomic.Bool
}

func (l *closeTrackedListener) Close() error {
	l.closed.Store(true)
	return l.Listener.Close()
}

func shutdownModel(t *testing.T) (*RTU, *closeTrackedListener) {
	t.Helper()
	r, _, _ := newModel(t, 0)
	// No field/network work is needed to exercise the server lifecycle.
	r.pm.Devices, r.devices, r.started = nil, nil, nil
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tracked := &closeTrackedListener{Listener: l}
	t.Cleanup(func() { tracked.Close() })
	return r, tracked
}

func TestShutdownBeforeServerStart(t *testing.T) {
	r, l := shutdownModel(t)
	r.server = &startAfterShutdown{stationServer: r.server, resume: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Serve(ctx, l); err != nil {
		t.Errorf("requested stop became station failure: %v", err)
	}
	if !l.closed.Load() {
		t.Error("listener leaked when the stack never took ownership")
	}
}

type serverFuncs struct {
	serve    func(context.Context, net.Listener) error
	shutdown func(context.Context) error
}

func (s serverFuncs) Serve(ctx context.Context, l net.Listener) error {
	return s.serve(ctx, l)
}
func (s serverFuncs) Shutdown(ctx context.Context) error { return s.shutdown(ctx) }

func TestServeStopErrorOrdering(t *testing.T) {
	failure := errors.New("listener failed")
	shutdownFailure := errors.New("shutdown failed")
	for _, tc := range []struct {
		name        string
		stopFirst   bool
		serveErr    error
		shutdownErr error
		want        error
	}{
		{"stop then local close", true, fmt.Errorf("serve: %w", iec.ErrClosed), nil, nil},
		{"stop then cancellation", true, fmt.Errorf("serve: %w", context.Canceled), nil, nil},
		{"stop then deadline", true, fmt.Errorf("serve: %w", context.DeadlineExceeded), nil, nil},
		{"local close before stop", false, iec.ErrClosed, nil, iec.ErrClosed},
		{"cancellation before stop", false, context.Canceled, nil, context.Canceled},
		{"failure before stop", false, failure, nil, failure},
		{"failure after stop", true, failure, nil, failure},
		{"shutdown failure after stop", true, iec.ErrClosed, shutdownFailure, shutdownFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, l := shutdownModel(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.stopFirst {
				cancel()
			}
			r.server = serverFuncs{
				serve: func(runCtx context.Context, _ net.Listener) error {
					if tc.stopFirst {
						<-runCtx.Done()
					}
					return tc.serveErr
				},
				shutdown: func(context.Context) error {
					// A later stop must not erase the already-observed failure.
					// RTU's own worker cancellation must not erase it either.
					cancel()
					return tc.shutdownErr
				},
			}
			if err := r.Serve(ctx, l); !errors.Is(err, tc.want) {
				t.Fatalf("Serve=%v, want %v", err, tc.want)
			}
		})
	}
}
