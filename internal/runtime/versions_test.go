package runtime

import (
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
)

func TestVersionPinsPreserveLegacyAndAffectImage(t *testing.T) {
	s := sandbox.Sandbox{Bindings: []sandbox.Binding{{Agent: session.Codex}}}
	pins := PinnedAgentVersions(s)
	if pins[session.Codex] != "0.164.0" {
		t.Fatal("旧沙箱版本发生漂移")
	}
	pins[session.Codex] = "0.163.0"
	if PinnedAgentVersions(s)[session.Codex] != "0.164.0" {
		t.Fatal("修改了全局旧版本映射")
	}
	before, _ := agentContextVersions(s.Agents(), PinnedAgentVersions(s))
	after, err := agentContextVersions(s.Agents(), pins)
	if err != nil || before.tag == after.tag || !strings.Contains(string(after.files["Dockerfile"]), "@openai/codex@0.163.0") {
		t.Fatalf("版本未进入内容摘要与安装命令: %v", err)
	}
	for _, bad := range []string{"latest", "1.2.3;sh", "../escape", "1.2.3\nRUN echo bad", "1.2"} {
		if ValidAgentVersion(bad) {
			t.Fatalf("接受了非法版本 %q", bad)
		}
	}
}
