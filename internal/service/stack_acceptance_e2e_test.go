package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
	"github.com/shaoxier/AIofCage/internal/vault"
)

// 显式启用后只复制供应商配置，在独立测试沙箱中产生少量真实请求，不改写用户配置。
func stackTestProvider(t *testing.T) provider.Provider {
	t.Helper()
	if os.Getenv("AIOFCAGE_STACK_E2E") != "1" {
		t.Skip("设置 AIOFCAGE_STACK_E2E=1 验证真实重置、恢复和备用供应商")
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
	for _, p := range providers {
		if p.Agent == session.DSH {
			return p
		}
	}
	t.Skip("没有已配置的 DeepSeek Harness 供应商")
	return provider.Provider{}
}

func TestStackRecoveryAndResetEndToEnd(t *testing.T) {
	p := stackTestProvider(t)
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
	s, err := app.CreateSandbox(CreateSandboxRequest{Name: "uat-merge-recovery-" + time.Now().Format("150405"), ProjectMode: ProjectNew,
		Bindings: []sandbox.Binding{{Agent: p.Agent, ProviderID: p.ID}}, MaxRequests: 10, MaxTokens: 30000})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.CloseAllTerminals()
		if err := app.DeleteSandbox(s.ID, false); err != nil {
			t.Error(err)
		}
		if err := os.RemoveAll(s.ProjectPath); err != nil {
			t.Error(err)
		}
	})
	if err := app.StartSandbox(s.ID); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(s.ProjectPath, "uat-preserved.txt")
	if err := os.WriteFile(markerPath, []byte("project-preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := app.StartConversation("t-uatmerge42", s.ID, p.Agent)
	if err != nil {
		t.Fatal(err)
	}
	turn := func(text string, options ConversationOptions) {
		t.Helper()
		events := make(chan map[string]string, 256)
		app.SetEmitter(func(name string, data any) {
			if name != "conversation:event" {
				return
			}
			raw, _ := json.Marshal(data)
			var event map[string]string
			if json.Unmarshal(raw, &event) == nil {
				events <- event
			}
		})
		if err := app.SendConversationMessage(info.ID, text, nil, options); err != nil {
			t.Fatal(err)
		}
		deadline := time.NewTimer(120 * time.Second)
		defer deadline.Stop()
		answer, problem := "", false
		for {
			select {
			case event := <-events:
				if event["id"] != info.ID {
					continue
				}
				switch event["type"] {
				case "text", "delta", "final":
					answer += event["text"]
				case "error":
					problem = true
				case "done", "stopped":
					if problem || !strings.Contains(answer, "cage-merge-42") {
						t.Fatal("恢复前后真实回复没有保留测试暗号，或会话报告错误")
					}
					return
				}
			case <-deadline.C:
				_ = app.StopConversation(info.ID)
				t.Fatal("真实会话超时")
			}
		}
	}
	turn("记住本次测试暗号 cage-merge-42。只回复这个暗号，不调用工具。", ConversationOptions{Model: info.Model, Effort: "high"})
	records, err := app.ListRecoverableConversations()
	if err != nil || len(records) != 1 || records[0].NativeSessionID == "" {
		t.Fatal("首轮真实会话未生成恢复记录", err)
	}
	nativeID := records[0].NativeSessionID
	app.closeConversations()
	app = NewApp(st, keys, manager, nil, nil)
	if err := app.ResetSandbox(s.ID, true, true); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(markerPath); err != nil || string(data) != "project-preserved" {
		t.Fatal("重置后项目文件未保留", err)
	}
	info, err = app.RecoverConversation(info.ID)
	if err != nil || !info.Resumed || info.NativeSessionID != nativeID || info.Effort != "high" {
		t.Fatalf("重建应用和运行环境后恢复信息不符：%+v %v", info, err)
	}
	turn("前一轮记住的暗号是什么？只回复暗号，不调用工具。", ConversationOptions{Model: info.Model, Effort: info.Effort})
	t.Log("应用重建、真实容器重置、项目保留、原生会话续聊和思考强度恢复通过")
	if err := app.ResetSandbox(s.ID, false, false); err != nil {
		t.Fatal(err)
	}
	if records, err := app.ListRecoverableConversations(); err != nil || len(records) != 0 {
		t.Fatal("删除 Agent home 后仍保留失效恢复记录", err)
	}
	t.Log("不保留 Agent home 时清理恢复记录通过")
}

type stackTestVault struct {
	vault.Store
	ref string
}

func (v stackTestVault) Read(ctx context.Context, ref string) ([]byte, error) {
	if ref == v.ref {
		return []byte("synthetic-invalid-credential-for-uat"), nil
	}
	return v.Store.Read(ctx, ref)
}

func TestStackProviderFallbackEndToEnd(t *testing.T) {
	p := stackTestProvider(t)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProvider(p); err != nil {
		t.Fatal(err)
	}
	primary := p
	primary.ID, primary.Name, primary.KeyRef = "uat-primary-invalid", "uat-primary-invalid", "uat.synthetic.invalid"
	if err := st.SaveProvider(primary); err != nil {
		t.Fatal(err)
	}
	keys := stackTestVault{Store: vault.NewKeychain(), ref: primary.KeyRef}
	manager, err := runtime.NewManager(st, keys)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, keys, manager, nil, nil)
	c := newAgentController(context.Background(), app)
	call := func(name string, args any) any {
		t.Helper()
		raw, _ := json.Marshal(args)
		value, err := c.Call(context.Background(), name, raw)
		if err != nil {
			t.Fatalf("%s：%v", name, err)
		}
		return value
	}
	created := call("create_sandbox", map[string]any{"name": "uat-merge-fallback", "provider_id": primary.ID, "max_requests": 10, "max_tokens": 30000}).(map[string]any)
	id, path := created["sandbox_id"].(string), created["project_path"].(string)
	t.Cleanup(func() {
		c.Close()
		if err := app.DeleteSandbox(id, false); err != nil {
			t.Error(err)
		}
		if err := os.RemoveAll(path); err != nil {
			t.Error(err)
		}
	})
	job := call("run_task", map[string]any{"sandbox_id": id, "text": "不要调用工具。只回复 cage-fallback-ok。", "fallback_provider_ids": []string{p.ID}, "timeout_seconds": 240}).(map[string]string)
	deadline := time.Now().Add(250 * time.Second)
	for {
		snapshot := call("get_task", map[string]string{"task_id": job["task_id"]}).(controlTask)
		if snapshot.State == "failed" || snapshot.State == "cancelled" {
			t.Fatal("真实备用供应商任务失败：", snapshot.Error)
		}
		if snapshot.State == "done" {
			if !snapshot.FallbackUsed || snapshot.ProviderID != p.ID || !strings.Contains(snapshot.Text, "cage-fallback-ok") || snapshot.FallbackReason == "" {
				t.Fatal("备用供应商、真实输出或切换原因不符")
			}
			if strings.Contains(snapshot.FallbackReason, "synthetic-invalid") || snapshot.Requests < 2 {
				t.Fatal("错误信息带出诊断原文，或未累计失败与成功尝试的用量")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("任务没有转为完成状态")
		}
		time.Sleep(100 * time.Millisecond)
	}
	rows := call("compare_tasks", map[string]any{"task_ids": []string{job["task_id"]}}).(map[string]any)["tasks"].([]controlTask)
	if len(rows) != 1 || rows[0].State != "done" || rows[0].Text != "" || !strings.Contains(rows[0].OutputSummary, "cage-fallback-ok") || rows[0].DurationMS <= 0 {
		t.Fatal("真实任务对比结果不符")
	}
	call("stop_sandbox", map[string]string{"sandbox_id": id})
	t.Log("真实认证失败后切换供应商、任务完成状态、累计用量、任务对比和释放运行占用通过")
}
