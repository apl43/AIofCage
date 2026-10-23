package service

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/sandbox"
)

func TestAuditLogAndRecentEvents(t *testing.T) {
	app, _, st := newApp(t)
	var emitted []string
	app.emit = func(event string, _ any) { emitted = append(emitted, event) }
	a := sandbox.Sandbox{ID: "sbx-a", Name: "alpha"}
	b := sandbox.Sandbox{ID: "sbx-b", Name: "beta"}
	st.SaveSandbox(a)
	st.SaveSandbox(b)
	base := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	app.record(a, audit.Event{ID: "run-1:1", Time: base, Action: audit.ActionRequest, Decision: audit.DecisionInject, Host: "xapi.qiniu.io"})
	app.record(b, audit.Event{ID: "run-2:1", Time: base.Add(time.Second), Action: audit.ActionRequest, Decision: audit.DecisionDeny, ReasonCode: "default_deny"})
	app.record(a, audit.Event{ID: "run-1:2", Time: base.Add(2 * time.Second), Action: audit.ActionGap, ReasonCode: "buffer_overflow"})
	if len(emitted) != 3 || emitted[0] != "sandbox:event" {
		t.Fatalf("emitted %v", emitted)
	}
	all, err := app.RecentEvents("")
	if err != nil || len(all) != 3 || all[0].SandboxName != "alpha" || all[1].SandboxName != "beta" {
		t.Fatalf("recent %+v %v", all, err)
	}
	if only, _ := app.RecentEvents("sbx-b"); len(only) != 1 || only[0].ReasonCode != "default_deny" {
		t.Fatalf("filtered %+v", only)
	}
	// The JSONL file is the durable record; a fresh app reads it back and resumes after the last
	// non-gap event.
	data, err := os.ReadFile(app.auditPath("sbx-a"))
	if err != nil || strings.Count(string(data), "\n") != 2 {
		t.Fatalf("audit file: %q %v", data, err)
	}
	if info, _ := os.Stat(app.auditPath("sbx-a")); info.Mode().Perm() != 0o600 {
		t.Fatalf("audit file mode %v", info.Mode())
	}
	fresh := NewApp(st, memoryVault{}, nil, nil, nil)
	if cursor := fresh.cursor("sbx-a"); cursor != "run-1:1" {
		t.Fatalf("cursor %q", cursor)
	}
	if events, _ := fresh.RecentEvents("sbx-a"); len(events) != 2 {
		t.Fatalf("reloaded %d events", len(events))
	}
}

func TestUsageSummaryAggregatesProviders(t *testing.T) {
	app, _, st := newApp(t)
	st.SaveSandbox(sandbox.Sandbox{ID: "sbx-a", Name: "alpha", Usage: sandbox.Usage{Requests: 3, Tokens: 30},
		Limits: sandbox.Limits{MaxRequests: 10}, ProviderUsage: map[string]sandbox.ProviderUsage{"xapi": {Requests: 3, InputTokens: 20, OutputTokens: 10}}})
	st.SaveSandbox(sandbox.Sandbox{ID: "sbx-b", Name: "beta",
		ProviderUsage: map[string]sandbox.ProviderUsage{"xapi": {Requests: 1, InputTokens: 5, OutputTokens: 1}, "ds": {Requests: 2}}})
	summary, err := app.UsageSummary()
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Sandboxes) != 2 || summary.Sandboxes[0].MaxRequests != 10 || summary.Sandboxes[0].Live {
		t.Fatalf("sandboxes %+v", summary.Sandboxes)
	}
	x := summary.Providers["xapi"]
	if x.Requests != 4 || x.InputTokens != 25 || x.OutputTokens != 11 || len(x.Sandboxes) != 2 || summary.Providers["ds"].Requests != 2 {
		t.Fatalf("providers %+v", summary.Providers)
	}
}
