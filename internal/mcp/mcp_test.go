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
