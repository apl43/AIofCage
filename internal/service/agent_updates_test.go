package service

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
)

func TestAgentDefaultSelectionDoesNotUpgradeSandboxes(t *testing.T) {
	app, _, st := newApp(t)
	existing := sandbox.Sandbox{ID: "old", Name: "old", Bindings: []sandbox.Binding{{Agent: session.Codex}}, AgentVersions: map[session.Agent]string{session.Codex: "0.164.0"}}
	if err := st.SaveSandbox(existing); err != nil {
		t.Fatal(err)
	}
	checks, _ := json.Marshal([]AgentUpdate{{Agent: session.Codex, Latest: "0.165.0"}})
	if err := st.SetSetting(agentChecksKey, string(checks)); err != nil {
		t.Fatal(err)
	}
	if err := app.SetAgentDefaultVersion(session.Codex, "0.165.0"); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Sandbox("old")
	defaults, _ := runtime.DefaultAgentVersions(st)
	if defaults[session.Codex] != "0.165.0" || after.AgentVersions[session.Codex] != "0.164.0" {
		t.Fatal("默认更新影响了既有沙箱")
	}
	if app.SetAgentDefaultVersion(session.Codex, "99.1.0") == nil {
		t.Fatal("允许未经检查的版本")
	}
	if err := app.SetAgentDefaultVersion(session.Codex, "0.164.0"); err != nil {
		t.Fatal(err)
	}
}

func TestVersionOrdering(t *testing.T) {
	for _, pair := range [][2]string{{"0.10.0", "0.9.9"}, {"1.0.0", "1.0.0-rc.10"}, {"1.0.0-rc.10", "1.0.0-rc.2"}} {
		if !versionNewer(pair[0], pair[1]) || versionNewer(pair[1], pair[0]) {
			t.Fatalf("版本顺序错误: %v", pair)
		}
	}
	if versionNewer("invalid", "1.0.0") || versionNewer("1.0.0", "1.0.0") {
		t.Fatal("无效或相同版本不应提示更新")
	}
}

func TestOfficialNPMVersionCheck(t *testing.T) {
	if os.Getenv("AIOFCAGE_NPM_CHECK") != "1" {
		t.Skip("设置 AIOFCAGE_NPM_CHECK=1 只读检查官方 npm 元数据")
	}
	app, _, _ := newApp(t)
	items, err := app.CheckAgentUpdates()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Error != "" || !runtime.ValidAgentVersion(item.Latest) {
			t.Errorf("%s: %s", item.Name, item.Error)
		} else {
			t.Logf("%s 官方 latest=%s", item.Name, item.Latest)
		}
	}
}
