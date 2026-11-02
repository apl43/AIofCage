package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
	"github.com/shaoxier/AIofCage/internal/vault"
)

// 显式启用后，复制已有供应商的非敏感配置到临时数据目录，使用独立沙箱验证真实两轮会话。
// 每个智能体使用独立沙箱和8次请求额度，失败与重试不会耗尽其他智能体的额度。
// 不修改用户供应商、绑定或容器；测试任务不包含用户文件内容。
func TestConversationEndToEnd(t *testing.T) {
	if os.Getenv("AIOFCAGE_CONVERSATION_E2E") != "1" {
		t.Skip("设置 AIOFCAGE_CONVERSATION_E2E=1 验证真实 CLI 与续聊")
	}
	for _, agent := range []session.Agent{session.DSH, session.Codex, session.ClaudeCode} {
		t.Run(string(agent), func(t *testing.T) {
			checkConversationAgent(t, configuredProvider(t, agent))
		})
	}
}

// 显式启用后，用第一个已配置的供应商在一次性沙箱里按给出的思考强度各开一个会话、发一句话，看真实供应商认不认。
// 例：AIOFCAGE_EFFORT_E2E=codex:max,ultra。思考强度在首条消息后锁定，所以每档一个会话。
func TestConversationEffortEndToEnd(t *testing.T) {
	spec := os.Getenv("AIOFCAGE_EFFORT_E2E")
	if spec == "" {
		t.Skip("设置 AIOFCAGE_EFFORT_E2E=codex:max,ultra 验证真实供应商接受的思考强度")
	}
	agent, levels, _ := strings.Cut(spec, ":")
	p := configuredProvider(t, session.Agent(agent))
	app, s, events := conversationSandbox(t, p, 40, 600000)
	for _, effort := range strings.Split(levels, ",") {
		id := "t-effort" + effort
		info, err := app.StartConversation(id, s.ID, p.Agent)
		if err != nil {
			t.Fatal(err)
		}
		if err := app.SendConversationMessage(id, "只回复一个字：好。不要调用工具。", nil, ConversationOptions{Model: info.Model, Effort: effort}); err != nil {
			t.Fatalf("%s：%v", effort, err)
		}
		answer, problem := waitConversationTurn(t, app, id, events, 300*time.Second)
		app.CloseConversation(id)
		if problem != "" {
			t.Errorf("%s %s：%s", info.Model, effort, problem)
			continue
		}
		t.Logf("%s %s：回复 %q", info.Model, effort, strings.TrimSpace(answer))
	}
}

// configuredProvider 取用户已配置的第一个这款智能体的供应商；只读用户数据目录，不改动。
func configuredProvider(t *testing.T, agent session.Agent) provider.Provider {
	t.Helper()
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
	for _, p := range providers {
		if p.Agent == agent {
			return p
		}
	}
	t.Skip("没有可复用的已配置供应商")
	return provider.Provider{}
}

// conversationSandbox 在临时数据目录里用这个供应商建一个一次性沙箱并启动，测试结束时删除；失败时打出门卫的脱敏元数据。
func conversationSandbox(t *testing.T, p provider.Provider, maxRequests, maxTokens uint64) (*App, SandboxView, chan map[string]string) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProvider(p); err != nil {
		t.Fatal(err)
	}
	keys := vault.NewKeychain()
	manager, err := runtime.NewManager(st, keys)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, keys, manager, nil, nil)
	s, err := app.CreateSandbox(CreateSandboxRequest{Name: "review-conversation-" + string(p.Agent) + "-" + time.Now().Format("150405"), ProjectMode: ProjectNew, Bindings: []sandbox.Binding{{Agent: p.Agent, ProviderID: p.ID}}, MaxRequests: maxRequests, MaxTokens: maxTokens})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = manager.Events(ctx, s.ID, "", func(event audit.Event) error {
				t.Logf("门卫元数据: %s %s%s %s/%s status=%d elapsed=%dms", event.Action, event.Host, event.Path, event.Decision, event.ReasonCode, event.Status, event.DurationMS)
				return nil
			})
		}
		app.CloseAllTerminals()
		if err := app.DeleteSandbox(s.ID, false); err != nil {
			t.Error(err)
		}
		_ = os.RemoveAll(s.ProjectPath)
	})
	manager.Progress = func(_ string, update runtime.Update) { t.Log(update.Step, update.Detail) }
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
	return app, s, events
}

// waitConversationTurn 等这一轮结束，返回回复正文和错误提示。
func waitConversationTurn(t *testing.T, app *App, id string, events chan map[string]string, timeout time.Duration) (answer, problem string) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
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
				return answer, problem
			}
		case <-deadline.C:
			_ = app.StopConversation(id)
			t.Fatal("真实会话超时")
		}
	}
}

func checkConversationAgent(t *testing.T, p provider.Provider) {
	t.Helper()
	app, s, events := conversationSandbox(t, p, 8, 30000)
	id := "t-realcheck"
	info, err := app.StartConversation(id, s.ID, p.Agent)
	if err != nil {
		t.Fatal(err)
	}
	defer app.CloseConversation(id)
	for turn, task := range []string{"记住本次测试暗号 cage-check42。只回复这个暗号，不要调用工具。", "上一条的暗号是什么？只回复暗号。"} {
		if err := app.SendConversationMessage(id, task, nil, ConversationOptions{Model: info.Model}); err != nil {
			t.Fatal(err)
		}
		answer, problem := waitConversationTurn(t, app, id, events, 120*time.Second)
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
}
