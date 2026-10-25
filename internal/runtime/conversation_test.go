package runtime

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/provider"
)

func TestAgentConversationBridge(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(python, "-B", "testdata/test_agent_turn.py", "assets/agent/agent-turn.py").CombinedOutput()
	if err != nil {
		t.Fatalf("会话桥接回归失败：%v\n%s", err, output)
	}
	t.Log(string(output))
	p := provider.Provider{Model: "original", Protocol: provider.OpenAIChat, BaseURL: "https://api.example.test/v1"}
	patch := DSHConversationPatch(p, "selected", "high")
	if !strings.Contains(patch, "reasoningEfforts:") || !strings.Contains(patch, `reasoningEffort: "high"`) || !strings.Contains(patch, `model: "selected"`) || p.Model != "original" {
		t.Fatalf("会话覆盖配置有误：%s", patch)
	}
}
