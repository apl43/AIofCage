package service

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
	"github.com/shaoxier/AIofCage/internal/vault"
)

// 显式启用后，复制已有供应商的非敏感配置到临时数据目录，使用独立沙箱验证真实两轮会话。
// 不修改用户供应商、绑定或容器；测试任务不包含用户文件内容，最多 15 个模型请求。
func TestConversationEndToEnd(t *testing.T) {
	if os.Getenv("AIOFCAGE_CONVERSATION_E2E") != "1" {
		t.Skip("设置 AIOFCAGE_CONVERSATION_E2E=1 验证真实 CLI 与续聊")
	}
	dir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	providers, err := source.Providers()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var bindings []sandbox.Binding
	for _, agent := range []session.Agent{session.DSH, session.Codex, session.ClaudeCode} {
		for _, p := range providers {
			if p.Agent == agent {
				if err := st.SaveProvider(p); err != nil {
					t.Fatal(err)
				}
				bindings = append(bindings, sandbox.Binding{Agent: agent, ProviderID: p.ID})
				break
			}
		}
	}
	if len(bindings) == 0 {
		t.Skip("没有可复用的已配置供应商")
	}
	keys := vault.NewKeychain()
	manager, err := runtime.NewManager(st, keys)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, keys, manager, nil, nil)
	s, err := app.CreateSandbox(CreateSandboxRequest{Name: "review-conversations-" + time.Now().Format("150405"), ProjectMode: ProjectNew, Bindings: bindings, MaxRequests: 15, MaxTokens: 60000})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.CloseAllTerminals()
		if err := app.DeleteSandbox(s.ID, false); err != nil {
			t.Error(err)
		}
		_ = os.RemoveAll(s.ProjectPath)
	})
	manager.Progress = func(_ string, message string) { t.Log(message) }
	if err := app.StartSandbox(s.ID); err != nil {
		t.Fatal(err)
	}
	events := make(chan map[string]string, 256)
	app.SetEmitter(func(name string, data any) {
		if name == "conversation:event" {
			raw, _ := json.Marshal(data)
			var event map[string]string
			// user 事件带数字附件数，单独忽略；其余事件都是字符串。
			if json.Unmarshal(raw, &event) == nil {
				events <- event
			}
		}
	})
	for index, binding := range bindings {
		t.Run(string(binding.Agent), func(t *testing.T) {
			id := "t-realcheck" + string(rune('a'+index))
			info, err := app.StartConversation(id, s.ID, binding.Agent)
			if err != nil {
				t.Fatal(err)
			}
			defer app.CloseConversation(id)
			for turn, task := range []string{"记住本次测试暗号 cage-check42。只回复这个暗号，不要调用工具。", "上一条的暗号是什么？只回复暗号。"} {
				if err := app.SendConversationMessage(id, task, nil, ConversationOptions{Model: info.Model}); err != nil {
					t.Fatal(err)
				}
				var answer, problem string
				deadline := time.NewTimer(120 * time.Second)
			loop:
				for {
					select {
					case event := <-events:
						if event["id"] != id {
							continue
						}
						switch event["type"] {
						case "text", "delta", "final":
							answer += event["text"]
						case "error":
							problem = event["text"]
						case "done", "stopped":
							break loop
						}
					case <-deadline.C:
						_ = app.StopConversation(id)
						t.Fatal("真实会话超时")
					}
				}
				deadline.Stop()
				if problem != "" {
					t.Fatalf("第%d轮: %s", turn+1, problem)
				}
				if !strings.Contains(answer, "cage-check42") || strings.Contains(answer, "\x1b") {
					t.Fatalf("第%d轮回复不符或含终端转义符", turn+1)
				}
				c, _ := app.conversation(id)
				c.mu.Lock()
				hasID := c.nativeID != ""
				c.mu.Unlock()
				if !hasID {
					t.Fatal("未保存原生会话标识，续聊将丢失上下文")
				}
				t.Logf("第%d轮暗号与原生会话标识通过", turn+1)
			}
		})
	}
}
