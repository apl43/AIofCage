package provider

import (
	"testing"

	"github.com/shaoxier/AIofCage/internal/policy"
	"github.com/shaoxier/AIofCage/internal/session"
)

func xapi() Provider {
	p := Provider{ID: "xapi", Name: "七牛 xapi", Agent: session.Codex, Protocol: OpenAIResponses,
		BaseURL: "https://xapi.qiniu.io/v1/", Model: "gpt-6.1-sol", KeyRef: "sbx-builderday-key"}
	p.Normalize()
	return p
}

func TestValidateAndEndpoint(t *testing.T) {
	p := xapi()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	host, base, err := p.Endpoint()
	if host != "xapi.qiniu.io" || base != "/v1" || err != nil {
		t.Fatalf("endpoint %q %q %v", host, base, err)
	}
	bad := map[string]func(*Provider){
		"http":          func(p *Provider) { p.BaseURL = "http://xapi.qiniu.io/v1" },
		"ip":            func(p *Provider) { p.BaseURL = "https://1.2.3.4/v1" },
		"port":          func(p *Provider) { p.BaseURL = "https://xapi.qiniu.io:8443/v1" },
		"query":         func(p *Provider) { p.BaseURL = "https://xapi.qiniu.io/v1?x=1" },
		"userinfo":      func(p *Provider) { p.BaseURL = "https://u:p@xapi.qiniu.io/v1" },
		"codex chat":    func(p *Provider) { p.Protocol = OpenAIChat },
		"claude openai": func(p *Provider) { p.Agent = session.ClaudeCode },
		"empty model":   func(p *Provider) { p.Model = "" },
		"bad keyref":    func(p *Provider) { p.KeyRef = "has space" },
		"bad auth":      func(p *Provider) { p.Auth = "basic" },
		"no name":       func(p *Provider) { p.Name = "" },
		"unknown agent": func(p *Provider) { p.Agent = "cursor" },
		"encoded path":  func(p *Provider) { p.BaseURL = "https://xapi.qiniu.io/v%31" },
	}
	for name, mutate := range bad {
		candidate := xapi()
		mutate(&candidate)
		candidate.Normalize()
		if candidate.Validate() == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestRulesAreValidAndMinimal(t *testing.T) {
	cases := []struct {
		provider Provider
		allowed  []policy.Request
		denied   []policy.Request
	}{
		{xapi(),
			[]policy.Request{{Host: "xapi.qiniu.io", Method: "POST", Path: "/v1/responses"}, {Host: "xapi.qiniu.io", Method: "POST", Path: "/v1/responses/compact"}, {Host: "xapi.qiniu.io", Method: "GET", Path: "/v1/models"}},
			[]policy.Request{{Host: "xapi.qiniu.io", Method: "POST", Path: "/v1/files"}, {Host: "xapi.qiniu.io", Method: "GET", Path: "/v1/responses"}, {Host: "xapi.qiniu.io", Method: "POST", Path: "/v1/chat/completions"}}},
		{Provider{ID: "ds", Name: "DeepSeek", Agent: session.ClaudeCode, Protocol: Anthropic, BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-chat", KeyRef: "k"},
			[]policy.Request{{Host: "api.deepseek.com", Method: "POST", Path: "/anthropic/v1/messages"}, {Host: "api.deepseek.com", Method: "POST", Path: "/anthropic/v1/messages/count_tokens"}},
			[]policy.Request{{Host: "api.deepseek.com", Method: "POST", Path: "/v1/messages"}, {Host: "api.deepseek.com", Method: "POST", Path: "/anthropic/v1/files"}}},
	}
	for _, c := range cases {
		c.provider.Normalize()
		snapshot := policy.Snapshot{Version: 1, Rules: c.provider.Rules()}
		if err := snapshot.Validate(); err != nil {
			t.Fatalf("%s rules invalid: %v", c.provider.ID, err)
		}
		for _, r := range c.allowed {
			if d := snapshot.Evaluate(r); !d.Allowed || d.SecretRef != c.provider.ID {
				t.Errorf("%s should allow %+v: %+v", c.provider.ID, r, d)
			}
		}
		for _, r := range c.denied {
			if d := snapshot.Evaluate(r); d.Allowed {
				t.Errorf("%s should deny %+v", c.provider.ID, r)
			}
		}
	}
}

func TestInjection(t *testing.T) {
	p := xapi()
	if h, prefix := p.Injection(); h != "Authorization" || prefix != "Bearer " {
		t.Fatal(h, prefix)
	}
	p.Auth = AuthXAPIKey
	if h, prefix := p.Injection(); h != "X-Api-Key" || prefix != "" {
		t.Fatal(h, prefix)
	}
}
