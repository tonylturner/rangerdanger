// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxHeader      = 8192
	maxBody        = 4096
	maxConnections = 32
	ioTimeout      = 5 * time.Second
)

func writeResponse(conn net.Conn, r response) {
	reason := http.StatusText(r.status)
	if r.status == 413 {
		reason = "Content Too Large"
	}
	allow := ""
	if r.allow != "" {
		allow = "Allow: " + r.allow + "\r\n"
	}
	head := fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nCache-Control: no-store\r\nConnection: close\r\n%s\r\n", r.status, reason, len(r.body), allow)
	conn.SetWriteDeadline(time.Now().Add(ioTimeout))
	if _, err := io.WriteString(conn, head); err != nil {
		return
	}
	_, _ = conn.Write(r.body)
}
func parseHead(head string) (request, int, response) {
	var r request
	lines := strings.Split(head, "\r\n")
	parts := strings.Split(lines[0], " ")
	if len(parts) != 3 {
		return r, 0, failure(400, "malformed request line")
	}
	r.method = parts[0]
	target, version := parts[1], parts[2]
	if len(r.method) == 0 || len(r.method) > 7 {
		return r, 0, failure(400, "malformed method")
	}
	for _, b := range []byte(r.method) {
		if b < 'A' || b > 'Z' {
			return r, 0, failure(400, "malformed method")
		}
	}
	if !strings.HasPrefix(version, "HTTP/") {
		return r, 0, failure(400, "malformed request line")
	}
	if version != "HTTP/1.0" && version != "HTTP/1.1" {
		return r, 0, failure(505, "only HTTP/1.0 and HTTP/1.1 are supported")
	}
	if !strings.HasPrefix(target, "/") {
		return r, 0, failure(400, "request target must be an absolute path")
	}
	if len(target) >= 512 {
		return r, 0, failure(414, "request target too long")
	}
	cl := -1
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			return r, 0, failure(400, "obsolete header line folding")
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || name == "" {
			return r, 0, failure(400, "malformed header line")
		}
		value = strings.Trim(value, " \t")
		if strings.EqualFold(name, "Transfer-Encoding") {
			return r, 0, failure(501, "Transfer-Encoding is not supported; send Content-Length")
		}
		if strings.EqualFold(name, "Content-Length") {
			if len(value) > 9 {
				return r, 0, failure(413, "invalid Content-Length")
			}
			if value == "" || !digits(value) {
				return r, 0, failure(400, "invalid Content-Length")
			}
			n, _ := strconv.Atoi(value)
			if cl >= 0 && n != cl {
				return r, 0, failure(400, "conflicting Content-Length headers")
			}
			cl = n
		}
	}
	if cl < 0 {
		if r.method == "POST" {
			return r, 0, failure(411, "POST needs a Content-Length body")
		}
		cl = 0
	}
	if cl > maxBody {
		return r, 0, failure(413, "request body too large")
	}
	r.path, r.query, _ = strings.Cut(target, "?")
	return r, cl, response{}
}
func (c *Centre) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	requestDeadline := time.Now().Add(ioTimeout)
	conn.SetReadDeadline(requestDeadline)
	defer func() {
		if tcp, ok := conn.(*net.TCPConn); ok {
			tcp.CloseWrite()
			sink := make([]byte, 4096)
			total := 0
			drainDeadline := time.Now().Add(time.Second)
			if requestDeadline.Before(drainDeadline) {
				drainDeadline = requestDeadline
			}
			conn.SetReadDeadline(drainDeadline)
			for total < 64*1024 {
				n, err := conn.Read(sink)
				total += n
				if err != nil {
					break
				}
			}
		}
	}()
	buf := make([]byte, maxHeader)
	used, end := 0, -1
	for end < 0 {
		if used == maxHeader {
			writeResponse(conn, failure(431, "request header too large"))
			return
		}
		n, err := conn.Read(buf[used:])
		used += n
		text := buf[:used]
		if nul := bytes.IndexByte(text, 0); nul >= 0 {
			text = text[:nul]
		}
		if i := bytes.Index(text, []byte("\r\n\r\n")); i >= 0 {
			end = i
			break
		}
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && used > 0 {
				writeResponse(conn, failure(408, "request not received in time"))
			}
			return
		}
	}
	r, want, resp := parseHead(string(buf[:end]))
	if resp.status != 0 {
		writeResponse(conn, resp)
		return
	}
	r.body = make([]byte, want)
	got := copy(r.body, buf[end+4:used])
	for got < want {
		n, err := conn.Read(r.body[got:])
		got += n
		if got == want {
			break
		}
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				writeResponse(conn, failure(408, "request body not received in time"))
			}
			return
		}
	}
	writeResponse(conn, c.api(ctx, r))
}
func (c *Centre) serveHTTP(ctx context.Context, listener net.Listener) error {
	var workers sync.WaitGroup
	slots := make(chan struct{}, maxConnections)
	var mu sync.Mutex
	active := map[net.Conn]struct{}{}
	stop := make(chan struct{})
	watcherDone := make(chan struct{})
	defer func() {
		close(stop)
		listener.Close()
		mu.Lock()
		for conn := range active {
			conn.Close()
		}
		mu.Unlock()
		workers.Wait()
		<-watcherDone
	}()
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			listener.Close()
		case <-stop:
		}
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil && errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			writeResponse(conn, failure(503, "too many concurrent connections"))
			conn.Close()
			continue
		}
		mu.Lock()
		if ctx.Err() != nil {
			mu.Unlock()
			conn.Close()
			<-slots
			return nil
		}
		active[conn] = struct{}{}
		mu.Unlock()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { mu.Lock(); delete(active, conn); mu.Unlock(); <-slots }()
			c.serveConn(ctx, conn)
		}()
	}
}
