package gatekeeper

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shaoxier/AIofCage/internal/audit"
)

const (
	connectorPlaceholder = "cage-placeholder-connector-pg-1"
	connectorToken       = "synthetic-connector-token-0123456789abcdef"
)

type mcpFixture struct {
	*fixture
	upstreamCalls atomic.Int32
	lastAuth      atomic.Value
	status        atomic.Int32
}

func newMCPFixture(t *testing.T) *mcpFixture {
	t.Helper()
	f := &mcpFixture{fixture: newFixture(t, true)}
	f.status.Store(200)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.upstreamCalls.Add(1)
		f.lastAuth.Store(r.Header.Get("Authorization"))
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &msg)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "session-from-connector")
		w.WriteHeader(int(f.status.Load()))
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID,
			"result": map[string]string{"method": msg.Method, "session": r.Header.Get("Mcp-Session-Id")}})
	}))
	t.Cleanup(upstream.Close)
	f.gate.mcpClient.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "10.9.0.3:8080" {
			t.Errorf("unexpected connector address %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}
	config := testConfig(1)
	config.Connectors = []Connector{{ID: "pg", Placeholder: connectorPlaceholder, Upstream: "http://10.9.0.3:8080",
		Token: connectorToken, Tools: []string{"query"}}}
	f.configure(t, config)
	return f
}

func (f *mcpFixture) call(t *testing.T, path, placeholder, session, body string) (int, string, http.Header) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if placeholder != "" {
		r.Header.Set("Authorization", "Bearer "+placeholder)
	}
	if session != "" {
		r.Header.Set("Mcp-Session-Id", session)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.handlers.MCP.ServeHTTP(w, r)
	return w.Code, w.Body.String(), w.Header()
}

func TestMCPGatewayForwardsWithConnectorToken(t *testing.T) {
	f := newMCPFixture(t)
	seq := f.gate.events.last()
	status, body, header := f.call(t, "/mcp/pg", connectorPlaceholder, "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if status != 200 || !strings.Contains(body, `"method":"initialize"`) || header.Get("Mcp-Session-Id") != "session-from-connector" {
		t.Fatalf("initialize: %d %s %v", status, body, header)
	}
	if auth := f.lastAuth.Load(); auth != "Bearer "+connectorToken {
		t.Fatalf("connector must receive its own token, got %v", auth)
	}
	_, body, _ = f.call(t, "/mcp/pg", connectorPlaceholder, "session-from-connector",
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query","arguments":{"sql":"select 1"}}}`)
	if !strings.Contains(body, `"session":"session-from-connector"`) {
		t.Fatalf("session header must reach the connector: %s", body)
	}
	f.call(t, "/mcp/pg", connectorPlaceholder, "session-from-connector", `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	f.call(t, "/mcp/pg", connectorPlaceholder, "session-from-connector", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if status, body, _ := f.call(t, "/mcp/pg", connectorPlaceholder, "", `{"jsonrpc":"2.0","id":4,"method":"server/discover"}`); status != 200 ||
		!strings.Contains(body, `"code":-32601`) {
		t.Fatalf("server/discover must look like an unsupported method: %d %s", status, body)
	}
	events := f.eventsAfter(t, seq)
	var calls []audit.Event
	for _, e := range events {
		if e.Action == audit.ActionMCPCall {
			calls = append(calls, e)
		}
	}
	// tools/list and notifications are forwarded but not recorded; probes are answered locally.
	if len(calls) != 2 || calls[0].Method != "initialize" || calls[1].ToolID != "query" || calls[1].ServiceID != "pg" ||
		calls[1].Decision != audit.DecisionAllow || calls[1].ReasonCode != "mcp_forwarded" {
		t.Fatalf("unexpected events: %+v", calls)
	}
	if strings.Contains(string(mustJSON(events)), "select 1") {
		t.Fatal("tool arguments must never be recorded")
	}
	if f.upstreamCalls.Load() != 4 {
		t.Fatalf("upstream calls: %d", f.upstreamCalls.Load())
	}
}

func TestMCPGatewayDenials(t *testing.T) {
	f := newMCPFixture(t)
	cases := []struct {
		name, path, placeholder, body string
		status                        int
		reason                        string
	}{
		{"unknown service", "/mcp/other", connectorPlaceholder, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, 404, "mcp_unknown_service"},
		{"no credential", "/mcp/pg", "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, 403, "credential_missing"},
		{"provider placeholder", "/mcp/pg", placeholderA, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, 403, "credential_mismatch"},
		{"invalid json", "/mcp/pg", connectorPlaceholder, `not json`, 400, "mcp_invalid_message"},
		{"tool not allowed", "/mcp/pg", connectorPlaceholder, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"drop_table"}}`, 200, "mcp_tool_denied"},
		{"method not allowed", "/mcp/pg", connectorPlaceholder, `{"jsonrpc":"2.0","id":9,"method":"sampling/createMessage"}`, 200, "mcp_method_denied"},
	}
	for _, c := range cases {
		seq := f.gate.events.last()
		status, body, _ := f.call(t, c.path, c.placeholder, "", c.body)
		if status != c.status {
			t.Errorf("%s: status %d %s", c.name, status, body)
			continue
		}
		if status == 200 && !strings.Contains(body, `"code":-32001`) {
			t.Errorf("%s: expected JSON-RPC error, got %s", c.name, body)
		}
		events := f.eventsAfter(t, seq)
		if last := events[len(events)-1]; last.Decision != audit.DecisionDeny || last.ReasonCode != c.reason {
			t.Errorf("%s: event %+v", c.name, last)
		}
	}
	if f.upstreamCalls.Load() != 0 {
		t.Fatalf("denied messages must not reach the connector: %d", f.upstreamCalls.Load())
	}
	f.status.Store(401)
	if status, _, _ := f.call(t, "/mcp/pg", connectorPlaceholder, "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`); status != 502 {
		t.Fatalf("connector auth failure must not surface as an auth challenge: %d", status)
	}
}

func TestConnectorConfigValidation(t *testing.T) {
	valid := Connector{ID: "pg", Placeholder: connectorPlaceholder, Upstream: "http://10.9.0.3:8080", Token: connectorToken}
	cases := map[string]func(*Connector){
		"public upstream":       func(c *Connector) { c.Upstream = "http://8.8.8.8:80" },
		"https upstream":        func(c *Connector) { c.Upstream = "https://10.9.0.3:8080" },
		"hostname upstream":     func(c *Connector) { c.Upstream = "http://connector:8080" },
		"loopback upstream":     func(c *Connector) { c.Upstream = "http://127.0.0.1:8080" },
		"upstream with path":    func(c *Connector) { c.Upstream = "http://10.9.0.3:8080/x" },
		"short token":           func(c *Connector) { c.Token = "short" },
		"bad id":                func(c *Connector) { c.ID = "Bad ID" },
		"placeholder of secret": func(c *Connector) { c.Placeholder = placeholderA },
	}
	for name, mutate := range cases {
		connector := valid
		mutate(&connector)
		config := testConfig(1)
		config.Connectors = []Connector{connector}
		if _, err := compile(config); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	config := testConfig(1)
	config.Connectors = []Connector{valid}
	if _, err := compile(config); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

// An agent that sends the session close while exiting hangs up at once. The connector must still
// end the session, and neither that nor a repeated close is a connector failure.
func TestMCPClientHangUpIsNotAnUpstreamFailure(t *testing.T) {
	f := newMCPFixture(t)
	seq := f.gate.events.last()
	send := func(method, body string, cancelled bool) int {
		r := httptest.NewRequest(method, "/mcp/pg", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+connectorPlaceholder)
		r.Header.Set("Mcp-Session-Id", "session-from-connector")
		if cancelled {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			r = r.WithContext(ctx)
		}
		w := httptest.NewRecorder()
		f.handlers.MCP.ServeHTTP(w, r)
		return w.Code
	}
	send(http.MethodDelete, "", true)
	if f.upstreamCalls.Load() != 1 {
		t.Fatalf("a close from a client that hung up must still reach the connector: %d", f.upstreamCalls.Load())
	}
	f.status.Store(404)
	if status := send(http.MethodDelete, "", false); status != 404 {
		t.Fatalf("repeated close: %d", status)
	}
	f.status.Store(200)
	send(http.MethodPost, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"query"}}`, true)
	events, _, _ := f.gate.events.since(seq)
	var calls []audit.Event
	for _, e := range events {
		if e.Decision == audit.DecisionError {
			t.Errorf("hang-ups and repeated closes are not errors: %+v", e)
		}
		if e.Action == audit.ActionMCPCall {
			calls = append(calls, e)
		}
	}
	if len(calls) != 1 || calls[0].ReasonCode != "client_closed" || calls[0].ToolID != "query" {
		t.Fatalf("a tool call abandoned by the agent is recorded as client_closed: %+v", calls)
	}
}
