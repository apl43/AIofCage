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
