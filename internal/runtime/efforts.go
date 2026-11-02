package runtime

import (
	"slices"

	"github.com/shaoxier/AIofCage/internal/session"
)

// EffortChoices are the reasoning levels a conversation can pick, lowest first. Default is the level
// the CLI uses when none is given, empty when it leaves the choice to the provider.
type EffortChoices struct {
	Levels  []string `json:"levels"`
	Default string   `json:"default,omitempty"`
}

// EffortTable holds an agent's reasoning levels; only Codex differs by model.
type EffortTable struct {
	Models   map[string]EffortChoices `json:"models,omitempty"`
	Fallback EffortChoices            `json:"fallback"`
}

// For returns the levels offered with model.
func (t EffortTable) For(model string) EffortChoices {
	if choices, ok := t.Models[model]; ok {
		return choices
	}
	return t.Fallback
}

// Allows reports whether effort can be chosen with model; "" leaves it to the CLI.
func (t EffortTable) Allows(model, effort string) bool {
	return effort == "" || slices.Contains(t.For(model).Levels, effort)
}

// AgentEfforts returns the official reasoning levels of the pinned CLI versions (agentCatalog):
//   - Codex 0.164.0: per model, from the catalog `codex debug models` prints. Codex maps "ultra" to a
//     level the model accepts and delegates parts of the task to sub-agents. Models outside the
//     catalog keep low, medium and high.
//   - Claude Code 2.1.309: the values of `--effort`; without it the CLI asks for high.
//   - dsh 0.2.1-rc.2: the reasoningEfforts keys of its llm-pi-ai adapter, "off" included.
func AgentEfforts(agent session.Agent) EffortTable {
	switch agent {
	case session.Codex:
		ultra := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
		maxLevel := []string{"low", "medium", "high", "xhigh", "max"}
		return EffortTable{Models: map[string]EffortChoices{
			"gpt-6.1-sol":              {Levels: ultra, Default: "low"},
			"gpt-6-astra":              {Levels: ultra, Default: "low"},
			"gpt-6-sol":                {Levels: ultra, Default: "medium"},
			"gpt-6-luna":               {Levels: maxLevel, Default: "medium"},
			"gpt-5.6-sol":              {Levels: ultra, Default: "low"},
			"gpt-5.6-terra":            {Levels: ultra, Default: "medium"},
			"gpt-5.6-luna":             {Levels: maxLevel, Default: "medium"},
			"gpt-5.5":                  {Levels: []string{"low", "medium", "high", "xhigh"}, Default: "medium"},
			"gpt-daybreak-blue-latest": {Levels: ultra, Default: "low"},
			"gpt-daybreak-red-latest":  {Levels: ultra, Default: "medium"},
			"codex-auto-review":        {Levels: maxLevel, Default: "medium"},
		}, Fallback: EffortChoices{Levels: []string{"low", "medium", "high"}}}
	case session.ClaudeCode:
		return EffortTable{Fallback: EffortChoices{Levels: []string{"low", "medium", "high", "xhigh", "max"}, Default: "high"}}
	case session.DSH:
		return EffortTable{Fallback: EffortChoices{Levels: []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}}}
	}
	return EffortTable{}
}
