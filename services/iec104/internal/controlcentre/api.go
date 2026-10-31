// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

type request struct {
	method, path, query string
	body                []byte
}
type response struct {
	status int
	body   json.RawMessage
	allow  string
}

func failure(status int, text string) response {
	return response{status: status, body: ordered("error", text)}
}
func (c *Centre) api(ctx context.Context, r request) response {
	method := ""
	switch r.path {
	case "/api/health", "/api/state", "/api/journal":
		method = "GET"
	case "/api/commands", "/api/gi", "/api/reconnect":
		method = "POST"
	default:
		return failure(404, "not found")
	}
	if r.method != method {
		out := failure(405, "method not allowed")
		out.allow = method
		return out
	}
	if r.path != "/api/journal" && r.query != "" {
		return failure(400, "this endpoint takes no query parameters")
	}
	if r.path == "/api/commands" {
		index, value, op, err := c.parseCommand(r.body)
		if err != "" {
			return failure(400, err)
		}
		return c.command(ctx, index, value, op)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch r.path {
	case "/api/health":
		return response{status: 200, body: c.healthJSON()}
	case "/api/state":
		return response{status: 200, body: c.stateJSON()}
	case "/api/journal":
		var after uint64
		if r.query != "" {
			if !strings.HasPrefix(r.query, "after=") {
				return failure(400, "the only query parameter is after=<non-negative integer>")
			}
			s := r.query[6:]
			if len(s) == 0 || len(s) > 19 || !digits(s) {
				return failure(400, "the only query parameter is after=<non-negative integer>")
			}
			after, _ = strconv.ParseUint(s, 10, 64)
		}
		return response{status: 200, body: c.journalJSON(after)}
	case "/api/gi":
		if len(r.body) > 0 {
			return failure(400, "this endpoint takes no body")
		}
		if c.link.state != "started" {
			return failure(503, "link not started")
		}
		if c.link.gi == "running" || c.link.pending != "" || c.link.reconnect {
			return failure(409, "interrogation already running")
		}
		c.link.pending = "operator"
	case "/api/reconnect":
		if len(r.body) > 0 {
			return failure(400, "this endpoint takes no body")
		}
		if c.link.state != "started" {
			return failure(503, "link not started; normal reconnect is already running")
		}
		if c.link.reconnect || c.tracker.active {
			return failure(409, "reconnect pending or command in flight")
		}
		c.link.reconnect = true
	}
	c.notify()
	return response{status: 202, body: ordered("trigger", "operator", "requested_at", c.stamp())}
}
func digits(s string) bool {
	for _, b := range []byte(s) {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
func (c *Centre) parseCommand(body []byte) (int, int, string, string) {
	if strings.IndexByte(string(body), 0) >= 0 {
		return 0, 0, "", "request body contains a NUL byte"
	}
	var root json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil || len(root) == 0 || root[0] != '{' {
		return 0, 0, "", "request body must be a JSON object"
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	if _, err := dec.Token(); err != nil {
		return 0, 0, "", "request body must be a JSON object"
	}
	fields := map[string]string{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, "", "request body must be a JSON object"
		}
		key := strings.SplitN(tok.(string), "\x00", 2)[0]
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return 0, 0, "", "request body must be a JSON object"
		}
		if key != "point" && key != "operation" {
			return 0, 0, "", "unknown field \"" + truncate(key, 64) + "\""
		}
		if _, ok := fields[key]; ok {
			return 0, 0, "", "duplicate field \"" + key + "\""
		}
		var v string
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &v) != nil {
			return 0, 0, "", "\"" + key + "\" must be a string"
		}
		fields[key] = strings.SplitN(v, "\x00", 2)[0]
	}
	if len(fields) != 2 {
		return 0, 0, "", "\"point\" and \"operation\" are required"
	}
	for i, cmd := range c.pm.Commands {
		if cmd.ID == fields["point"] {
			op := truncate(fields["operation"], 15)
			value := -1
			switch string(cmd.Kind) {
			case "double-command":
				if op == "open" {
					value = 1
				}
				if op == "close" {
					value = 2
				}
			case "regulating-step":
				if op == "lower" {
					value = 1
				}
				if op == "raise" {
					value = 2
				}
			case "single-command":
				if op == "manual" {
					value = 0
				}
				if op == "auto" {
					value = 1
				}
			}
			if value < 0 {
				return 0, 0, "", "operation does not fit command kind"
			}
			return i, value, op, ""
		}
	}
	return 0, 0, "", "unknown command point \"" + truncate(fields["point"], 64) + "\""
}
