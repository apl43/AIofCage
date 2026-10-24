package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
)

// 增装只追加智能体，供应商由独立绑定流程设置。
func addedBindings(current []sandbox.Binding, agents []session.Agent) ([]sandbox.Binding, error) {
	result := append([]sandbox.Binding{}, current...)
	seen := map[session.Agent]bool{}
	for _, b := range current {
		seen[b.Agent] = true
	}
	if len(agents) == 0 {
		return nil, errors.New("请选择要添加的智能体")
	}
	for _, agent := range agents {
		if !agent.Valid() {
			return nil, fmt.Errorf("不支持的智能体：%s", agent)
		}
		if !seen[agent] {
			result = append(result, sandbox.Binding{Agent: agent})
			seen[agent] = true
		}
	}
	return result, nil
}

// AddAgents 在镜像准备完成后才中断运行；失败时恢复配置和原运行状态。
func (m *Manager) AddAgents(ctx context.Context, id string, agents []session.Agent) error {
	defer m.lock(id)()
	original, err := m.store.Sandbox(id)
	if err != nil {
		return err
	}
	bindings, err := addedBindings(original.Bindings, agents)
	if err != nil {
		return err
	}
	if len(bindings) == len(original.Bindings) {
		return nil
	}
	candidate := original
	candidate.Bindings = bindings
	build, err := agentContext(candidate.Agents())
	if err != nil {
		return err
	}
	m.progress(id, "准备智能体镜像，当前沙箱继续运行")
	if err := m.env.Ensure(ctx); err != nil {
		return err
	}
	if err := m.ensureImage(ctx, build, func(message string) { m.progress(id, message) }); err != nil {
		return err
	}
	state := m.Status(ctx, id).State
	if state == statePartial {
		return errors.New("沙箱组件状态不一致，请先停止沙箱再添加智能体")
	}
	running := state == stateRunning
	if running {
		m.progress(id, "保存用量并更新容器，终端会话将结束")
		m.saveUsage(ctx, original)
		if err := m.stopContainers(ctx, names(id)); err != nil {
			return err
		}
	}
	if err := m.store.UpdateSandbox(id, func(s *sandbox.Sandbox) error { s.Bindings = bindings; return nil }); err != nil {
		return err
	}
	if running {
		if err := m.start(ctx, id); err != nil {
			recovery, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			restoreErr := m.store.UpdateSandbox(id, func(s *sandbox.Sandbox) error { s.Bindings = original.Bindings; return nil })
			if restoreErr == nil {
				restoreErr = m.start(recovery, id)
			}
			if restoreErr != nil {
				return fmt.Errorf("添加失败：%v；恢复原沙箱失败：%w。项目和主目录仍保留，可重试启动", err, restoreErr)
			}
			return fmt.Errorf("添加失败，已恢复原沙箱：%w", err)
		}
	}
	m.progress(id, "智能体已添加")
	return nil
}
