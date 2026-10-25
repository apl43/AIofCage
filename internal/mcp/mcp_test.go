package mcp

import (
	"testing"

	"github.com/shaoxier/AIofCage/internal/policy"
)

func TestGitHubServiceRules(t *testing.T) {
	s := Service{ID: "github", Name: "GitHub", URL: "https://api.githubcopilot.com/mcp/", KeyRef: KeyRefFor("github")}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot := policy.Snapshot{Version: 1, Rules: s.Rules()}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, r := range []policy.Request{{Host: "api.githubcopilot.com", Method: "POST", Path: "/mcp/"},
		{Host: "api.githubcopilot.com", Method: "GET", Path: "/mcp"}, {Host: "api.githubcopilot.com", Method: "DELETE", Path: "/mcp/"}} {
		if d := snapshot.Evaluate(r); !d.Allowed || d.SecretRef != "mcp-github" {
			t.Errorf("should allow %+v: %+v", r, d)
		}
	}
	for _, r := range []policy.Request{{Host: "api.githubcopilot.com", Method: "POST", Path: "/other"},
		{Host: "api.githubcopilot.com", Method: "PUT", Path: "/mcp/"}, {Host: "api.github.com", Method: "POST", Path: "/mcp/"}} {
		if d := snapshot.Evaluate(r); d.Allowed {
			t.Errorf("should deny %+v", r)
		}
	}
	if EnvVar("my-svc") != "AIOFCAGE_MCP_MY_SVC_TOKEN" {
		t.Fatal(EnvVar("my-svc"))
	}
	for _, bad := range []Service{{ID: "Bad", Name: "x", URL: s.URL, KeyRef: "k"}, {ID: "x", Name: "x", URL: "http://a.example/mcp", KeyRef: "k"},
		{ID: "x", Name: "x", URL: "https://1.2.3.4/mcp", KeyRef: "k"}, {ID: "x", Name: "", URL: s.URL, KeyRef: "k"}} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if len(Presets()) == 0 {
		t.Fatal("no presets")
	}
}

func TestDatabaseURL(t *testing.T) {
	clean, password, err := SplitDatabaseURL(KindPostgres, " postgresql://app:p%40ss@DB.Example.com:6543/shop?sslmode=require ")
	if err != nil || clean != "postgresql://app@db.example.com:6543/shop?sslmode=require" || password != "p@ss" {
		t.Fatalf("%q %q %v", clean, password, err)
	}
	if clean, password, err := SplitDatabaseURL(KindPostgres, "postgres://app@localhost/shop"); err != nil || clean != "postgres://app@localhost/shop" || password != "" {
		t.Fatalf("%q %q %v", clean, password, err)
	}
	for _, bad := range []string{"mysql://a@h/db", "postgresql://h/db", "postgresql://a@h/", "postgresql://a@h:99999/db",
		"postgresql://a@h/db?host=/tmp", "postgresql://a@h/db?sslrootcert=/etc/x", "postgresql://a@h/db?sslmode=maybe",
		"postgresql://a@bad_host/db", "postgresql://a@h/db/x", "postgresql://a@h/db#x"} {
		if _, _, err := SplitDatabaseURL(KindPostgres, bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestConnectorService(t *testing.T) {
	s := Service{ID: "shop-db", Name: "商城数据库", Kind: KindPostgres, URL: "postgresql://app@localhost/shop?sslmode=disable", KeyRef: KeyRefFor("shop-db")}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Rules() != nil || s.AgentURL() != "http://gatekeeper:8890/mcp/shop-db" || len(s.Tools()) != 1 || s.Tools()[0] != "query" {
		t.Fatalf("rules %v url %s tools %v", s.Rules(), s.AgentURL(), s.Tools())
	}
	command, redact, err := s.Launch("p@ss word")
	if err != nil || len(command) != 2 || command[0] != "/opt/aiofcage-postgres/server.mjs" ||
		command[1] != "postgresql://app:p%40ss%20word@host.lima.internal:5432/shop?sslmode=disable" {
		t.Fatalf("%v %v", command, err)
	}
	if len(redact) != 2 || redact[0] != "p@ss word" || redact[1] != "p%40ss%20word" {
		t.Fatalf("redact %v", redact)
	}
	withPassword := s
	withPassword.URL = "postgresql://app:secret@localhost/shop"
	if withPassword.Validate() == nil {
		t.Fatal("a stored URL must never contain the password")
	}
	unknown := s
	unknown.Kind = "redis"
	if unknown.Validate() == nil {
		t.Fatal("unknown kind accepted")
	}
	http := Service{ID: "gh", Name: "GitHub", URL: "https://api.githubcopilot.com/mcp/", KeyRef: "k"}
	if http.AgentURL() != http.URL || http.Tools() != nil {
		t.Fatal("HTTP services keep their URL and all tools")
	}
}

func TestOAuthService(t *testing.T) {
	s := Service{ID: "notion", Name: "Notion", Kind: KindOAuth, URL: "https://mcp.notion.com/mcp", KeyRef: KeyRefFor("notion")}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.IsConnector() || !s.IsOAuth() || s.AgentURL() != s.URL || len(s.Rules()) != 1 || s.Rules()[0].SecretRef != "mcp-notion" {
		t.Fatalf("oauth services are remote HTTP services: %+v", s.Rules())
	}
	s.URL = "http://mcp.notion.com/mcp"
	if s.Validate() == nil {
		t.Fatal("OAuth services must use https")
	}
}

func TestValidateTools(t *testing.T) {
	postgres := Service{ID: "db", Name: "数据库", Kind: KindPostgres, URL: "postgresql://app@localhost/shop", KeyRef: KeyRefFor("db")}
	for _, tc := range []struct {
		name  string
		tools []string
		want  bool
	}{
		{name: "catalog tool", tools: []string{"query"}, want: true},
		{name: "explicit empty grant", tools: []string{}, want: true},
		{name: "unknown tool", tools: []string{"drop_table"}},
		{name: "duplicate tool", tools: []string{"query", "query"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTools(postgres, tc.tools); (err == nil) != tc.want {
				t.Fatalf("ValidateTools(%v) error = %v, want valid = %v", tc.tools, err, tc.want)
			}
		})
	}
	httpService := Service{ID: "github", Name: "GitHub", Kind: KindHTTP, URL: "https://api.githubcopilot.com/mcp/", KeyRef: KeyRefFor("github")}
	if err := ValidateTools(httpService, []string{"query"}); err == nil {
		t.Fatal("remote HTTP service must reject connector tool grants")
	}
	if err := ValidateTools(httpService, nil); err != nil {
		t.Fatalf("empty remote grant should be accepted for compatibility: %v", err)
	}
}
