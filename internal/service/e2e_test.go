package service

import (
	"bytes"
	"context"
	"io"
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
	// Monitor: traffic inside the sandbox must show up as recorded events and usage.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.tick(ctx)
	traffic := `curl -s -o /dev/null https://example.com; curl -s -o /dev/null -H "Authorization: Bearer $AIOFCAGE_CODEX_API_KEY" https://xapi.qiniu.io/v1/models -m 15; true`
	if err := manager.Exec(ctx, view.ID, false, nil, io.Discard, io.Discard, "bash", "-c", traffic); err != nil {
		t.Fatal(err)
	}
	var events []EventView
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		events, _ = app.RecentEvents(view.ID)
		if countReason(events, "default_deny") > 0 && countHost(events, "xapi.qiniu.io") > 0 {
			break
		}
	}
	if countReason(events, "default_deny") == 0 || countHost(events, "xapi.qiniu.io") == 0 {
		t.Fatalf("events not recorded: %+v", events)
	}
	if data, err := os.ReadFile(app.auditPath(view.ID)); err != nil || bytes.Contains(data, key) || !bytes.Contains(data, []byte("example.com")) {
		t.Fatalf("audit log missing or leaking: %v", err)
	}
	app.tick(ctx)
	summary, _ := app.UsageSummary()
	if len(summary.Sandboxes) != 1 || !summary.Sandboxes[0].Live || summary.Sandboxes[0].Denied == 0 || summary.Sandboxes[0].Stats == nil {
		t.Fatalf("usage summary %+v", summary.Sandboxes)
	}
	if summary.Providers[p.ID].Requests == 0 {
		t.Fatalf("provider usage not counted: %+v", summary.Providers)
	}
	results, err := app.SimulateAttacks(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		t.Logf("attack %-13s blocked=%v skipped=%v\n%s", r.ID, r.Blocked, r.Skipped, r.Evidence)
		if !r.Blocked && !r.Skipped {
			t.Errorf("attack %s not blocked: %s", r.ID, r.Evidence)
		}
		if bytes.Contains([]byte(r.Evidence), key) {
			t.Errorf("attack %s evidence contains the real key", r.ID)
		}
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

func countReason(events []EventView, reason string) int {
	n := 0
	for _, e := range events {
		if e.ReasonCode == reason {
			n++
		}
	}
	return n
}

func countHost(events []EventView, host string) int {
	n := 0
	for _, e := range events {
		if e.Host == host && e.SecretRef != "" {
			n++
		}
	}
	return n
}
