package containd

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Client communicates with the containd NGFW API. One Client is shared
// by the whole backend for the life of the process, so it holds the JWT
// secret rather than a token and signs a fresh token for every request:
// containd rejects expired tokens, and a token minted once at startup
// would expire 24h into a long-running workshop.
type Client struct {
	BaseURL    string
	jwtSecret  string
	now        func() time.Time
	httpClient *http.Client
}

// JWTSecret is the secret the backend signs containd tokens with:
// CONTAIND_JWT_SECRET, else the Compose default. The backend starts the range
// with this value so the range firewall verifies what the backend signs.
func JWTSecret() string {
	if secret := os.Getenv("CONTAIND_JWT_SECRET"); secret != "" {
		return secret
	}
	return "rangerdanger-dev"
}

// NewClient creates a containd API client with JWT authentication.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:   baseURL,
		jwtSecret: JWTSecret(),
		now:       time.Now,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// generateJWT creates a minimal JWT token for containd API auth, valid
// for 24h from now.
func generateJWT(secret string, now time.Time) string {
	// JWT header: {"alg":"HS256","typ":"JWT"}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	// JWT payload with admin role and long expiry
	exp := now.Add(24 * time.Hour).Unix()
	payload := fmt.Sprintf(`{"sub":"rangerdanger-backend","role":"admin","exp":%d}`, exp)
	payloadEnc := base64.RawURLEncoding.EncodeToString([]byte(payload))

	// Sign with HMAC-SHA256
	message := header + "." + payloadEnc
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	signature := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	return message + "." + signature
}

// send signs req with a freshly minted token and performs it.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+generateJWT(c.jwtSecret, c.now()))
	return c.httpClient.Do(req)
}

// doRequest performs an authenticated HTTP request.
func (c *Client) doRequest(ctx context.Context, method, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	return c.send(req)
}

// doRequestWithBody performs an authenticated HTTP request with a body.
func (c *Client) doRequestWithBody(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.send(req)
}
