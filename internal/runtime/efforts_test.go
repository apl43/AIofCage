package runtime

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/session"
)

// 档位和锁定版本的官方 CLI 一致：Codex 按模型（codex debug models），Claude Code 按 --effort，dsh 按 llm-pi-ai。
func TestAgentEffortsFollowTheCLIs(t *testing.T) {
	codex := AgentEfforts(session.Codex)
	for _, tc := range []struct {
		model, def string
		levels     []string
	}{
		{"gpt-6.1-sol", "low", []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
		{"gpt-6-sol", "medium", []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
		{"gpt-6-luna", "medium", []string{"low", "medium", "high", "xhigh", "max"}},
		{"gpt-5.5", "medium", []string{"low", "medium", "high", "xhigh"}},
		{"deepseek-flash", "", []string{"low", "medium", "high"}}, // 目录外的模型
	} {
		got := codex.For(tc.model)
		if !slices.Equal(got.Levels, tc.levels) || got.Default != tc.def {
			t.Errorf("Codex %s：%+v", tc.model, got)
		}
	}
	if got := AgentEfforts(session.ClaudeCode).For("deepseek-flash"); !slices.Equal(got.Levels, []string{"low", "medium", "high", "xhigh", "max"}) || got.Default != "high" {
		t.Errorf("Claude Code：%+v", got)
	}
	if got := AgentEfforts(session.DSH).For("deepseek-flash"); !slices.Equal(got.Levels, []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}) || got.Default != "" {
		t.Errorf("dsh：%+v", got)
	}
}

func TestEffortAllows(t *testing.T) {
	for _, tc := range []struct {
		agent         session.Agent
		model, effort string
		ok            bool
	}{
		{session.Codex, "gpt-6.1-sol", "", true},
		{session.Codex, "gpt-6.1-sol", "ultra", true},
		{session.Codex, "gpt-5.5", "max", false},
		{session.Codex, "deepseek-flash", "xhigh", false},
		{session.Codex, "deepseek-flash", "high", true},
		{session.ClaudeCode, "deepseek-flash", "max", true},
		{session.ClaudeCode, "deepseek-flash", "off", false},
		{session.DSH, "deepseek-flash", "off", true},
		{session.DSH, "deepseek-flash", "ultra", false},
		{session.Codex, "constructor", "ultra", false},
		{session.Agent("other"), "x", "low", false},
	} {
		if got := AgentEfforts(tc.agent).Allows(tc.model, tc.effort); got != tc.ok {
			t.Errorf("%s %s %q：%v，应为 %v", tc.agent, tc.model, tc.effort, got, tc.ok)
		}
	}
}

// 前端按这个形状读档位表。
func TestEffortTableJSON(t *testing.T) {
	data, err := json.Marshal(AgentEfforts(session.Codex))
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		Models map[string]struct {
			Levels  []string `json:"levels"`
			Default string   `json:"default"`
		} `json:"models"`
		Fallback struct {
			Levels []string `json:"levels"`
		} `json:"fallback"`
	}
	if err := json.Unmarshal(data, &shape); err != nil || shape.Models["gpt-6.1-sol"].Default != "low" || len(shape.Fallback.Levels) != 3 {
		t.Fatalf("档位表的 JSON 形状不对：%s %v", data, err)
	}
}

// dsh 的 off：pi-ai 要求至少声明一个思考档位，off 自己不带值；选中 off 时请求明确关闭思考（11-02 抓包确认）。
func TestDSHEffortOff(t *testing.T) {
	p := provider.Provider{Model: "deepseek-flash", Protocol: provider.Anthropic, BaseURL: "https://api.example.test/anthropic"}
	patch := DSHConversationPatch(p, "", "off")
	if !strings.Contains(patch, "reasoningEfforts:\n              \"off\":\n              \"low\": \"low\"\n") || !strings.Contains(patch, `reasoningEffort: "off"`) {
		t.Fatalf("off 的写法不对：\n%s", patch)
	}
	if patch := DSHConversationPatch(p, "", "xhigh"); !strings.Contains(patch, `"xhigh": "xhigh"`) || !strings.Contains(patch, `reasoningEffort: "xhigh"`) {
		t.Fatalf("xhigh 的写法不对：\n%s", patch)
	}
	if patch := DSHConversationPatch(p, "", ""); strings.Contains(patch, "reasoningEffort") {
		t.Fatalf("模型默认不应写思考档位：\n%s", patch)
	}
}
