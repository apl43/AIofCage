package runtime

import (
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
)

func TestAgentFilesContainOnlyPlaceholders(t *testing.T) {
	codex := provider.Provider{ID: "xapi", Name: `七牛 "xapi"`, Agent: session.Codex, Protocol: provider.OpenAIResponses,
		BaseURL: "https://xapi.qiniu.io/v1", Model: "gpt-6.1-sol", Auth: provider.AuthBearer, KeyRef: "k"}
	claude := provider.Provider{ID: "ds", Name: "DeepSeek", Agent: session.ClaudeCode, Protocol: provider.Anthropic,
		BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-chat", Auth: provider.AuthXAPIKey, KeyRef: "k2"}
	sbx := sandbox.Sandbox{ID: "sbx-x", Name: "it's mine", ProjectPath: "/Users/u/CageWorkspace/p",
		Bindings:     []sandbox.Binding{{Agent: session.Codex, ProviderID: "xapi"}, {Agent: session.ClaudeCode, ProviderID: "ds"}, {Agent: session.DSH}},
		Placeholders: map[string]string{"xapi": "cage-AAAAAAAAAAAAAAAAAAAA", "ds": "cage-BBBBBBBBBBBBBBBBBBBB"}}
	files := agentFiles(sbx, map[string]provider.Provider{"xapi": codex, "ds": claude})
	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = f.Content
		if f.Mode != 0o600 {
			t.Errorf("%s mode %o", f.Path, f.Mode)
		}
	}
	env := byPath[".aiofcage/env"]
	for _, want := range []string{"export AIOFCAGE_CODEX_API_KEY='cage-AAAAAAAAAAAAAAAAAAAA'",
		"export ANTHROPIC_API_KEY='cage-BBBBBBBBBBBBBBBBBBBB'", "export ANTHROPIC_BASE_URL='https://api.deepseek.com/anthropic'",
		`export AIOFCAGE_SANDBOX='it'"'"'s mine'`} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %q:\n%s", want, env)
		}
	}
	config := byPath[".codex/config.toml"]
	for _, want := range []string{`model = "gpt-6.1-sol"`, `name = "七牛 \"xapi\""`, `wire_api = "responses"`,
		`env_key = "AIOFCAGE_CODEX_API_KEY"`, `[projects."/Users/u/CageWorkspace/p"]`} {
		if !strings.Contains(config, want) {
			t.Errorf("codex config missing %q:\n%s", want, config)
		}
	}
	if strings.Contains(config, "cage-") {
		t.Error("codex config must reference the placeholder through env_key only")
	}
}

func TestAgentImageContext(t *testing.T) {
	a, err := agentContext([]session.Agent{session.DSH, session.Codex})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := agentContext([]session.Agent{session.Codex, session.DSH})
	if a.tag != b.tag || !strings.HasPrefix(a.tag, "aiofcage/agent:") {
		t.Fatalf("tag must not depend on selection order: %s %s", a.tag, b.tag)
	}
	dockerfile := string(a.files["Dockerfile"])
	codex := strings.Index(dockerfile, "@openai/codex@0.164.0")
	dsh := strings.Index(dockerfile, "@deepseek-ai/dsh@")
	if codex < 0 || dsh < 0 || codex > dsh || strings.Contains(dockerfile, "claude-code") {
		t.Fatalf("unexpected Dockerfile:\n%s", dockerfile)
	}
	if !strings.Contains(dockerfile, "public.ecr.aws/docker/library/node:22.16.0-bookworm-slim@sha256:") {
		t.Fatal("base image must be pinned from public.ecr.aws")
	}
	if _, err := agentContext(nil); err == nil {
		t.Fatal("empty agent set accepted")
	}
}

func TestParseSize(t *testing.T) {
	for value, want := range map[string]uint64{"512B": 512, "1.5KiB": 1536, "12MiB": 12 << 20, "2kB": 2000, " 1.2GB ": 1_200_000_000, "bogus": 0} {
		if got := parseSize(value); got != want {
			t.Errorf("%q: got %d want %d", value, got, want)
		}
	}
}
