package service

import (
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
)

func TestConversationValidation(t *testing.T) {
	for _, options := range []ConversationOptions{{Model: ""}, {Model: "test\x1b[200~"}, {Model: "test", Effort: "invalid"}} {
		if validateConversationOptions(options) == nil {
			t.Fatalf("接受了非法选项：%+v", options)
		}
	}
	if err := validateConversationOptions(ConversationOptions{Model: "vendor/model", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	buffer := &tailBuffer{limit: 8}
	_, _ = buffer.Write([]byte(strings.Repeat("a", 100)))
	_, _ = buffer.Write([]byte("END"))
	if string(buffer.data) != "aaaaaEND" {
		t.Fatalf("诊断未按上限保存尾部：%q", buffer.data)
	}
	for _, tc := range []struct{ detail, want string }{
		{"401 unauthorized", "供应商认证失败"},
		{"429 rate limit", "供应商限流"},
		{"dial timeout", "请求超时"},
		{"connection refused", "无法连接供应商"},
	} {
		if got := classifyConversationFailure(tc.detail); !strings.Contains(got, tc.want) {
			t.Fatalf("%q: %q", tc.detail, got)
		}
	}
}

func TestConversationRecoveryMetadataRoundTrip(t *testing.T) {
	app, _, st := newApp(t)
	p := provider.Provider{ID: "p1", Name: "测试供应商", Agent: session.Codex}
	if err := st.SaveProvider(p); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSandbox(sandbox.Sandbox{ID: "sbx-recovery", Name: "可恢复", Bindings: []sandbox.Binding{{Agent: session.Codex, ProviderID: p.ID}}}); err != nil {
		t.Fatal(err)
	}
	app.now = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }
	item := app.conversationRecoveryFrom(&conversation{id: "t-recovery", sandbox: sandbox.Sandbox{ID: "sbx-recovery", Bindings: []sandbox.Binding{{Agent: session.Codex, ProviderID: p.ID}}}, agent: session.Codex,
		provider: p, model: "model-a", effort: "high", nativeID: "native-session-1"})
	if err := app.saveConversationRecovery(item); err != nil {
		t.Fatal(err)
	}
	items, err := app.ListRecoverableConversations()
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %+v %v", items, err)
	}
	if items[0].ID != item.ID || items[0].SandboxName != "可恢复" || items[0].ProviderName != p.Name || !items[0].Ready {
		t.Fatalf("unexpected recovery view: %+v", items[0])
	}
	if raw := st.Setting(conversationRecoverySetting); strings.Contains(raw, "message") || !strings.Contains(raw, "native-session-1") {
		t.Fatalf("unexpected persisted metadata: %s", raw)
	}
	if err := app.removeConversationRecovery(item.ID); err != nil {
		t.Fatal(err)
	}
	if items, err := app.ListRecoverableConversations(); err != nil || len(items) != 0 {
		t.Fatalf("remove: %+v %v", items, err)
	}
}
