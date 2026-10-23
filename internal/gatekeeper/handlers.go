package gatekeeper

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/shaoxier/AIofCage/internal/buildinfo"
)

// Handlers separates the management and untrusted data planes. It never forwards traffic.
type Handlers struct {
	Management http.Handler
	Proxy      http.Handler
	MCP        http.Handler
}

func NewHandlers(token string) (Handlers, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) < 32 || len(decoded) > 128 {
		return Handlers{}, errors.New("管理 token 必须是 32 至 128 随机字节的无填充 URL-safe Base64 编码")
	}
	expected := sha256.Sum256([]byte("Bearer " + token))
	management := http.NewServeMux()
	management.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, struct {
			Version string `json:"version"`
			Stage   string `json:"stage"`
			Ready   bool   `json:"ready"`
			Reason  string `json:"reason"`
		}{buildinfo.Version, buildinfo.Stage, false, "not_configured"})
	})
	for _, route := range []string{"PUT /config", "GET /events", "GET /usage"} {
		management.HandleFunc(route, func(w http.ResponseWriter, _ *http.Request) {
			writeProblem(w, http.StatusNotImplemented, "not_implemented")
		})
	}
	authenticated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No browser may access management. Origin-less desktop HTTP calls use the token.
		if len(r.Header.Values("Origin")) != 0 {
			writeProblem(w, http.StatusForbidden, "origin_not_allowed")
			return
		}
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeProblem(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		actual := sha256.Sum256([]byte(values[0]))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeProblem(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		management.ServeHTTP(w, r)
	})
	deny := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusServiceUnavailable, "not_configured")
	})
	return Handlers{Management: authenticated, Proxy: deny, MCP: deny}, nil
}

func writeProblem(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, struct {
		Code string `json:"code"`
	}{code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
