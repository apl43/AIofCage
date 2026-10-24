package gatekeeper

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/buildinfo"
)

// Handlers separates the management plane from the untrusted data planes.
type Handlers struct {
	Management http.Handler
	Proxy      http.Handler
	MCP        http.Handler
}

const maxConfigBytes = 1 << 20

// NewHandlers builds the gatekeeper with a management token. The proxy rejects everything until
// the desktop app has delivered a valid configuration through PUT /config.
func NewHandlers(token string) (Handlers, error) {
	gate, err := NewGate()
	if err != nil {
		return Handlers{}, err
	}
	return gate.Handlers(token)
}

func (g *Gate) Handlers(token string) (Handlers, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) < 32 || len(decoded) > 128 {
		return Handlers{}, errors.New("管理 token 必须是 32 至 128 随机字节的无填充 URL-safe Base64 编码")
	}
	expected := sha256.Sum256([]byte("Bearer " + token))
	management := http.NewServeMux()
	management.HandleFunc("GET /health", g.serveHealth)
	management.HandleFunc("PUT /config", g.serveConfig)
	management.HandleFunc("GET /events", g.serveEvents)
	management.HandleFunc("GET /usage", g.serveUsage)
	management.HandleFunc("GET /ca", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(g.caPEM)
	})
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
		if r.URL.RawQuery != "" {
			writeProblem(w, http.StatusBadRequest, "query_not_allowed")
			return
		}
		management.ServeHTTP(w, r)
	})
	// The MCP gateway is not implemented yet; it stays closed.
	mcp := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusServiceUnavailable, "not_configured")
	})
	return Handlers{Management: authenticated, Proxy: g.proxy, MCP: mcp}, nil
}

type healthResponse struct {
	Version       string `json:"version"`
	Stage         string `json:"stage"`
	Ready         bool   `json:"ready"`
	Reason        string `json:"reason,omitempty"`
	RunID         string `json:"run_id"`
	PolicyVersion uint64 `json:"policy_version,omitempty"`
	LastEvent     uint64 `json:"last_event"`
}

func (g *Gate) serveHealth(w http.ResponseWriter, _ *http.Request) {
	response := healthResponse{Version: buildinfo.Version, Stage: buildinfo.Stage, RunID: g.runID, LastEvent: g.events.last()}
	active := g.current()
	if active == nil {
		response.Reason = "not_configured"
		writeJSON(w, http.StatusServiceUnavailable, response)
		return
	}
	response.Ready, response.PolicyVersion = true, active.config.Policy.Version
	writeJSON(w, http.StatusOK, response)
}

// serveConfig atomically replaces policy, secrets and limits. The request body is never logged
// and validation errors never contain secret values.
func (g *Gate) serveConfig(w http.ResponseWriter, r *http.Request) {
	var config Config
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxConfigBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil || decoder.More() {
		writeProblem(w, http.StatusBadRequest, "invalid_json")
		return
	}
	compiled, err := compile(config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		}{"invalid_config", err.Error()})
		return
	}
	g.mu.Lock()
	if g.active != nil && config.Policy.Version <= g.active.config.Policy.Version {
		g.mu.Unlock()
		writeProblem(w, http.StatusConflict, "stale_policy_version")
		return
	}
	limits := config.Limits
	g.stats.QuotaRequests = max(g.stats.QuotaRequests, limits.BaselineRequests)
	g.stats.QuotaTokens = max(g.stats.QuotaTokens, limits.BaselineTokens)
	g.warned = map[string]bool{}
	g.active = compiled
	warnings := g.quotaWarningsLocked(limits)
	g.mu.Unlock()
	g.events.append(audit.Event{Action: audit.ActionPolicyChange, Decision: audit.DecisionNotApplicable,
		ReasonCode: "config_applied", SessionID: config.Policy.SessionID, PolicyVersion: config.Policy.Version})
	for _, reason := range warnings {
		g.events.append(audit.Event{Action: audit.ActionQuotaWarning, Decision: audit.DecisionNotApplicable,
			ReasonCode: reason, SessionID: config.Policy.SessionID, PolicyVersion: config.Policy.Version})
	}
	writeJSON(w, http.StatusOK, struct {
		PolicyVersion uint64 `json:"policy_version"`
	}{config.Policy.Version})
}

type usageResponse struct {
	RunID         string                `json:"run_id"`
	PolicyVersion uint64                `json:"policy_version,omitempty"`
	Requests      uint64                `json:"requests"`
	Denied        uint64                `json:"denied"`
	QuotaRequests uint64                `json:"quota_requests"`
	QuotaTokens   uint64                `json:"quota_tokens"`
	Limits        Limits                `json:"limits"`
	Secrets       map[string]secretStat `json:"secrets"`
	LastEvent     uint64                `json:"last_event"`
}

func (g *Gate) serveUsage(w http.ResponseWriter, _ *http.Request) {
	g.mu.RLock()
	response := usageResponse{RunID: g.runID, Requests: g.stats.Requests, Denied: g.stats.Denied,
		QuotaRequests: g.stats.QuotaRequests, QuotaTokens: g.stats.QuotaTokens, Secrets: map[string]secretStat{}}
	if g.active != nil {
		response.PolicyVersion = g.active.config.Policy.Version
		response.Limits = g.active.config.Limits
	}
	for id, stat := range g.stats.Secrets {
		response.Secrets[id] = *stat
	}
	g.mu.RUnlock()
	response.LastEvent = g.events.last()
	writeJSON(w, http.StatusOK, response)
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
