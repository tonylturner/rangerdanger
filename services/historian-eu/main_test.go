package main

import (
	"context"
	"net"
	"reflect"
	"testing"
)

func TestConfig(t *testing.T) {
	defaults, err := loadConfig(func(string) (string, bool) { return "", false })
	if err != nil || defaults.ingest != "10.70.70.40:8081" ||
		!reflect.DeepEqual(defaults.reads, []string{"10.70.70.40:8080", "10.99.99.40:8080"}) {
		t.Fatalf("defaults: %+v %v", defaults, err)
	}
	for _, tc := range []struct {
		name   string
		ingest string
		read   string
		valid  bool
	}{
		{"loopback", "127.0.0.1:8081", "127.0.0.1:8080,127.0.0.2:8080", true},
		{"ipv6", "[::1]:8081", "[::1]:8080", true},
		{"overlap", "127.0.0.1:8081", "127.0.0.1:8081", false},
		{"duplicate-read", "127.0.0.1:8081", "127.0.0.1:8080,127.0.0.1:8080", false},
		{"normalized-overlap", "[::1]:8081", "[0:0:0:0:0:0:0:1]:8081", false},
		{"empty-ingest", "", "127.0.0.1:8080", false},
		{"empty-read", "127.0.0.1:8081", "", false},
		{"trailing-read-comma", "127.0.0.1:8081", "127.0.0.1:8080,", false},
		{"wildcard", ":8081", "127.0.0.1:8080", false},
		{"unspecified", "0.0.0.0:8081", "127.0.0.1:8080", false},
		{"unspecified-v6", "[::]:8081", "127.0.0.1:8080", false},
		{"hostname", "localhost:8081", "127.0.0.1:8080", false},
		{"zero-port", "127.0.0.1:0", "127.0.0.1:8080", false},
		{"oversize-port", "127.0.0.1:65536", "127.0.0.1:8080", false},
		{"service-port", "127.0.0.1:http", "127.0.0.1:8080", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadConfig(func(key string) (string, bool) {
				if key == "HISTORIAN_INGEST_LISTEN" {
					return tc.ingest, true
				}
				return tc.read, true
			})
			if (err == nil) != tc.valid {
				t.Fatalf("config: %+v %v", c, err)
			}
		})
	}
}

func TestBindFailureRejectsPartialStartup(t *testing.T) {
	// The ingest bind is ephemeral here; the occupied read address guarantees
	// all-or-nothing startup without serving any requests.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	c := config{ingest: "127.0.0.1:0", reads: []string{occupied.Addr().String()}}
	if err := serve(context.Background(), c, newStore(testClock)); err == nil {
		t.Fatal("partial listener startup succeeded")
	}
}

func TestServeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := config{ingest: "127.0.0.1:0", reads: []string{"127.0.0.1:0", "127.0.0.1:0"}}
	if err := serve(ctx, c, newStore(testClock)); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
