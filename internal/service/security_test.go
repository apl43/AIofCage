package service

import (
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/audit"
	"github.com/shaoxier/AIofCage/internal/mcp"
	"github.com/shaoxier/AIofCage/internal/sandbox"
)

func eventForSecurityTest() audit.Event {
	return audit.Event{ID: "run:1", Time: time.Now(), Decision: audit.DecisionDeny, Action: audit.ActionConnect, ReasonCode: "default_deny"}
}

func TestSecurityDiagnosticsShowsMCPGrantAndRecentDenials(t *testing.T) {
	app, secrets, st := newApp(t)
	service := mcp.Service{ID: "shop", Name: "Shop", Kind: mcp.KindPostgres, URL: "postgresql://app@localhost/shop", KeyRef: mcp.KeyRefFor("shop")}
	if err := st.SaveMCPService(service); err != nil {
		t.Fatal(err)
	}
	secrets[service.KeyRef] = []byte("database-password")
	if err := st.SaveSandbox(sandbox.Sandbox{ID: "sbx-security", Name: "安全检查", MCP: []string{"shop"},
		MCPPermissions: map[string][]string{"shop": {}}}); err != nil {
		t.Fatal(err)
	}
	app.record(sandbox.Sandbox{ID: "sbx-security", Name: "安全检查"}, eventForSecurityTest())
	view, err := app.SecurityDiagnostics("sbx-security")
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "unavailable" || view.RecentDenied != 1 || len(view.Controls) != 4 || len(view.MCP) != 1 {
		t.Fatalf("unexpected security summary: %+v", view)
	}
	item := view.MCP[0]
	if !item.Connector || !item.ExplicitGrant || len(item.GrantedTools) != 0 || !item.KeySaved || len(item.CatalogTools) != 1 {
		t.Fatalf("unexpected MCP security view: %+v", item)
	}
}
