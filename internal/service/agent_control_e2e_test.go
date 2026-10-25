package service

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
	"github.com/shaoxier/AIofCage/internal/vault"
)

func TestAgentControlEndToEnd(t *testing.T) {
	if os.Getenv("AIOFCAGE_CONTROL_E2E") != "1" {
		t.Skip("设置 AIOFCAGE_CONTROL_E2E=1 验证真实批量模型任务与取消")
	}
	dir, _ := store.DefaultDir()
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
	providerID := ""
	for _, p := range providers {
		if p.Agent == session.DSH {
			if err := st.SaveProvider(p); err != nil {
				t.Fatal(err)
			}
			providerID = p.ID
			break
		}
	}
	if providerID == "" {
		t.Fatal("没有 DeepSeek 供应商")
	}
	keys := vault.NewKeychain()
	m, err := runtime.NewManager(st, keys)
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp(st, keys, m, nil, nil)
	c := newAgentController(context.Background(), a)
	type fixture struct{ ID, Path string }
	fixtures := []fixture{}
	t.Cleanup(func() {
		c.Close()
		for _, f := range fixtures {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			if err := m.Delete(ctx, f.ID, false); err != nil {
				t.Error(err)
			}
			cancel()
			_ = os.RemoveAll(f.Path)
		}
	})
	call := func(tool string, args any) any {
		t.Helper()
		raw, _ := json.Marshal(args)
		value, err := c.Call(context.Background(), tool, raw)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		return value
	}
	jobs := []string{}
	for _, label := range []string{"a", "b"} {
		created := call("create_sandbox", map[string]any{"name": "review-control-" + label, "provider_id": providerID, "max_requests": 10, "max_tokens": 30000}).(map[string]any)
		id := created["sandbox_id"].(string)
		fixtures = append(fixtures, fixture{id, created["project_path"].(string)})
		job := call("run_task", map[string]any{"sandbox_id": id, "text": "不要调用工具。只回复 control-batch-" + label, "timeout_seconds": 120}).(map[string]string)
		jobs = append(jobs, job["task_id"])
	}
	if _, err := c.Call(context.Background(), "stop_sandbox", json.RawMessage(`{"sandbox_id":"not-owned"}`)); err == nil {
		t.Fatal("控制范围越界")
	}
	deadline := time.Now().Add(150 * time.Second)
	for i, job := range jobs {
		for {
			snapshot := call("get_task", map[string]string{"task_id": job}).(controlTask)
			if snapshot.State == "failed" || snapshot.State == "cancelled" {
				t.Fatal("批量任务失败", snapshot.Error)
			}
			if snapshot.State == "done" {
				if !strings.Contains(snapshot.Text, "control-batch-"+string(rune('a'+i))) {
					t.Fatal("批量回复不符")
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("批量任务超时")
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	t.Log("两个独立沙箱的真实并发任务与结果查询通过")
	job := call("run_task", map[string]any{"sandbox_id": fixtures[0].ID, "text": "请调用 Bash 或 bash 工具运行 sleep 30，等待结束后只回复 done。不要做其他操作。", "timeout_seconds": 120}).(map[string]string)
	jobID := job["task_id"]
	deadline = time.Now().Add(60 * time.Second)
	for {
		snapshot := call("get_task", map[string]string{"task_id": jobID}).(controlTask)
		if len(snapshot.Tools) > 0 {
			break
		}
		if snapshot.State == "failed" || snapshot.State == "done" || time.Now().After(deadline) {
			t.Fatal("未能进入可验证取消的工具执行阶段", snapshot.State)
		}
		time.Sleep(100 * time.Millisecond)
	}
	call("cancel_task", map[string]string{"task_id": jobID})
	deadline = time.Now().Add(15 * time.Second)
	for {
		snapshot := call("get_task", map[string]string{"task_id": jobID}).(controlTask)
		if snapshot.State == "cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("取消未及时完成")
		}
		time.Sleep(100 * time.Millisecond)
	}
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := `import pathlib,sys
needle=('AIOFCAGE_TERMINAL_ID='+sys.argv[1]).encode()
found=0
for p in pathlib.Path('/proc').glob('[0-9]*/environ'):
 try:
  if needle in p.read_bytes().split(b'\0'): found+=1
 except (PermissionError,FileNotFoundError,ProcessLookupError): pass
assert found==0, 'task processes remain'
print('cancelled task processes cleaned')`
	if err := m.Exec(ctx, fixtures[0].ID, false, nil, &out, &out, "python3", "-c", script, "t-control-"+jobID); err != nil {
		t.Fatalf("取消后进程清理失败: %v %s", err, out.String())
	}
	c.Close()
	for _, f := range fixtures {
		if m.Status(context.Background(), f.ID).State != "stopped" {
			t.Fatal("控制会话退出后沙箱未停止")
		}
		s, _ := st.Sandbox(f.ID)
		if s.Usage.Requests == 0 {
			t.Fatal("用量未保留")
		}
	}
	t.Log("工具执行取消、容器内进程清理、正常断开后停止沙箱与保留用量通过")
}
