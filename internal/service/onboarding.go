package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
)

const onboardingKey = "onboarding.v1"

// OnboardingState stores only progress and references, never form drafts or conversation text.
type OnboardingState struct {
	Version              int           `json:"version"`
	Status               string        `json:"status"`
	Step                 int           `json:"step"`
	Agent                session.Agent `json:"agent"`
	ProviderID           string        `json:"provider_id"`
	SandboxID            string        `json:"sandbox_id"`
	ConversationVerified bool          `json:"conversation_verified"`
}

func (a *App) GetOnboardingState() (OnboardingState, error) {
	a.onboardingMu.Lock()
	defer a.onboardingMu.Unlock()
	raw, err := a.store.SettingValue(onboardingKey)
	if err != nil {
		return OnboardingState{}, err
	}
	if raw != "" {
		var state OnboardingState
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return state, errors.New("教程进度读取失败，请检查应用数据")
		}
		return state, validateOnboarding(state)
	}
	// Existing users get the permanent entry without an unsolicited welcome dialog.
	providers, err := a.store.Providers()
	if err != nil {
		return OnboardingState{}, err
	}
	sandboxes, err := a.store.Sandboxes()
	if err != nil {
		return OnboardingState{}, err
	}
	services, err := a.store.MCPServices()
	if err != nil {
		return OnboardingState{}, err
	}
	state := OnboardingState{Version: 1, Status: "new", Agent: session.Codex}
	if len(providers)+len(sandboxes)+len(services) > 0 {
		state.Status = "paused"
	}
	// Empty metadata files and saved preferences also indicate a previous installation.
	for _, name := range []string{"providers.json", "sandboxes.json", "mcp.json", "settings.json"} {
		if _, err := os.Stat(filepath.Join(a.store.Dir(), name)); err == nil {
			state.Status = "paused"
		} else if !errors.Is(err, os.ErrNotExist) {
			return OnboardingState{}, err
		}
	}
	return state, a.writeOnboarding(state)
}

func validateOnboarding(state OnboardingState) error {
	if state.Version != 1 || state.Step < 0 || state.Step > 4 || !state.Agent.Valid() {
		return errors.New("教程进度无效")
	}
	switch state.Status {
	case "new", "active", "paused", "completed":
	default:
		return errors.New("教程状态无效")
	}
	return nil
}

func (a *App) SaveOnboardingState(state OnboardingState) (OnboardingState, error) {
	a.onboardingMu.Lock()
	defer a.onboardingMu.Unlock()
	if err := validateOnboarding(state); err != nil {
		return state, err
	}
	// Removed resources are cleared on the next save. Other read failures must stay visible.
	if state.ProviderID != "" {
		p, err := a.store.Provider(state.ProviderID)
		if errors.Is(err, store.ErrNotFound) || err == nil && p.Agent != state.Agent {
			state.ProviderID, state.SandboxID = "", ""
			state.ConversationVerified = false
		} else if err != nil {
			return state, err
		}
	}
	if state.SandboxID != "" {
		s, err := a.store.Sandbox(state.SandboxID)
		if errors.Is(err, store.ErrNotFound) || err == nil && s.ProviderFor(state.Agent) != state.ProviderID {
			state.SandboxID = ""
			state.ConversationVerified = false
		} else if err != nil {
			return state, err
		}
	}
	return state, a.writeOnboarding(state)
}

func (a *App) writeOnboarding(state OnboardingState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return a.store.SetSetting(onboardingKey, string(data))
}

type OnboardingEnvironment struct {
	Checks          []runtime.Check `json:"checks"`
	CanContinue     bool            `json:"can_continue"`
	RestartRequired bool            `json:"restart_required"`
	InstallCommand  string          `json:"install_command"`
}

func (a *App) CheckOnboardingEnvironment() OnboardingEnvironment {
	checks, command := runtime.CheckDependencies()
	result := OnboardingEnvironment{Checks: checks, InstallCommand: command}
	for _, check := range checks {
		if check.Status != "detected" {
			return result
		}
	}
	// Probe afresh after installation; do not replace the live manager while other calls use it.
	if a.manager == nil {
		result.RestartRequired = true
		result.Checks = append(result.Checks, runtime.Check{ID: "restart", Name: "重新打开应用", Status: "pending", Detail: "依赖已安装，请退出并重新打开 App，再从使用教程继续。"})
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report := a.manager.Environment().Inspect(ctx)
	return onboardingEnvironmentReport(result, report)
}

func onboardingEnvironmentReport(result OnboardingEnvironment, report runtime.Report) OnboardingEnvironment {
	result.CanContinue = true
	for _, check := range report.Checks {
		if check.ID == "platform" {
			continue
		}
		result.Checks = append(result.Checks, check)
		preparedOnStart := check.ID == "vm" && (check.Status == "missing" || check.Status == "stopped") ||
			check.ID == "docker_context" && check.Status == "missing"
		if check.Status != "ready" && !preparedOnStart {
			result.CanContinue = false
		}
	}
	return result
}
