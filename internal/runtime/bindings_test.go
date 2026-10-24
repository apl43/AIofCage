package runtime

import (
	"context"
	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
	"sync"
	"testing"
)

func TestIndependentProviderBindings(t *testing.T) {
	st, _ := store.Open(t.TempDir())
	for _, id := range []string{"p1", "p2"} {
		if err := st.SaveProvider(provider.Provider{ID: id, Name: id, Agent: session.Codex}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"sbx-a", "sbx-b"} {
		_ = st.SaveSandbox(sandbox.Sandbox{ID: id, Name: id, Bindings: []sandbox.Binding{{Agent: session.Codex, ProviderID: "p1"}}})
	}
	m := &Manager{store: st, docker: runner{path: "/usr/bin/false"}, locks: map[string]*sync.Mutex{}}
	if err := m.Bind(context.Background(), "sbx-a", session.Codex, "p2"); err != nil {
		t.Fatal(err)
	}
	a, _ := st.Sandbox("sbx-a")
	b, _ := st.Sandbox("sbx-b")
	if a.ProviderFor(session.Codex) != "p2" || b.ProviderFor(session.Codex) != "p1" || a.Placeholders["p2"] == "" {
		t.Fatal("沙箱间配置发生串联")
	}
	if err := m.Bind(context.Background(), "sbx-b", session.Codex, "p2"); err != nil {
		t.Fatal(err)
	}
	b, _ = st.Sandbox("sbx-b")
	if b.Placeholders["p2"] == a.Placeholders["p2"] {
		t.Fatal("不同沙箱共享了占位符")
	}
	if err := m.Bind(context.Background(), "sbx-a", session.ClaudeCode, "p1"); err == nil {
		t.Fatal("接受了不兼容绑定")
	}
}
