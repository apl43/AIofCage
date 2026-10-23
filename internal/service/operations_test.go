package service

import (
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
)

func TestOperationsAreExclusiveAndVisible(t *testing.T) {
	app, _, st := newApp(t)
	st.SaveSandbox(sandbox.Sandbox{ID: "sbx-a", Name: "alpha", Bindings: []sandbox.Binding{{Agent: session.Codex}}})
	done, err := app.beginOperation("sbx-a", "启动")
	if err != nil {
		t.Fatal(err)
	}
	app.ops.setProgress("sbx-a", "正在构建镜像")
	if _, err := app.beginOperation("sbx-a", "启动"); err == nil || !strings.Contains(err.Error(), "正在启动") {
		t.Fatalf("a second start must be refused while the first runs: %v", err)
	}
	views, _ := app.ListSandboxes()
	if views[0].Operation != "启动" || views[0].Progress != "正在构建镜像" {
		t.Fatalf("operation must be visible to every page: %+v", views[0])
	}
	done()
	if views, _ = app.ListSandboxes(); views[0].Operation != "" || views[0].Progress != "" {
		t.Fatalf("finished operation still shown: %+v", views[0])
	}
}

func TestProviderUsageSurvivesSandboxDeletion(t *testing.T) {
	app, _, st := newApp(t)
	usage := map[string]sandbox.ProviderUsage{"ds": {Requests: 3, InputTokens: 100, OutputTokens: 10}}
	st.SaveSandbox(sandbox.Sandbox{ID: "sbx-a", Name: "alpha", ProviderUsage: usage})
	st.SaveSandbox(sandbox.Sandbox{ID: "sbx-b", Name: "beta", ProviderUsage: usage})
	sbx, _ := st.Sandbox("sbx-a")
	if err := app.retireUsage(sbx); err != nil {
		t.Fatal(err)
	}
	st.DeleteSandbox("sbx-a")
	summary, _ := app.UsageSummary()
	if got := summary.Providers["ds"]; got.Requests != 6 || got.InputTokens != 200 || got.OutputTokens != 20 {
		t.Fatalf("deleted sandbox usage lost: %+v", got)
	}
	app.forgetRetiredUsage("ds")
	if summary, _ = app.UsageSummary(); summary.Providers["ds"].Requests != 3 {
		t.Fatalf("retired usage of a deleted provider must go: %+v", summary.Providers["ds"])
	}
}
