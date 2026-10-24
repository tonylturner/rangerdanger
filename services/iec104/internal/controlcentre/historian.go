// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/tturner/rangerdanger/services/iec104/internal/logfmt"
)

// Publisher is a startup-resolved, source-bound HTTP destination.
type Publisher struct {
	host, path, target, bind string
	port                     int
}

// CheckBind refuses non-local IPv4 sources before a connection attempt.
func CheckBind(bind string) error {
	ip := net.ParseIP(bind)
	if ip == nil || ip.To4() == nil || strings.Contains(bind, ":") {
		return fmt.Errorf("--bind %s is not an IPv4 address", bind)
	}
	l, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: ip})
	if err != nil {
		return fmt.Errorf("--bind %s: %w", bind, err)
	}
	return l.Close()
}

// NewPublisher validates the optional URL/source pair and resolves once, never on a link callback.
func NewPublisher(ctx context.Context, url, bind string) (*Publisher, error) {
	if url == "" && bind == "" {
		return nil, nil
	}
	invalid := fmt.Errorf("invalid or unbindable --historian-url / --historian-bind")
	if !strings.HasPrefix(url, "http://") || strings.ContainsAny(url, "\r\n ") || bind == "" {
		return nil, invalid
	}
	endpoint, path, ok := strings.Cut(url[7:], "/")
	path = "/" + path
	if !ok || len(path) >= 256 || len(endpoint) == 0 || len(endpoint) >= 160 {
		return nil, invalid
	}
	host, port, err := logfmt.ParseEndpoint(endpoint, 128)
	if err != nil {
		return nil, invalid
	}
	if !strings.Contains(endpoint, ":") {
		port = 80
	}
	if CheckBind(bind) != nil {
		return nil, invalid
	}
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil || len(addresses) == 0 {
		return nil, invalid
	}
	return &Publisher{host: host, path: path, target: net.JoinHostPort(addresses[0].String(), strconv.Itoa(port)), port: port, bind: bind}, nil
}
func (c *Centre) enqueue() {
	if !c.historian {
		return
	}
	state := c.stateJSON()
	if len(c.queue) == 16 {
		copy(c.queue, c.queue[1:])
		c.queue = c.queue[:15]
		c.dropped++
	}
	c.queue = append(c.queue, state)
	c.notify()
}
func (p *Publisher) push(ctx context.Context, body []byte) bool {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(p.bind)}}
	conn, err := d.DialContext(ctx, "tcp4", p.target)
	if err != nil {
		return false
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	text := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s:%d\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", p.path, p.host, p.port, len(body), body)
	for len(text) > 0 {
		n, err := conn.Write([]byte(text))
		if err != nil || n <= 0 {
			return false
		}
		text = text[n:]
	}
	buf := make([]byte, 4095)
	used := 0
	for used < len(buf) {
		n, err := conn.Read(buf[used:])
		used += n
		if strings.Contains(string(buf[:used]), "\r\n\r\n") {
			var minor, code int
			_, err := fmt.Sscanf(string(buf[:used]), "HTTP/1.%d %d", &minor, &code)
			return err == nil && code >= 200 && code < 300
		}
		if err != nil {
			return false
		}
	}
	return false
}
func (c *Centre) publish(ctx context.Context, p *Publisher) {
	defer func() { c.mu.Lock(); c.historian = false; c.queue = nil; c.mu.Unlock() }()
	next := time.Time{}
	for ctx.Err() == nil {
		c.mu.Lock()
		now := c.opts.Now()
		if !now.Before(next) {
			c.enqueue()
			next = now.Add(time.Second)
		}
		if len(c.queue) == 0 {
			changed := c.changed
			left := next.Sub(now)
			c.mu.Unlock()
			timer := time.NewTimer(left)
			select {
			case <-ctx.Done():
			case <-changed:
			case <-timer.C:
			}
			timer.Stop()
			continue
		}
		body := c.queue[0]
		c.queue = c.queue[1:]
		c.mu.Unlock()
		ok := p.push(ctx, body)
		c.mu.Lock()
		if ok {
			c.lastOK = c.stamp()
		} else {
			c.failures++
		}
		c.mu.Unlock()
	}
}
