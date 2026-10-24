package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/session"
)

func TestValidateProject(t *testing.T) {
	home := t.TempDir()
	home, _ = filepath.EvalSymlinks(home)
	t.Setenv("HOME", home)
	project := filepath.Join(home, "CageWorkspace", "demo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "elsewhere")
	os.MkdirAll(outside, 0o755)
	link := filepath.Join(home, "CageWorkspace", "link")
	os.Symlink(outside, link)
	if err := ValidateProject(project); err != nil {
		t.Fatalf("valid project rejected: %v", err)
	}
	for name, path := range map[string]string{
		"outside":   outside,
		"workspace": filepath.Join(home, "CageWorkspace"),
		"symlink":   link,
		"relative":  "CageWorkspace/demo",
		"missing":   filepath.Join(home, "CageWorkspace", "nope"),
		"dotdot":    filepath.Join(home, "CageWorkspace", "demo", "..", "..", "elsewhere"),
	} {
		if ValidateProject(path) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestSandboxHelpers(t *testing.T) {
	s := Sandbox{ID: NewID(), Name: "demo", Bindings: []Binding{{Agent: session.DSH}, {Agent: session.Codex, ProviderID: "a"}}}
	if !strings.HasPrefix(s.ID, "sbx-") || len(s.ID) != 12 {
		t.Fatalf("id %q", s.ID)
	}
	if got := s.Agents(); len(got) != 2 || got[0] != session.Codex || got[1] != session.DSH {
		t.Fatalf("agents %v", got)
	}
	if s.ProviderFor(session.Codex) != "a" || s.ProviderFor(session.ClaudeCode) != "" {
		t.Fatal("provider lookup")
	}
	a, b := NewPlaceholder(), NewPlaceholder()
	if a == b || !strings.HasPrefix(a, "cage-") || len(a) < 30 {
		t.Fatalf("placeholders %q %q", a, b)
	}
	s.Bindings = append(s.Bindings, Binding{Agent: session.Codex})
	if s.Validate() == nil {
		t.Fatal("duplicate agent accepted")
	}
}

func TestAddLiveCountsDeltasPerRun(t *testing.T) {
	var s Sandbox
	s.AddLive("run-a", map[string]ProviderUsage{"p": {Requests: 2, InputTokens: 10, OutputTokens: 5}}, 2, 15)
	s.AddLive("run-a", map[string]ProviderUsage{"p": {Requests: 3, InputTokens: 30, OutputTokens: 6}}, 3, 36)
	s.AddLive("run-a", map[string]ProviderUsage{"p": {Requests: 3, InputTokens: 30, OutputTokens: 6}}, 3, 36) // repeated poll
	if got := s.ProviderUsage["p"]; got != (ProviderUsage{3, 30, 6}) {
		t.Fatalf("run-a total %+v", got)
	}
	s.AddLive("run-b", map[string]ProviderUsage{"p": {Requests: 1, InputTokens: 4, OutputTokens: 1}, "q": {Requests: 1}}, 4, 41)
	if got := s.ProviderUsage["p"]; got != (ProviderUsage{4, 34, 7}) {
		t.Fatalf("after restart %+v", got)
	}
	if s.ProviderUsage["q"].Requests != 1 || s.Usage.Requests != 4 || s.Usage.Tokens != 41 {
		t.Fatalf("totals %+v %+v", s.ProviderUsage, s.Usage)
	}
}
