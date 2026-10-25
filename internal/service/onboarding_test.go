package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
)

func TestOnboardingFirstLaunchAndResume(t *testing.T) {
	app, secrets, st := newApp(t)
	state, err := app.GetOnboardingState()
	if err != nil || state.Status != "new" || state.Step != 0 {
		t.Fatalf("first launch: %+v %v", state, err)
	}
	p, err := app.SaveProvider(codexRequest())
	if err != nil {
		t.Fatal(err)
	}
	s := sandbox.Sandbox{ID: "tutorial-sandbox", Name: "tutorial", Bindings: []sandbox.Binding{{Agent: session.Codex, ProviderID: p.ID}}}
	if err := st.SaveSandbox(s); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Step, state.ProviderID, state.SandboxID = "paused", 3, p.ID, s.ID
	if _, err := app.SaveOnboardingState(state); err != nil {
		t.Fatal(err)
	}
	reopened := NewApp(st, secrets, nil, nil, nil)
	got, err := reopened.GetOnboardingState()
	if err != nil || got != state {
		t.Fatalf("resume: %+v %v", got, err)
	}
	state.Status, state.Step = "active", 0
	if _, err := reopened.SaveOnboardingState(state); err != nil {
		t.Fatal(err)
	}
	ps, _ := st.Providers()
	ss, _ := st.Sandboxes()
	if len(ps) != 1 || len(ss) != 1 {
		t.Fatal("restarting tutorial changed resources")
	}
	data, err := os.ReadFile(filepath.Join(st.Dir(), "settings.json"))
	if err != nil || strings.Contains(string(data), codexRequest().APIKey) {
		t.Fatal("progress contains a secret or cannot be read", err)
	}
}

func TestOnboardingExistingUsersAreNotInterrupted(t *testing.T) {
	app, _, _ := newApp(t)
	if _, err := app.SaveProvider(codexRequest()); err != nil {
		t.Fatal(err)
	}
	state, err := app.GetOnboardingState()
	if err != nil || state.Status != "paused" {
		t.Fatalf("existing user: %+v %v", state, err)
	}
}

func TestOnboardingDetectsEmptyPreviousInstallation(t *testing.T) {
	for _, file := range []string{"providers.json", "sandboxes.json", "mcp.json", "settings.json"} {
		t.Run(file, func(t *testing.T) {
			app, _, st := newApp(t)
			data := "[]"
			if file == "settings.json" {
				data = `{"some_preference":"value"}`
			}
			if err := os.WriteFile(filepath.Join(st.Dir(), file), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			state, err := app.GetOnboardingState()
			if err != nil || state.Status != "paused" {
				t.Fatalf("existing metadata triggered welcome: %+v %v", state, err)
			}
		})
	}
}

func TestOnboardingDoesNotRequireAnAlreadyRunningVM(t *testing.T) {
	for _, status := range []string{"missing", "stopped", "ready", "error", "unknown"} {
		report := runtime.Report{Checks: []runtime.Check{{ID: "vm", Status: status}}}
		got := onboardingEnvironmentReport(OnboardingEnvironment{}, report)
		want := status == "missing" || status == "stopped" || status == "ready"
		if got.CanContinue != want {
			t.Fatalf("VM %s: got %v want %v", status, got.CanContinue, want)
		}
	}
}

func TestOnboardingAllowsOnlyMissingDockerContext(t *testing.T) {
	for _, status := range []string{"missing", "ready", "error", "unknown"} {
		report := runtime.Report{Checks: []runtime.Check{{ID: "vm", Status: "ready"}, {ID: "docker_context", Status: status}}}
		got := onboardingEnvironmentReport(OnboardingEnvironment{}, report)
		if got.CanContinue != (status == "missing" || status == "ready") {
			t.Fatalf("Docker connection %s: %+v", status, got)
		}
	}
}

func TestOnboardingReadErrorsNeverBecomeFirstLaunch(t *testing.T) {
	for _, file := range []string{"settings.json", "providers.json", "sandboxes.json", "mcp.json"} {
		t.Run(file, func(t *testing.T) {
			app, _, st := newApp(t)
			if err := os.WriteFile(filepath.Join(st.Dir(), file), []byte("broken json"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := app.GetOnboardingState(); err == nil {
				t.Fatal("read failure treated as new user")
			}
		})
	}
}

func TestOnboardingRejectsNullSettings(t *testing.T) {
	app, _, st := newApp(t)
	if err := os.WriteFile(filepath.Join(st.Dir(), "settings.json"), []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetOnboardingState(); err == nil {
		t.Fatal("null settings treated as new user")
	}
	if err := st.SetSetting("test", "value"); err == nil {
		t.Fatal("damaged settings overwritten")
	}
}

func TestOnboardingValidatesAndClearsRemovedReferences(t *testing.T) {
	app, _, st := newApp(t)
	state, _ := app.GetOnboardingState()
	state.Step = 99
	if _, err := app.SaveOnboardingState(state); err == nil {
		t.Fatal("accepted invalid step")
	}
	state.Step, state.Status = 1, "unknown"
	if _, err := app.SaveOnboardingState(state); err == nil {
		t.Fatal("accepted invalid status")
	}
	p, _ := app.SaveProvider(codexRequest())
	state.Status, state.ProviderID = "active", p.ID
	state.SandboxID, state.ConversationVerified = "removed", true
	saved, err := app.SaveOnboardingState(state)
	if err != nil || saved.SandboxID != "" || saved.ConversationVerified || saved.ProviderID != p.ID {
		t.Fatalf("removed sandbox: %+v %v", saved, err)
	}
	if err := st.DeleteProvider(p.ID); err != nil {
		t.Fatal(err)
	}
	saved, err = app.SaveOnboardingState(state)
	if err != nil || saved.ProviderID != "" || saved.SandboxID != "" {
		t.Fatalf("removed provider: %+v %v", saved, err)
	}
}
