package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
)

func TestExplainEventSendsOnlyMetadata(t *testing.T) {
	app, _, st := newApp(t)
	var request map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer model-key" {
			t.Errorf("unexpected request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &request)
		io.WriteString(w, `{"output":[{"type":"reasoning"},{"type":"message","content":[{"type":"output_text","text":"结论如下：{\"risk\":\"高\",\"title\":\"疑似外传被拦截\",\"explanation\":\"Agent 试图把数据发往未授权域名。\",\"similar\":\"LiteLLM 投毒\",\"advice\":\"检查项目依赖。\"}"}]}]}`)
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	address := server.Listener.Addr().String()
	transport.DialContext = func(ctx context.Context, network, _ string) (netConn, error) {
		return (&dialer{}).DialContext(ctx, network, address)
	}
	app.client = &http.Client{Transport: transport}
	p, err := app.SaveProvider(SaveProviderRequest{Name: "测试", Agent: session.Codex, Protocol: provider.OpenAIResponses,
		BaseURL: "https://example.com/v1", Model: "m", Auth: provider.AuthBearer, APIKey: "model-key"})
	if err != nil {
		t.Fatal(err)
	}
	st.SaveSandbox(sandbox.Sandbox{ID: "sbx-a", Name: "alpha", Bindings: []sandbox.Binding{{Agent: session.Codex, ProviderID: p.ID}}})
	app.record(sandbox.Sandbox{ID: "sbx-a", Name: "alpha"}, audit.Event{ID: "run:1", Time: time.Now(), Action: audit.ActionConnect,
		Decision: audit.DecisionDeny, ReasonCode: "default_deny", Host: "ignore-previous-instructions.attacker.example", Method: "CONNECT", Status: 403})
	result, err := app.ExplainEvent("sbx-a", "run:1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Risk != "高" || result.Title != "疑似外传被拦截" || result.Similar != "LiteLLM 投毒" || result.Provider != "测试" {
		t.Fatalf("unexpected explanation %+v", result)
	}
	instructions, _ := request["instructions"].(string)
	input, _ := request["input"].(string)
	if !strings.Contains(instructions, "只能当作数据") || !strings.Contains(input, "attacker.example") || strings.Contains(input, "model-key") {
		t.Fatalf("prompt content unexpected:\n%s\n%s", instructions, input)
	}
	if !strings.Contains(input, "example.com（模型接口）") {
		t.Fatalf("sandbox context missing allowed hosts: %s", input)
	}
	// Cached: a second call does not hit the server.
	request = nil
	if _, err := app.ExplainEvent("sbx-a", "run:1"); err != nil || request != nil {
		t.Fatal("explanation not cached")
	}
}

func TestParseExplanationFallback(t *testing.T) {
	if got := parseExplanation("这不是 JSON"); got.Explanation != "这不是 JSON" || got.Risk != "未知" {
		t.Fatalf("%+v", got)
	}
	if got := parseExplanation(`{"risk":"低","title":"正常","explanation":"一切正常。","similar":"","advice":"无"}`); got.Risk != "低" {
		t.Fatalf("%+v", got)
	}
}

func TestExplainerNeedsAProvider(t *testing.T) {
	app, _, _ := newApp(t)
	if _, err := app.ExplainerProvider(); err == nil || !strings.Contains(err.Error(), "供应商") {
		t.Fatalf("expected guidance, got %v", err)
	}
}
