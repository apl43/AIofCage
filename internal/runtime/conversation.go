package runtime

import (
	"context"
	"os/exec"

	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/sandbox"
)

// ConversationCommand 只在容器内启动官方 CLI，正文及公开配置通过标准输入交付。
func (m *Manager) ConversationCommand(ctx context.Context, sbx sandbox.Sandbox, id string) *exec.Cmd {
	args := m.ExecArgs(sbx, false, "env", "AIOFCAGE_TERMINAL_ID="+id, "python3", "/opt/aiofcage/agent-turn.py")
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = childEnv()
	return cmd
}

// DSHConversationPatch 为单次进程配置模型，不改写其他会话的全局设置。
func DSHConversationPatch(p provider.Provider, model, effort string) string {
	p.Model = model
	return dshConfigWithEffort(p, effort)
}
