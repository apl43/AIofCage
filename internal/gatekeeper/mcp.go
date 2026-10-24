package gatekeeper

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
)

// The MCP gateway is the only way from the sandbox to connector-backed MCP services. Every
// message is checked against the service placeholder and the allowed tools; tool calls are
// recorded by name only, never with arguments or results.

const (
	maxMCPBody     = 4 << 20
	mcpCallTimeout = 10 * time.Minute
)

var mcpMethods = map[string]bool{"initialize": true, "ping": true, "tools/list": true, "tools/call": true,
	"resources/list": true, "resources/read": true, "resources/templates/list": true, "prompts/list": true,
	"prompts/get": true, "completion/complete": true, "logging/setLevel": true}

// Newer protocol probes that clients fall back from (Claude Code tries server/discover before
// initialize). They are answered like a server without them and not logged as denials.
var mcpProbes = map[string]bool{"server/discover": true}

// Handshakes and listings would only add noise to the request log; data access is recorded.
var mcpRecorded = map[string]bool{"initialize": true, "tools/call": true, "resources/read": true, "prompts/get": true}

var (
	mcpMethodPattern = regexp.MustCompile(`^[a-z][a-zA-Z/_]{0,63}$`)
	toolPattern      = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

var mcpForwardHeaders = []string{"Content-Type", "Accept", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-Id"}

func newMCPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			ResponseHeaderTimeout: mcpCallTimeout, IdleConnTimeout: 90 * time.Second, MaxIdleConnsPerHost: 4},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type mcpMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Name string `json:"name"`
	} `json:"params"`
}

func (g *Gate) serveMCP(w http.ResponseWriter, r *http.Request) {
	started := g.now()
	active := g.current()
	if active == nil {
		writeProblem(w, http.StatusServiceUnavailable, "not_configured")
		return
	}
	event := audit.Event{Action: audit.ActionMCPCall, Decision: audit.DecisionDeny, Method: r.Method,
		SessionID: active.config.Policy.SessionID, PolicyVersion: active.config.Policy.Version}
	// Denials use 403, never 401: a 401 would make agents start an OAuth login against us.
	deny := func(status int, reason string) {
		event.ReasonCode, event.Status = reason, status
		event.DurationMS = g.now().Sub(started).Milliseconds()
		g.deny(event)
		writeProblem(w, status, reason)
	}
	id, ok := strings.CutPrefix(r.URL.Path, "/mcp/")
	connector, found := active.connectors[id]
	if !ok || !found || r.URL.RawQuery != "" {
		deny(http.StatusNotFound, "mcp_unknown_service")
		return
	}
	event.ServiceID = id
	values := r.Header.Values("Authorization")
	if len(values) == 0 {
		deny(http.StatusForbidden, "credential_missing")
		return
	}
	if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte("Bearer "+connector.Placeholder)) != 1 {
		deny(http.StatusForbidden, "credential_mismatch")
		return
	}
	var body []byte
	var msg mcpMessage
	switch r.Method {
	case http.MethodPost:
		data, err := io.ReadAll(io.LimitReader(r.Body, maxMCPBody+1))
		if err != nil || len(data) > maxMCPBody {
			deny(http.StatusRequestEntityTooLarge, "body_too_large")
			return
		}
		if json.Unmarshal(data, &msg) != nil {
			deny(http.StatusBadRequest, "mcp_invalid_message")
			return
		}
		body = data
		// Agent-chosen strings are only recorded when they look like a method or tool name.
		event.Method = ""
		if mcpMethodPattern.MatchString(msg.Method) {
			event.Method = msg.Method
		}
		if msg.Method == "tools/call" && toolPattern.MatchString(msg.Params.Name) {
			event.ToolID = msg.Params.Name
		}
		if mcpProbes[msg.Method] && len(msg.ID) > 0 {
			writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": msg.ID,
				"error": map[string]any{"code": -32601, "message": "Method not found"}})
			return
		}
		if msg.Method != "" && !strings.HasPrefix(msg.Method, "notifications/") && !mcpMethods[msg.Method] {
			g.denyRPC(w, msg, event, started, "mcp_method_denied", "AIofCage 门卫不允许这个 MCP 方法")
			return
		}
		if msg.Method == "tools/call" && len(connector.Tools) > 0 && !slices.Contains(connector.Tools, msg.Params.Name) {
			g.denyRPC(w, msg, event, started, "mcp_tool_denied",
				"AIofCage 门卫拒绝了这个工具调用：该服务只开放 "+strings.Join(connector.Tools, "、"))
			return
		}
	case http.MethodDelete:
		event.Method = "session/close"
	default:
		w.Header().Set("Allow", "POST, DELETE")
		writeProblem(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	g.forwardMCP(w, r, connector, body, event, started)
}

// denyRPC answers a refused message with a JSON-RPC error so the agent can tell the user why.
func (g *Gate) denyRPC(w http.ResponseWriter, msg mcpMessage, event audit.Event, started time.Time, reason, text string) {
	event.ReasonCode, event.Status = reason, http.StatusForbidden
	event.DurationMS = g.now().Sub(started).Milliseconds()
	g.deny(event)
	if len(msg.ID) == 0 || string(msg.ID) == "null" {
		writeProblem(w, http.StatusForbidden, reason)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": msg.ID,
		"error": map[string]any{"code": -32001, "message": text}})
}

func (g *Gate) forwardMCP(w http.ResponseWriter, r *http.Request, connector Connector, body []byte, event audit.Event, started time.Time) {
	parent := r.Context()
	if r.Method == http.MethodDelete {
		// Clients often send the session close while exiting and hang up at once; the connector
		// must still end the session and its server process.
		parent = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(parent, mcpCallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, strings.TrimSuffix(connector.Upstream, "/")+"/mcp", bytes.NewReader(body))
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error")
		return
	}
	for _, name := range mcpForwardHeaders {
		if value := r.Header.Get(name); value != "" {
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("Authorization", "Bearer "+connector.Token)
	event.BytesIn = int64(len(body))
	resp, err := g.mcpClient.Do(req)
	if err != nil && r.Context().Err() != nil {
		// The agent hung up before the answer: not a connector failure.
		event.Decision, event.ReasonCode = audit.DecisionAllow, "client_closed"
		g.recordMCP(event, started, mcpRecorded[event.Method])
		return
	}
	if err != nil {
		event.Decision, event.ReasonCode, event.Status = audit.DecisionError, "upstream_failed", http.StatusBadGateway
		g.recordMCP(event, started, true)
		writeProblem(w, http.StatusBadGateway, "upstream_failed")
		return
	}
	defer resp.Body.Close()
	status := resp.StatusCode
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		// The connector token is ours; never let the agent see an auth challenge.
		status = http.StatusBadGateway
	}
	for _, name := range []string{"Content-Type", "Mcp-Session-Id"} {
		if value := resp.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	written, _ := copyFlushing(w, resp.Body)
	event.Status, event.BytesOut = status, written
	event.Decision, event.ReasonCode = audit.DecisionAllow, "mcp_forwarded"
	// Closing a session that already ended (404) is routine, e.g. a second close on exit.
	failed := status >= 400 && !(r.Method == http.MethodDelete && status == http.StatusNotFound)
	if failed {
		event.Decision, event.ReasonCode = audit.DecisionError, "upstream_status"
	}
	g.recordMCP(event, started, mcpRecorded[event.Method] || failed)
}

func (g *Gate) recordMCP(event audit.Event, started time.Time, visible bool) {
	if !visible {
		return
	}
	event.DurationMS = g.now().Sub(started).Milliseconds()
	g.mu.Lock()
	g.stats.Requests++
	g.mu.Unlock()
	g.events.append(event)
}

// copyFlushing streams the connector's answer, flushing so SSE responses are not held back.
func copyFlushing(w http.ResponseWriter, body io.Reader) (int64, error) {
	controller := http.NewResponseController(w)
	buffer := make([]byte, 32<<10)
	var total int64
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return total, writeErr
			}
			total += int64(n)
			_ = controller.Flush()
		}
		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
	}
}
