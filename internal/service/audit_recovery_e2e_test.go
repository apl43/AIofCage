package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/store"
)

// 仅创建一次性空白沙箱，不调用模型，不启停现有沙箱或虚拟机。
func TestAuditRecoveryEndToEnd(t *testing.T) {
	if os.Getenv("AIOFCAGE_AUDIT_E2E") != "1" {
		t.Skip("设置 AIOFCAGE_AUDIT_E2E=1 验证真实门卫的补发与跨运行缺口")
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := runtime.NewManager(st, memoryVault{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := os.MkdirTemp(sandbox.Workspace(), "review-audit-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(project) })
	s, err := manager.Create(context.Background(), runtime.CreateRequest{Name: "review-audit-recovery", ProjectPath: project})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := manager.Delete(ctx, s.ID, false); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := manager.Start(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, memoryVault{}, manager, nil, nil)
	interrupted := make(chan struct{}, 4)
	app.SetEmitter(func(name string, data any) {
		if name == "sandbox:alert" && strings.Contains(data.(map[string]string)["message"], "中断") {
			select {
			case interrupted <- struct{}{}:
			default:
			}
		}
	})
	// 和桌面应用一样由监视循环订阅：门卫停了跟随自己结束，再启动时重新订阅。
	app.StartMonitor(ctx)
	probe := func(label string) {
		t.Helper()
		var out bytes.Buffer
		// 未授权的保留域名由门卫本地拒绝，不向公网发送请求。
		err := manager.Exec(ctx, s.ID, false, nil, &out, io.Discard, "curl", "--silent", "--max-time", "5", "--output", "/dev/null", "--write-out", "%{http_code}", "http://blocked.demo.invalid/"+label)
		if err != nil || out.String() != "403" {
			t.Fatalf("未观察到本地拒绝：status=%q err=%v", out.String(), err)
		}
	}
	waitFor := func(description string, condition func([]audit.Event) bool) {
		t.Helper()
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			tail, err := app.loadTail(s.ID, 500)
			if err == nil && condition(tail) {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-deadline.C:
				t.Fatalf("等待%s超时：%+v %v", description, tail, err)
			case <-ticker.C:
			}
		}
	}
	denials := func(want int) func([]audit.Event) bool {
		return func(tail []audit.Event) bool {
			seen := map[string]bool{}
			for _, event := range tail {
				// 明文 HTTP 在读取目标元数据前就被拒绝，事件不包含 host/path。
				if event.Action == audit.ActionRequest && event.ReasonCode == "plain_http_denied" && event.Decision == audit.DecisionDeny {
					seen[event.ID] = true
				}
			}
			return len(seen) == want
		}
	}
	probe("before-failure")
	waitFor("首次请求落盘", denials(1))
	path := app.auditPath(s.ID)
	backup := filepath.Join(filepath.Dir(path), "saved.jsonl")
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	probe("during-failure-1")
	select {
	case <-interrupted:
	case <-time.After(15 * time.Second):
		t.Fatal("写盘失败后未提示采集中断")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	probe("during-failure-2")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	waitFor("两条未保存事件补发", denials(3))
	t.Log("真实写盘失败后，原游标补齐两条被拒请求；没有调用模型")
	quiet := func(description string) {
		t.Helper()
		select {
		case <-interrupted:
			t.Fatalf("%s时提示了采集中断", description)
		default:
		}
	}
	for len(interrupted) > 0 {
		<-interrupted
	}
	crossRunGaps := func() int {
		tail, err := app.loadTail(s.ID, 500)
		if err != nil {
			t.Fatal(err)
		}
		gaps := 0
		for _, event := range tail {
			if event.Action == audit.ActionGap && event.ReasonCode == "cursor_from_other_run" {
				gaps++
			}
		}
		return gaps
	}
	// 正常停止后马上启动（更新智能体、重置后恢复运行都是这样）：停止前采集完这一轮，重启不记缺口，也不提示中断。
	if err := manager.Stop(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if app.collectedCursor(s.ID) == "" {
		t.Fatal("停止前没有确认这一轮已采集完")
	}
	if err := manager.Start(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	probe("after-quick-restart")
	waitFor("马上重启后继续记录", denials(4))
	quiet("马上重启")
	// 正常停止，等跟随结束后再启动：重新订阅，同样不记缺口。
	if err := manager.Stop(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(8 * time.Second)
	if err := manager.Start(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	probe("after-restart")
	waitFor("停一会儿再启动后继续记录", denials(5))
	quiet("停止和再启动")
	if gaps := crossRunGaps(); gaps != 0 {
		t.Fatalf("正常停止后重启仍记了 %d 处跨运行缺口", gaps)
	}
	// 门卫停止前应用没有收尾（比如在应用外被停掉）：可能有事件没采到，重启后照常记缺口。
	manager.BeforeGateStop = nil
	if err := manager.Stop(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	manager.BeforeGateStop = app.finishAudit
	if err := manager.Start(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	probe("after-unfinished-stop")
	waitFor("没收尾的停止后继续记录", denials(6))
	report, err := app.QueryUsage(UsageQuery{SandboxID: s.ID})
	if err != nil || report.Gaps != 1 || report.Requests != 0 || crossRunGaps() != 1 {
		t.Fatalf("缺口统计错误：%+v %v", report, err)
	}
	t.Log("一次性沙箱：两次正常停止后重启都没有缺口、没有提示中断；没收尾的停止后记了 1 处缺口；模型请求数为 0")
}
