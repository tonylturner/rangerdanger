// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func wire(t *testing.T, c *Centre, payload string) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := l.Accept()
		if err == nil {
			c.serveConn(context.Background(), conn)
		}
	}()
	conn, err := net.DialTimeout("tcp4", l.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	if _, err := io.WriteString(conn, payload); err != nil {
		t.Fatal(err)
	}
	conn.(*net.TCPConn).CloseWrite()
	b, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	return string(b)
}
func TestHTTPBoundaryGoldens(t *testing.T) {
	c, _ := model(t)
	cmd := `{"point":"feeder-breaker-command","operation":"open"}`
	post := func(s string) string {
		return fmt.Sprintf("POST /api/commands HTTP/1.1\r\nContent-Length: %d\r\n\r\n%s", len(s), s)
	}
	for _, tc := range []struct {
		name, payload string
		status        int
		text, allow   string
	}{
		{"bad JSON", post("{not json"), 400, "request body must be a JSON object", ""},
		{"array", post("[]"), 400, "request body must be a JSON object", ""},
		{"empty", post(""), 400, "request body must be a JSON object", ""},
		{"trailing", post(cmd + " x"), 400, "request body must be a JSON object", ""},
		{"unknown field", post(`{"point":"feeder-breaker-command","operation":"open","force":true}`), 400, `unknown field "force"`, ""},
		{"duplicate", post(`{"point":"feeder-breaker-command","operation":"open","operation":"close"}`), 400, `duplicate field "operation"`, ""},
		{"missing", post(`{"point":"feeder-breaker-command"}`), 400, `"point" and "operation" are required`, ""},
		{"unknown point", post(`{"point":"feeder-current","operation":"open"}`), 400, `unknown command point "feeder-current"`, ""},
		{"operation", post(`{"point":"feeder-breaker-command","operation":"trip"}`), 400, "operation does not fit command kind", ""},
		{"number", post(`{"point":"feeder-breaker-command","operation":1}`), 400, `"operation" must be a string`, ""},
		{"null", post(`{"point":"feeder-breaker-command","operation":null}`), 400, `"operation" must be a string`, ""},
		{"NUL body", post(cmd + "\x00"), 400, "request body contains a NUL byte", ""},
		{"command query", "POST /api/commands?x=1 HTTP/1.1\r\nContent-Length: 0\r\n\r\n", 400, "this endpoint takes no query parameters", ""},
		{"after abc", "GET /api/journal?after=abc HTTP/1.1\r\n\r\n", 400, "the only query parameter is after=<non-negative integer>", ""},
		{"after other", "GET /api/journal?since=1 HTTP/1.1\r\n\r\n", 400, "the only query parameter is after=<non-negative integer>", ""},
		{"state query", "GET /api/state?x=1 HTTP/1.1\r\n\r\n", 400, "this endpoint takes no query parameters", ""},
		{"GET commands", "GET /api/commands HTTP/1.1\r\n\r\n", 405, "method not allowed", "POST"},
		{"POST state", "POST /api/state HTTP/1.1\r\nContent-Length: 2\r\n\r\n{}", 405, "method not allowed", "GET"},
		{"unknown", "GET /api/nope HTTP/1.1\r\n\r\n", 404, "not found", ""},
		{"big body", post(strings.Repeat("x", 5000)), 413, "request body too large", ""},
		{"missing length", "POST /api/commands HTTP/1.1\r\nHost: cc\r\n\r\n", 411, "POST needs a Content-Length body", ""},
		{"chunked", "POST /api/commands HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n", 501, "Transfer-Encoding is not supported; send Content-Length", ""},
		{"version", "GET /api/state HTTP/2.0\r\n\r\n", 505, "only HTTP/1.0 and HTTP/1.1 are supported", ""},
		{"garbage", "hello\r\n\r\n", 400, "malformed request line", ""},
		{"big header", "GET /api/state HTTP/1.1\r\nX: " + strings.Repeat("a", 9000) + "\r\n\r\n", 431, "request header too large", ""},
		{"method lowercase", "get /api/state HTTP/1.1\r\n\r\n", 400, "malformed method", ""},
		{"method long", "ABCDEFGH /api/state HTTP/1.1\r\n\r\n", 400, "malformed method", ""},
		{"spaces", "GET  /api/state HTTP/1.1\r\n\r\n", 400, "malformed request line", ""},
		{"version malformed", "GET /api/state xyz\r\n\r\n", 400, "malformed request line", ""},
		{"absolute URI", "GET http://cc/api/state HTTP/1.1\r\n\r\n", 400, "request target must be an absolute path", ""},
		{"URI long", "GET /" + strings.Repeat("a", 511) + " HTTP/1.1\r\n\r\n", 414, "request target too long", ""},
		{"fold", "GET /api/state HTTP/1.1\r\n X: a\r\n\r\n", 400, "obsolete header line folding", ""},
		{"colon", "GET /api/state HTTP/1.1\r\nX\r\n\r\n", 400, "malformed header line", ""},
		{"empty header", "GET /api/state HTTP/1.1\r\n: x\r\n\r\n", 400, "malformed header line", ""},
		{"length sign", "POST /api/commands HTTP/1.1\r\nContent-Length: +1\r\n\r\nx", 400, "invalid Content-Length", ""},
		{"length long", "POST /api/commands HTTP/1.1\r\nContent-Length: 0000000000\r\n\r\n", 413, "invalid Content-Length", ""},
		{"length conflict", "POST /api/commands HTTP/1.1\r\nContent-Length: 1\r\nContent-Length: 2\r\n\r\n{}", 400, "conflicting Content-Length headers", ""},
		{"GI body", "POST /api/gi HTTP/1.1\r\nContent-Length: 2\r\n\r\n{}", 400, "this endpoint takes no body", ""},
		{"reconnect body", "POST /api/reconnect HTTP/1.1\r\nContent-Length: 2\r\n\r\n{}", 400, "this endpoint takes no body", ""},
		{"link down", post(cmd), 503, "link not started", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"error":` + strconv.Quote(tc.text) + `}`
			reason := http.StatusText(tc.status)
			if tc.status == 413 {
				reason = "Content Too Large"
			}
			allow := ""
			if tc.allow != "" {
				allow = "Allow: " + tc.allow + "\r\n"
			}
			want := fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nCache-Control: no-store\r\nConnection: close\r\n%s\r\n%s", tc.status, reason, len(body), allow, body)
			equal(t, []byte(wire(t, c, tc.payload)), want)
		})
	}
	// No Host requirement, repeated identical lengths, HTTP/1.0, one request only.
	text := wire(t, c, "GET /api/health HTTP/1.0\r\nContent-Length: 0\r\ncontent-length:\t0 \r\n\r\nGET /api/state HTTP/1.1\r\n\r\n")
	if !strings.HasPrefix(text, "HTTP/1.1 200 OK\r\n") || strings.Count(text, "HTTP/1.1") != 1 || !strings.HasSuffix(text, `{"status":"ok","link":"disconnected","historian":{"last_ok_at":null,"failures":0,"dropped_snapshots":0}}`) {
		t.Fatal(text)
	}
}
func TestHTTPTimeouts(t *testing.T) {
	for _, tc := range []struct{ payload, text string }{{"GET /api/state HTTP/1.1\r\n", "request not received in time"}, {"POST /api/commands HTTP/1.1\r\nContent-Length: 1\r\n\r\n", "request body not received in time"}} {
		c, _ := model(t)
		a, b := net.Pipe()
		done := make(chan struct{})
		go func() { defer close(done); c.serveConn(context.Background(), a) }()
		b.SetDeadline(time.Now().Add(7 * time.Second))
		if _, err := io.WriteString(b, tc.payload); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(b)
		if err != nil {
			t.Fatal(err)
		}
		b.Close()
		<-done
		if !strings.Contains(string(data), "408 Request Timeout") || !strings.HasSuffix(string(data), `{"error":"`+tc.text+`"}`) {
			t.Fatal(string(data))
		}
	}
}
func TestHTTPConnectionCapAndShutdown(t *testing.T) {
	c, _ := model(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.serveHTTP(ctx, l) }()
	var conns []net.Conn
	defer func() {
		for _, conn := range conns {
			conn.Close()
		}
	}()
	for i := 0; i < 33; i++ {
		conn, err := net.DialTimeout("tcp4", l.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		if i == 32 {
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			data, err := io.ReadAll(conn)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(string(data), `{"error":"too many concurrent connections"}`) {
				t.Fatal(string(data))
			}
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown leaked HTTP connections")
	}
}
