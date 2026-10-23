package service

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
	"github.com/shaoxier/AIofCage/internal/vault"
)

// TestEndToEnd drives the real VM and Keychain. Run with AIOFCAGE_E2E=1 on a machine where
// `cagectl env up` works and the Keychain holds the sbx-builderday-key item.
func TestEndToEnd(t *testing.T) {
	if os.Getenv("AIOFCAGE_E2E") == "" {
		t.Skip("set AIOFCAGE_E2E=1 to run against the real VM and Keychain")
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keychain := vault.NewKeychain()
	manager, err := runtime.NewManager(st, keychain)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, keychain, manager, nil, nil)
	var opened string
	app.terminal = func(command string) error { opened = command; return nil }

	p, err := app.SaveProvider(SaveProviderRequest{Name: "七牛 xapi", Agent: session.Codex, Protocol: provider.OpenAIResponses,
		BaseURL: "https://xapi.qiniu.io/v1", Model: "gpt-6.1-sol", Auth: provider.AuthBearer, ExistingKeyRef: "sbx-builderday-key"})
	if err != nil || !p.KeySaved {
		t.Fatalf("provider: %+v %v", p, err)
	}
	name := "e2e-" + time.Now().Format("150405")
	view, err := app.CreateSandbox(CreateSandboxRequest{Name: name, ProjectMode: ProjectNew,
		Bindings: []sandbox.Binding{{Agent: session.Codex, ProviderID: p.ID}}, MaxRequests: 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = app.DeleteSandbox(view.ID, false)
		_ = os.RemoveAll(view.ProjectPath)
	})
	if err := app.StartSandbox(view.ID); err != nil {
		t.Fatal(err)
	}
	views, _ := app.ListSandboxes()
	if len(views) != 1 || views[0].State != "running" {
		t.Fatalf("not running: %+v", views)
	}
	if err := app.OpenTerminal(view.ID, ""); err != nil {
		t.Fatal(err)
	}
	// Run the exact command Terminal would get, minus the TTY, to prove it reaches the sandbox.
	command := strings.Replace(opened, "'--tty' ", "", 1) + " -c 'id -u; pwd; echo $AIOFCAGE_CODEX_API_KEY'"
	command = strings.Replace(command, "'bash' -c", "'bash' '-c'", 1)
	out, err := exec.Command("/bin/sh", "-c", command).Output()
	if err != nil {
		t.Fatalf("terminal command failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 || !strings.HasSuffix(lines[len(lines)-3], "1000") || lines[len(lines)-2] != view.ProjectPath || !strings.HasPrefix(lines[len(lines)-1], "cage-") {
		t.Fatalf("unexpected terminal output: %q", out)
	}
	key, _ := keychain.Read(context.Background(), "sbx-builderday-key")
	if len(key) > 0 && bytes.Contains(out, key) {
		t.Fatal("real key visible inside the sandbox")
	}
	if err := app.BindProvider(view.ID, session.Codex, ""); err != nil {
		t.Fatal(err)
	}
	if err := app.StopSandbox(view.ID); err != nil {
		t.Fatal(err)
	}
	if views, _ = app.ListSandboxes(); views[0].State != "stopped" {
		t.Fatalf("not stopped: %+v", views[0])
	}
}
