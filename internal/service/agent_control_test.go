package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/sandbox"
)

func TestAgentControlScopeAndNoCredentialExposure(t *testing.T) {
	a, _, st := newApp(t)
	p, err := a.SaveProvider(codexRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSandbox(sandbox.Sandbox{ID: "existing", Name: "existing"}); err != nil {
		t.Fatal(err)
	}
	c := newAgentController(context.Background(), a)
	defer c.Close()
	value, err := c.Call(context.Background(), "list_providers", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(value)
	for _, bad := range []string{p.KeyRef, p.BaseURL, "secret-key-123", "key_ref"} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("控制入口暴露了凭据相关信息: %s", bad)
		}
	}
	for _, tool := range []string{"stop_sandbox", "sandbox_usage", "run_task"} {
		if _, err := c.Call(context.Background(), tool, json.RawMessage(`{"sandbox_id":"existing","text":"test"}`)); err == nil {
			t.Fatal("允许控制原沙箱", tool)
		}
	}
	for _, tool := range []string{"stop_sandbox", "sandbox_usage"} {
		if _, err := c.Call(context.Background(), tool, json.RawMessage(`{"sandbox_id":"existing"}`)); err == nil {
			t.Fatal("允许控制原沙箱", tool)
		}
	}
	value, err = c.Call(context.Background(), "list_sandboxes", json.RawMessage(`{}`))
	raw, _ = json.Marshal(value)
	if err != nil || strings.Contains(string(raw), "existing") {
		t.Fatal("列出不属于本会话的沙箱")
	}
	if _, err := c.Call(context.Background(), "create_sandbox", json.RawMessage(`{"name":"bad","provider_id":"x","project_path":"/"}`)); err == nil {
		t.Fatal("接受任意项目路径")
	}
	for _, quota := range []string{`"max_tokens":0`, `"max_tokens":100001`, `"max_requests":101`} {
		if _, err := c.Call(context.Background(), "create_sandbox", json.RawMessage(`{"name":"bad","provider_id":"`+p.ID+`",`+quota+`}`)); err == nil {
			t.Fatal("接受越界额度")
		}
	}
}

func TestAgentControlCapsAndCancellation(t *testing.T) {
	a, _, st := newApp(t)
	p, err := a.SaveProvider(codexRequest())
	if err != nil {
		t.Fatal(err)
	}
	s := sandbox.Sandbox{ID: "own", Name: "own", Bindings: []sandbox.Binding{{Agent: p.Agent, ProviderID: p.ID}}}
	if err := st.SaveSandbox(s); err != nil {
		t.Fatal(err)
	}
	c := newAgentController(context.Background(), a)
	c.owned[s.ID] = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.tasks["one"] = &controlTask{ID: "one", SandboxID: "a", State: "running", cancel: cancel}
	c.tasks["two"] = &controlTask{ID: "two", SandboxID: "b", State: "starting", cancel: cancel}
	if _, err := c.Call(context.Background(), "run_task", json.RawMessage(`{"sandbox_id":"own","text":"test"}`)); err == nil {
		t.Fatal("允许超过并发上限")
	}
	if _, err := c.Call(context.Background(), "cancel_task", json.RawMessage(`{"task_id":"one"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("取消没有触发")
	}
	c.Close()
}

func TestAgentControlComparesTaskMetadata(t *testing.T) {
	a, _, _ := newApp(t)
	c := newAgentController(context.Background(), a)
	defer c.Close()
	first := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	c.tasks["job-1"] = &controlTask{ID: "job-1", SandboxID: "sbx-1", SandboxName: "一个", Agent: "codex", ProviderName: "供应商一", Model: "model-a", State: "done", Text: "第一份很长的结果", OutputSummary: "第一份很长的结果", StartedAt: first, FinishedAt: first.Add(2 * time.Second), DurationMS: 2000, Requests: 1, Tokens: 120}
	c.tasks["job-2"] = &controlTask{ID: "job-2", SandboxID: "sbx-2", SandboxName: "两个", Agent: "claude-code", ProviderName: "供应商二", Model: "model-b", State: "failed", OutputSummary: "失败摘要", StartedAt: first.Add(time.Second), FinishedAt: first.Add(4 * time.Second), DurationMS: 3000, Error: "失败"}
	value, err := c.Call(context.Background(), "compare_tasks", json.RawMessage(`{"task_ids":["job-2","job-1"]}`))
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := value.(map[string]any)["tasks"].([]controlTask)
	if !ok || len(rows) != 2 || rows[0].ID != "job-1" || rows[1].ID != "job-2" || rows[0].Tokens != 120 || rows[1].OutputSummary != "失败摘要" {
		t.Fatalf("unexpected comparison: %#v", value)
	}
	if rows[0].Text != "" {
		t.Fatal("comparison returned full task output")
	}
}

func TestProviderFallbackFailureClassification(t *testing.T) {
	for _, detail := range []string{"401 unauthorized", "429 rate limit", "deadline exceeded", "connection refused", "DNS lookup failed"} {
		if !retryableProviderFailure(detail) {
			t.Fatalf("应触发备用供应商：%q", detail)
		}
	}
	for _, detail := range []string{"invalid request", "模型输出格式错误"} {
		if retryableProviderFailure(detail) {
			t.Fatalf("不应因非供应商故障切换：%q", detail)
		}
	}
}

func TestTaskSummaryPreservesChineseCharacters(t *testing.T) {
	text := strings.Repeat("验", 501)
	if got, want := outputSummary(text), strings.Repeat("验", 500)+"…"; got != want {
		t.Fatal("任务摘要未按完整字符截断")
	}
}

func TestControlFailureHintDoesNotReturnStderr(t *testing.T) {
	detail := "401 unauthorized: Authorization=Bearer synthetic-test-credential"
	if got := conversationFailureHint(detail); got != "供应商认证失败，请检查 API Key、协议和供应商配置" {
		t.Fatalf("控制任务未返回固定处理建议：%q", got)
	}
}
