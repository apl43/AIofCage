package gatekeeper

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnconfiguredBoundary(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("t", 32)))
	handlers, err := NewHandlers(token)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		target  string
		auth    []string
		origin  string
		status  int
		body    string
	}{
		{"missing auth", handlers.Management, "GET", "/health", nil, "", 401, "unauthorized"},
		{"foreign auth", handlers.Management, "GET", "/health", []string{"Bearer wrong"}, "", 401, "unauthorized"},
		{"duplicate auth", handlers.Management, "GET", "/health", []string{"Bearer " + token, "Bearer wrong"}, "", 401, "unauthorized"},
		{"browser origin", handlers.Management, "GET", "/health", []string{"Bearer " + token}, "https://untrusted.example", 403, "origin_not_allowed"},
		{"not ready", handlers.Management, "GET", "/health", []string{"Bearer " + token}, "", 503, `"ready":false`},
		{"no fake config", handlers.Management, "PUT", "/config", []string{"Bearer " + token}, "", 501, "not_implemented"},
		{"proxy denied", handlers.Proxy, "GET", "https://untrusted.example/", nil, "", 503, "not_configured"},
		{"connect denied", handlers.Proxy, "CONNECT", "untrusted.example:443", nil, "", 503, "not_configured"},
		{"mcp denied", handlers.MCP, "POST", "/mcp", nil, "", 503, "not_configured"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.target, nil)
			for _, auth := range tc.auth {
				r.Header.Add("Authorization", auth)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			tc.handler.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("got %d %s, want %d containing %q", w.Code, w.Body.String(), tc.status, tc.body)
			}
			if strings.Contains(w.Body.String(), token) {
				t.Fatal("response disclosed management token")
			}
		})
	}
	for _, invalid := range []string{"", "short", base64.RawURLEncoding.EncodeToString([]byte("short"))} {
		if _, err := NewHandlers(invalid); err == nil {
			t.Fatal("accepted an invalid management token")
		}
	}
	for _, address := range []string{":8899", "0.0.0.0:8899", "[::]:8899", "localhost:8899", "127.0.0.1:0"} {
		if err := validateAddress(address); err == nil {
			t.Fatalf("accepted unsafe listener %s", address)
		}
	}
}
