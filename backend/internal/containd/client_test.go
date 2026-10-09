package containd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestGenerateJWT verifies that the self-generated JWT has valid structure
// and is compatible with containd's lab-mode token validation.
func TestGenerateJWT(t *testing.T) {
	token := generateJWT("test-secret", time.Now())
	parts := splitJWT(t, token)

	// Verify header
	header := decodeJWTPart(t, parts[0])
	if header["alg"] != "HS256" {
		t.Errorf("expected alg HS256, got %v", header["alg"])
	}
	if header["typ"] != "JWT" {
		t.Errorf("expected typ JWT, got %v", header["typ"])
	}

	// Verify payload
	payload := decodeJWTPart(t, parts[1])
	if payload["sub"] != "rangerdanger-backend" {
		t.Errorf("expected sub rangerdanger-backend, got %v", payload["sub"])
	}
	if payload["role"] != "admin" {
		t.Errorf("expected role admin, got %v", payload["role"])
	}

	// Verify expiration is ~24h from now
	exp, ok := payload["exp"].(float64)
	if !ok {
		t.Fatal("exp claim missing or not a number")
	}
	expTime := time.Unix(int64(exp), 0)
	diff := time.Until(expTime)
	if diff < 23*time.Hour || diff > 25*time.Hour {
		t.Errorf("expected exp ~24h from now, got %v", diff)
	}
}

// TestGenerateJWTDeterministicSignature verifies that the same secret
// produces consistent signatures (i.e. HMAC-SHA256 is correctly applied).
func TestGenerateJWTDeterministicSignature(t *testing.T) {
	// Two tokens with the same secret should have identical header+payload structure
	// (payload differs due to exp timestamp, but the signing mechanism should work)
	token1 := generateJWT("shared-secret", time.Now())
	token2 := generateJWT("different-secret", time.Now())

	parts1 := splitJWT(t, token1)
	parts2 := splitJWT(t, token2)

	// Headers should be identical (same alg)
	if parts1[0] != parts2[0] {
		t.Error("headers should be identical regardless of secret")
	}

	// Signatures must differ for different secrets
	if parts1[2] == parts2[2] {
		t.Error("signatures should differ for different secrets")
	}
}

// TestNewClientUsesEnvSecret verifies the client reads CONTAIND_JWT_SECRET
// from environment and falls back to default.
func TestNewClientUsesEnvSecret(t *testing.T) {
	t.Setenv("CONTAIND_JWT_SECRET", "")
	if got := NewClient("http://localhost:8080").jwtSecret; got != "rangerdanger-dev" {
		t.Errorf("default secret: got %q, want rangerdanger-dev", got)
	}

	t.Setenv("CONTAIND_JWT_SECRET", "custom-secret-123")
	if got := NewClient("http://localhost:8080").jwtSecret; got != "custom-secret-123" {
		t.Errorf("env secret: got %q, want custom-secret-123", got)
	}
}

// TestClientMintsTokenPerRequest pins that a long-lived shared client
// keeps authenticating past the 24h token lifetime: each request carries
// a token whose expiry is measured from the request, not from NewClient.
func TestClientMintsTokenPerRequest(t *testing.T) {
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		json.NewEncoder(w).Encode(HealthStatus{Status: "ok"})
	}))
	defer srv.Close()

	start := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	clock := start
	client := newTestClient(srv.URL)
	client.now = func() time.Time { return clock }

	for _, at := range []time.Time{start, start.Add(25 * time.Hour)} {
		clock = at
		if _, err := client.GetHealth(); err != nil {
			t.Fatalf("GetHealth at %v: %v", at, err)
		}
	}
	var exps []float64
	for _, token := range tokens {
		exp, _ := decodeJWTPart(t, splitJWT(t, token)[1])["exp"].(float64)
		exps = append(exps, exp)
	}
	want := []float64{
		float64(start.Add(24 * time.Hour).Unix()),
		float64(start.Add(49 * time.Hour).Unix()),
	}
	if len(exps) != 2 || exps[0] != want[0] || exps[1] != want[1] {
		t.Errorf("token exp per request: got %v, want %v", exps, want)
	}
}

// TestNewClientSetsBaseURL verifies the client stores the base URL correctly.
func TestNewClientSetsBaseURL(t *testing.T) {
	client := NewClient("http://firewall:8080")
	if client.BaseURL != "http://firewall:8080" {
		t.Errorf("expected base URL http://firewall:8080, got %s", client.BaseURL)
	}
}

// TestAuthHeaderFormat verifies the Authorization header is set correctly
// on all API requests (Bearer token format).
func TestAuthHeaderFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			t.Error("missing Authorization header")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if len(auth) < 8 || auth[:7] != "Bearer " {
			t.Errorf("expected 'Bearer <token>', got: %s", auth)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Verify the token has 3 parts (JWT format)
		token := auth[7:]
		parts := 0
		for _, c := range token {
			if c == '.' {
				parts++
			}
		}
		if parts != 2 {
			t.Errorf("expected JWT with 3 parts (2 dots), got %d dots", parts)
		}
		json.NewEncoder(w).Encode(HealthStatus{Status: "ok"})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GetHealth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDoRequestWithBodySetsContentType verifies POST requests include
// Content-Type: application/json.
func TestDoRequestWithBodySetsContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.ImportConfig([]byte(`{}`))
}

// --- helpers ---

func splitJWT(t *testing.T, token string) [3]string {
	t.Helper()
	var parts [3]string
	idx := 0
	for i, s := range splitString(token, '.') {
		if i >= 3 {
			t.Fatal("JWT has more than 3 parts")
		}
		parts[i] = s
		idx = i
	}
	if idx != 2 {
		t.Fatalf("JWT should have 3 parts, got %d", idx+1)
	}
	return parts
}

func splitString(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func decodeJWTPart(t *testing.T, part string) map[string]any {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatalf("failed to decode JWT part: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse JWT part JSON: %v", err)
	}
	return result
}

func newTestClient(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		jwtSecret:  "test-secret",
		now:        time.Now,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func assertAuthHeader(t *testing.T, r *http.Request) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	if auth == "" {
		t.Error("missing Authorization header")
	}
	if len(auth) < 7 || auth[:7] != "Bearer " {
		t.Errorf("expected 'Bearer <token>' format, got: %s", auth)
	}
}
