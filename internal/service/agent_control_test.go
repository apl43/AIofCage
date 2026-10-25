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
