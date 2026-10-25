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
