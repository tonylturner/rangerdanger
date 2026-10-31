package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Helpers for the range services the backend calls over mgmt_net by
// manifest endpoint (control centre, sim device, process solver):
// bounded requests, the "not supported" answer for a package without the
// service, and the mapping of upstream failures to 502/504.

// notSupportedMessage answers a range route whose service, or recipe
// operation, the active package does not have.
const notSupportedMessage = "not supported by this package"

func notSupported(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": notSupportedMessage})
}

// upstreamReadTimeout bounds one read of a range service.
const upstreamReadTimeout = 5 * time.Second

// maxUpstreamBody bounds what the backend reads from a range service.
const maxUpstreamBody = 4 << 20

// maxRouteRequestBody bounds request bodies of the operator, sim and
// session routes.
const maxRouteRequestBody = 4 << 10

// upstreamClient is shared: requests carry their own context and deadline.
// The transport does not replay POSTs, so commands are never retried.
var upstreamClient = &http.Client{}

// upstreamError is a range service answer other than the expected status.
type upstreamError struct {
	service string
	status  int
	body    []byte
}

func (e *upstreamError) Error() string {
	return fmt.Sprintf("%s returned %d: %s", e.service, e.status, strings.TrimSpace(string(e.body)))
}

// callUpstream sends one request to a range service, bounded by timeout and
// ctx, and returns its status and body.
func callUpstream(ctx context.Context, method, url string, body []byte, timeout time.Duration) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, data, nil
}

// fetchUpstreamObject GETs url and returns its body, which must be a JSON
// object with status 200.
func fetchUpstreamObject(ctx context.Context, service, url string) (json.RawMessage, error) {
	status, body, err := callUpstream(ctx, http.MethodGet, url, nil, upstreamReadTimeout)
	if err != nil {
		return nil, fmt.Errorf("%s unreachable: %w", service, err)
	}
	if status != http.StatusOK {
		return nil, &upstreamError{service: service, status: status, body: body}
	}
	return jsonObject(service, body)
}

// jsonObject checks that body is one JSON object.
func jsonObject(service string, body []byte) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return nil, fmt.Errorf("%s returned a body that is not a JSON object", service)
	}
	return json.RawMessage(trimmed), nil
}

// upstreamFailureStatus is 504 for a timed-out upstream, else 502. A range
// service failure never answers as a healthy empty body.
func upstreamFailureStatus(err error) int {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

// upstreamFailed answers a request whose range service failed.
func upstreamFailed(c *gin.Context, err error) {
	c.JSON(upstreamFailureStatus(err), gin.H{"error": err.Error()})
}

// bindStrictJSON decodes the request body, one JSON value of at most
// maxRouteRequestBody bytes without unknown fields, into out. It answers
// 400 itself and reports false on failure.
func bindStrictJSON(c *gin.Context, out any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxRouteRequestBody))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body too large or unreadable"})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
		return false
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: trailing data after the JSON value"})
		return false
	}
	return true
}

// afterParam is the ?after= journal cursor: a non-negative integer, 0 when
// absent. It answers 400 itself and reports false on a bad value.
func afterParam(c *gin.Context) (uint64, bool) {
	after, err := strconv.ParseUint(c.DefaultQuery("after", "0"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "after must be a non-negative integer"})
		return 0, false
	}
	return after, true
}
