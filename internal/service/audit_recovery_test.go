package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/events"
	"github.com/shaoxier/AIofCage/internal/sandbox"
)

type auditStreamFunc func(context.Context, string, string, events.Handler) error

func (f auditStreamFunc) Events(ctx context.Context, id, cursor string, handle events.Handler) error {
	return f(ctx, id, cursor, handle)
}

func TestAuditRetriesFromDurableCursorAfterWriteFailure(t *testing.T) {
	a, _, st := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-recovery", Name: "恢复验证"}
	if err := st.SaveSandbox(s); err != nil {
		t.Fatal(err)
	}
	newEvent := func(id string) audit.Event {
		return audit.Event{ID: id, Time: time.Now(), SessionID: s.ID, Action: audit.ActionRequest, ModelRequest: true, UsageKind: "reported"}
	}
	if err := a.record(s, newEvent("run:1")); err != nil {
		t.Fatal(err)
	}
	// 新进程必须从磁盘恢复游标，而不是依赖上次的内存状态。
	a = NewApp(st, memoryVault{}, nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var cursors, alerts []string
	a.emit = func(name string, data any) {
		if name == "sandbox:alert" {
			alert := data.(map[string]string)
			alerts = append(alerts, alert["message"])
			if strings.Contains(alert["message"], "恢复") && alert["level"] != "info" {
				t.Error("恢复消息不应显示为错误")
			}
		}
	}
	source := auditStreamFunc(func(_ context.Context, id, cursor string, handle events.Handler) error {
		if id != s.ID {
			t.Fatalf("错误沙箱 %q", id)
		}
		cursors = append(cursors, cursor)
		if len(cursors) == 1 {
			path := a.auditPath(s.ID)
			backup := path + ".saved"
			if err := os.Rename(path, backup); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			failed := handle(newEvent("run:2"))
			if failed == nil {
				t.Fatal("真实写盘错误未返回给事件源")
			}
			if current, err := a.cursor(s.ID); err != nil || current != "run:1" {
				t.Fatalf("写盘失败推进了游标：%q %v", current, err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(backup, path); err != nil {
				t.Fatal(err)
			}
			return failed
		}
		for _, id := range []string{"run:2", "run:3"} {
			if err := handle(newEvent(id)); err != nil {
				t.Fatal(err)
			}
		}
		cancel()
		return context.Canceled
	})
	a.follow(ctx, s, source)
	if !reflect.DeepEqual(cursors, []string{"run:1", "run:1"}) {
		t.Fatalf("未从原游标重试：%v", cursors)
	}
	if len(alerts) != 2 || !strings.Contains(alerts[0], "中断") || !strings.Contains(alerts[1], "恢复") {
		t.Fatalf("采集状态未提示：%v", alerts)
	}
	fresh := NewApp(st, memoryVault{}, nil, nil, nil)
	report, err := fresh.QueryUsage(UsageQuery{SandboxID: s.ID})
	if err != nil || report.Requests != 3 || report.Gaps != 0 {
		t.Fatalf("恢复后请求丢失：%+v %v", report, err)
	}
	if current, err := fresh.cursor(s.ID); err != nil || current != "run:3" {
		t.Fatalf("恢复后游标未持久化：%q %v", current, err)
	}
}

func TestAuditCrossRunGapDoesNotAcknowledgeLostEvents(t *testing.T) {
	a, _, _ := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-runs"}
	if err := a.record(s, audit.Event{ID: "previous:42", Action: audit.ActionRequest}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := auditStreamFunc(func(_ context.Context, _, cursor string, handle events.Handler) error {
		if cursor != "previous:42" {
			t.Fatalf("旧运行游标被清空：%q", cursor)
		}
		if err := handle(audit.Event{ID: "next:0", Action: audit.ActionGap, ReasonCode: "cursor_from_other_run"}); err != nil {
			t.Fatal(err)
		}
		if current, err := a.cursor(s.ID); err != nil || current != cursor {
			t.Fatalf("缺口被用作成功保存的事件游标：%q %v", current, err)
		}
		if err := handle(audit.Event{ID: "next:1", Action: audit.ActionPolicyChange}); err != nil {
			t.Fatal(err)
		}
		cancel()
		return context.Canceled
	})
	a.follow(ctx, s, source)
	if current, err := a.cursor(s.ID); err != nil || current != "next:1" {
		t.Fatalf("新运行未继续：%q %v", current, err)
	}
}

func TestUnreadableAuditDoesNotBecomeEmptyHistory(t *testing.T) {
	a, _, st := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-unreadable"}
	if err := st.SaveSandbox(s); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(a.auditPath(s.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.cursor(s.ID); err == nil {
		t.Fatal("读取失败被视为新日志")
	}
	if _, err := a.RecentEvents(s.ID); err == nil {
		t.Fatal("读取失败被视为空列表")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.emit = func(name string, _ any) {
		if name == "sandbox:alert" {
			cancel()
		}
	}
	a.follow(ctx, s, auditStreamFunc(func(context.Context, string, string, events.Handler) error {
		t.Fatal("无法读取游标时不应重新订阅")
		return nil
	}))
}

func TestAuditPreservesIncompleteLineWithoutLosingNextEvent(t *testing.T) {
	a, _, st := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-partial"}
	if err := st.SaveSandbox(s); err != nil {
		t.Fatal(err)
	}
	path := a.auditPath(s.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	fragment := `{"id":"interrupted`
	if err := os.WriteFile(path, []byte(fragment), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.record(s, audit.Event{ID: "run:1", Action: audit.ActionRequest, ModelRequest: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), fragment+"\n") {
		t.Fatalf("崩溃残行被覆盖：%q %v", data, err)
	}
	report, err := a.QueryUsage(UsageQuery{SandboxID: s.ID})
	if err != nil || report.Gaps != 1 || report.Requests != 1 {
		t.Fatalf("残行吞掉后续事件或未提示缺口：%+v %v", report, err)
	}
}

func TestAuditRejectsDifferentSandbox(t *testing.T) {
	a, _, _ := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-owned"}
	err := a.record(s, audit.Event{ID: "run:1", SessionID: "sbx-other", Action: audit.ActionRequest})
	if err == nil {
		t.Fatal("错误沙箱的事件进入当前账本")
	}
	if _, err := os.Stat(a.auditPath(s.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("错误事件已落盘：%v", err)
	}
}

// 门卫停止前事件已全部落盘：新一轮开头“游标来自另一轮”的缺口不记录，用量里没有缺口。
func TestAuditCollectedRunRestartsWithoutGap(t *testing.T) {
	a, _, _ := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-clean"}
	if err := a.record(s, audit.Event{ID: "old:5", Action: audit.ActionRequest}); err != nil {
		t.Fatal(err)
	}
	a.monitor.subs[s.ID] = &subscription{cancel: func() {}}
	a.gateCursor = func(context.Context, string) (string, uint64, error) { return "old", 5, nil }
	a.finishAudit(context.Background(), s.ID)
	if got := a.collectedCursor(s.ID); got != "old:5" {
		t.Fatalf("采集完后没有记下游标：%q", got)
	}
	// 应用重开后也要认得：游标和记号都从磁盘读。
	a = NewApp(a.store, memoryVault{}, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := auditStreamFunc(func(_ context.Context, _, cursor string, handle events.Handler) error {
		if cursor != "old:5" {
			t.Fatalf("没有用保存的游标续传：%q", cursor)
		}
		for _, event := range []audit.Event{{ID: "new:0", Action: audit.ActionGap, ReasonCode: "cursor_from_other_run"}, {ID: "new:1", Action: audit.ActionPolicyChange}} {
			if err := handle(event); err != nil {
				t.Fatal(err)
			}
		}
		cancel()
		return context.Canceled
	})
	a.follow(ctx, s, source)
	tail, err := a.loadTail(s.ID, 10)
	if err != nil || len(tail) != 2 || tail[1].ID != "new:1" {
		t.Fatalf("新一轮的事件不对：%+v %v", tail, err)
	}
	if report, err := a.QueryUsage(UsageQuery{SandboxID: s.ID}); err != nil || report.Gaps != 0 {
		t.Fatalf("采集完的上一轮仍记了缺口：%+v %v", report, err)
	}
}

// 停止前没等到最后一条事件（或没有在订阅），不记游标，缺口照常记录。
func TestAuditUncollectedRunKeepsGap(t *testing.T) {
	a, _, _ := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-behind"}
	if err := a.record(s, audit.Event{ID: "old:4", Action: audit.ActionRequest}); err != nil {
		t.Fatal(err)
	}
	a.gateCursor = func(context.Context, string) (string, uint64, error) { return "old", 5, nil }
	a.finishAudit(context.Background(), s.ID) // 没有在订阅：直接返回
	a.monitor.subs[s.ID] = &subscription{cancel: func() {}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	a.finishAudit(ctx, s.ID) // 门卫有第 5 条，跟随只写到第 4 条
	if got := a.collectedCursor(s.ID); got != "" {
		t.Fatalf("没采集完也记了游标：%q", got)
	}
	follow, stop := context.WithCancel(context.Background())
	defer stop()
	a.follow(follow, s, auditStreamFunc(func(_ context.Context, _, _ string, handle events.Handler) error {
		if err := handle(audit.Event{ID: "new:0", Action: audit.ActionGap, ReasonCode: "cursor_from_other_run"}); err != nil {
			t.Fatal(err)
		}
		stop()
		return context.Canceled
	}))
	if report, err := a.QueryUsage(UsageQuery{SandboxID: s.ID}); err != nil || report.Gaps != 1 {
		t.Fatalf("可能丢事件的上一轮没有记缺口：%+v %v", report, err)
	}
}

// 门卫停了，事件流结束：不提示采集中断，跟随直接结束；门卫还在运行时照常提示并重试。
func TestAuditFollowerEndsQuietlyWhenGateStops(t *testing.T) {
	for _, running := range []bool{false, true} {
		a, _, _ := newApp(t)
		s := sandbox.Sandbox{ID: "sbx-stopping"}
		var alerts []string
		a.emit = func(name string, data any) {
			if name == "sandbox:alert" {
				alerts = append(alerts, data.(map[string]string)["message"])
			}
		}
		a.gateRunning = func(context.Context, string) (bool, error) { return running, nil }
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		attempts := 0
		a.follow(ctx, s, auditStreamFunc(func(context.Context, string, string, events.Handler) error {
			attempts++
			if attempts == 2 {
				cancel()
			}
			return errors.New("stream closed")
		}))
		cancel()
		if !running && (attempts != 1 || len(alerts) != 0) {
			t.Fatalf("门卫停止后仍在重试或提示中断：重试 %d 次，提示 %v", attempts, alerts)
		}
		if running && (attempts != 2 || len(alerts) != 1 || !strings.Contains(alerts[0], "中断")) {
			t.Fatalf("门卫还在运行时没有提示中断并重试：重试 %d 次，提示 %v", attempts, alerts)
		}
	}
}

// 收尾后门卫马上又启动了新一轮（更新智能体、重置后恢复运行）：旧流结束是预期的，重连时不提示中断。
func TestAuditCollectedRunReconnectsWithoutAlert(t *testing.T) {
	a, _, _ := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-replace"}
	if err := a.record(s, audit.Event{ID: "old:3", Action: audit.ActionRequest}); err != nil {
		t.Fatal(err)
	}
	if err := a.markCollected(s.ID, "old:3"); err != nil {
		t.Fatal(err)
	}
	var alerts []string
	a.emit = func(name string, data any) {
		if name == "sandbox:alert" {
			alerts = append(alerts, data.(map[string]string)["message"])
		}
	}
	a.gateRunning = func(context.Context, string) (bool, error) { return true, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	attempts := 0
	a.follow(ctx, s, auditStreamFunc(func(_ context.Context, _, cursor string, handle events.Handler) error {
		attempts++
		if attempts == 1 {
			return errors.New("old gatekeeper stopped")
		}
		for _, event := range []audit.Event{{ID: "new:0", Action: audit.ActionGap, ReasonCode: "cursor_from_other_run"}, {ID: "new:1", Action: audit.ActionPolicyChange}} {
			if err := handle(event); err != nil {
				t.Fatal(err)
			}
		}
		cancel()
		return context.Canceled
	}))
	if attempts != 2 || len(alerts) != 0 {
		t.Fatalf("预期的重连提示了中断：重试 %d 次，提示 %v", attempts, alerts)
	}
	if report, err := a.QueryUsage(UsageQuery{SandboxID: s.ID}); err != nil || report.Gaps != 0 {
		t.Fatalf("新一轮记了缺口：%+v %v", report, err)
	}
}
