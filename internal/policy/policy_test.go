package policy

import "testing"

func sample() Snapshot {
	return Snapshot{Version: 1, SessionID: "sbx-1", Rules: []Rule{
		{ID: "model", Effect: Allow, Host: "xapi.qiniu.io", Methods: []string{"POST"}, Path: "/v1/responses", SecretRef: "provider-a"},
		{ID: "npm", Effect: Allow, Host: "registry.npmjs.org", Methods: []string{"GET", "HEAD"}, Path: "/*"},
		{ID: "files", Effect: Deny, Host: "xapi.qiniu.io", Path: "/v1/files/*"},
		{ID: "gh-raw", Effect: Deny, Host: "*.githubusercontent.com"},
		{ID: "gh-read", Effect: Allow, Host: "api.github.com", Methods: []string{"GET"}, Path: "/repos/*"},
	}}
}

func TestValidateAcceptsSample(t *testing.T) {
	s := sample()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]Rule{
		"ip host":             {ID: "r", Effect: Allow, Host: "1.2.3.4", Methods: []string{"GET"}},
		"uppercase host":      {ID: "r", Effect: Allow, Host: "Example.com", Methods: []string{"GET"}},
		"single label":        {ID: "r", Effect: Allow, Host: "localhost", Methods: []string{"GET"}},
		"wildcard allow":      {ID: "r", Effect: Allow, Host: "*.example.com", Methods: []string{"GET"}},
		"allow without verbs": {ID: "r", Effect: Allow, Host: "example.com"},
		"unknown verb":        {ID: "r", Effect: Allow, Host: "example.com", Methods: []string{"TRACE"}},
		"non-443 port":        {ID: "r", Effect: Allow, Host: "example.com", Port: 8443, Methods: []string{"GET"}},
		"dirty path":          {ID: "r", Effect: Allow, Host: "example.com", Methods: []string{"GET"}, Path: "/a/../b"},
		"encoded path":        {ID: "r", Effect: Allow, Host: "example.com", Methods: []string{"GET"}, Path: "/a%2fb"},
		"inner wildcard":      {ID: "r", Effect: Allow, Host: "example.com", Methods: []string{"GET"}, Path: "/a/*/b"},
		"secret on deny":      {ID: "r", Effect: Deny, Host: "example.com", SecretRef: "s"},
		"bad effect":          {ID: "r", Effect: "maybe", Host: "example.com"},
		"bad id":              {ID: "has space", Effect: Deny, Host: "example.com"},
	}
	for name, rule := range cases {
		s := Snapshot{Version: 1, Rules: []Rule{rule}}
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	duplicate := Snapshot{Version: 1, Rules: []Rule{
		{ID: "r", Effect: Deny, Host: "a.example"}, {ID: "r", Effect: Deny, Host: "b.example"}}}
	if duplicate.Validate() == nil {
		t.Error("duplicate IDs must be rejected")
	}
	if (&Snapshot{}).Validate() == nil {
		t.Error("version 0 must be rejected")
	}
}

func TestEvaluate(t *testing.T) {
	s := sample()
	cases := []struct {
		name    string
		request Request
		allowed bool
		reason  string
		rule    string
		secret  string
	}{
		{"model call", Request{"xapi.qiniu.io", 443, "POST", "/v1/responses"}, true, "allowed", "model", "provider-a"},
		{"wrong method", Request{"xapi.qiniu.io", 443, "GET", "/v1/responses"}, false, "default_deny", "", ""},
		{"other path", Request{"xapi.qiniu.io", 443, "POST", "/v1/chat/completions"}, false, "default_deny", "", ""},
		{"deny wins", Request{"xapi.qiniu.io", 443, "POST", "/v1/files/abc"}, false, "rule_denied", "files", ""},
		{"deny prefix root", Request{"xapi.qiniu.io", 443, "GET", "/v1/files"}, false, "rule_denied", "files", ""},
		{"prefix allow", Request{"registry.npmjs.org", 0, "GET", "/@openai/codex"}, true, "allowed", "npm", ""},
		{"prefix method", Request{"registry.npmjs.org", 443, "PUT", "/pkg"}, false, "default_deny", "", ""},
		{"case and dot", Request{"API.GITHUB.COM.", 443, "GET", "/repos/a/b"}, true, "allowed", "gh-read", ""},
		{"prefix boundary", Request{"api.github.com", 443, "GET", "/reposx"}, false, "default_deny", "", ""},
		{"unknown host", Request{"example.com", 443, "GET", "/"}, false, "default_deny", "", ""},
		{"other port", Request{"xapi.qiniu.io", 8443, "POST", "/v1/responses"}, false, "default_deny", "", ""},
	}
	for _, c := range cases {
		d := s.Evaluate(c.request)
		if d.Allowed != c.allowed || d.ReasonCode != c.reason || d.RuleID != c.rule || d.SecretRef != c.secret {
			t.Errorf("%s: got %+v", c.name, d)
		}
	}
}

func TestAllowsConnect(t *testing.T) {
	s := sample()
	if d := s.AllowsConnect("xapi.qiniu.io", 443); !d.Allowed {
		t.Errorf("model host tunnel: %+v", d)
	}
	if d := s.AllowsConnect("raw.githubusercontent.com", 443); d.Allowed || d.RuleID != "gh-raw" {
		t.Errorf("host-wide deny must block tunnel: %+v", d)
	}
	if d := s.AllowsConnect("example.com", 443); d.Allowed {
		t.Errorf("unknown host tunnel: %+v", d)
	}
	if d := s.AllowsConnect("xapi.qiniu.io", 80); d.Allowed {
		t.Errorf("non-443 tunnel: %+v", d)
	}
}

func TestSecretHosts(t *testing.T) {
	s := sample()
	hosts := s.SecretHosts()
	if len(hosts) != 1 || len(hosts["xapi.qiniu.io"]) != 1 || hosts["xapi.qiniu.io"][0] != "provider-a" {
		t.Errorf("unexpected secret hosts: %v", hosts)
	}
}
