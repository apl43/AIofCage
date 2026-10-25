package service

import (
	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"testing"
	"time"
)

func TestUsageQueryDeduplicatesAfterRestart(t *testing.T) {
	a, _, st := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-usage", Name: "用量测试"}
	_ = st.SaveSandbox(s)
	event := audit.Event{ID: "run:1", Time: time.Now(), Action: audit.ActionRequest, ModelRequest: true, SecretRef: "p", Model: "m", UsageKind: "reported", InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30}
	a.record(s, event)
	a.record(s, event)
	event.ID = "run:2"
	event.UsageKind = "unknown"
	event.InputTokens = 0
	event.OutputTokens = 0
	event.CacheReadTokens = 0
	a.record(s, event)
	fresh := NewApp(st, memoryVault{}, nil, nil, nil)
	r, err := fresh.QueryUsage(UsageQuery{Period: "all"})
	if err != nil || r.Requests != 2 || r.Unknown != 1 || len(r.Groups) != 1 || r.Groups[0].InputTokens != 100 || r.Groups[0].CacheReadTokens != 30 {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = fresh.QueryUsage(UsageQuery{ProviderID: "other", Period: "today"})
	if err != nil || r.Requests != 0 {
		t.Fatal("筛选错误")
	}
}

func TestNewLedgerDoesNotClaimLegacy(t *testing.T) {
	a, _, st := newApp(t)
	s := sandbox.Sandbox{ID: "sbx-new", Name: "新沙箱", Usage: sandbox.Usage{Requests: 1}}
	_ = st.SaveSandbox(s)
	a.record(s, audit.Event{ID: "new:1", Time: time.Now(), Action: audit.ActionRequest, Agent: "codex", ModelRequest: true, SecretRef: "p", UsageKind: "reported"})
	r, err := a.QueryUsage(UsageQuery{SandboxID: s.ID, Period: "all"})
	if err != nil || r.Legacy {
		t.Fatalf("新账本误报旧累计：%+v %v", r, err)
	}
	a.record(s, audit.Event{ID: "old:1", Time: time.Now(), Action: audit.ActionRequest, SecretRef: "p"})
	r, _ = a.QueryUsage(UsageQuery{SandboxID: s.ID, Period: "all"})
	if !r.Legacy {
		t.Fatal("旧请求未提示无法细分")
	}
}

func TestUsageQueryKeepsGapBesideRequest(t *testing.T) {
	for _, gapFirst := range []bool{false, true} {
		t.Run(map[bool]string{true: "gap-first", false: "request-first"}[gapFirst], func(t *testing.T) {
			a, _, st := newApp(t)
			s := sandbox.Sandbox{ID: "sbx-gap", Name: "缺口核对"}
			if err := st.SaveSandbox(s); err != nil {
				t.Fatal(err)
			}
			request := audit.Event{ID: "run:1", Time: time.Now(), Action: audit.ActionRequest, ModelRequest: true, SecretRef: "p", UsageKind: "reported", InputTokens: 10}
			// 缺口标记使用补发游标的 ID，可能与正常请求相同，不能互相去重。
			gap := audit.Event{ID: request.ID, Time: request.Time, Action: audit.ActionGap, ReasonCode: "buffer_overflow"}
			first, second := request, gap
			if gapFirst {
				first, second = gap, request
			}
			a.record(s, first)
			a.record(s, second)
			a.record(s, gap)
			report, err := a.QueryUsage(UsageQuery{SandboxID: s.ID})
			if err != nil || report.Gaps != 1 || report.Requests != 1 || len(report.Groups) != 1 || report.Groups[0].InputTokens != 10 {
				t.Fatalf("缺口与请求必须分别保留：%+v %v", report, err)
			}
		})
	}
}
