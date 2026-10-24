package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/sandbox"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
	"github.com/shaoxier/AIofCage/internal/vault"
)

// TestEndToEndDatabaseMCP runs a throwaway PostgreSQL in the VM, publishes it on the Mac's
// loopback like a local database, and checks that agents can query it through the gatekeeper
// while the password never enters the sandbox.
func TestEndToEndDatabaseMCP(t *testing.T) {
	if os.Getenv("AIOFCAGE_E2E") == "" {
		t.Skip("set AIOFCAGE_E2E=1 to run against the real VM and Keychain")
	}
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	// Spaces and quotes exercise the Keychain encoding and the URL escaping in the connector.
	password := "pw " + hex.EncodeToString(raw[:]) + ` "q'`
	docker := func(args ...string) ([]byte, error) {
		return exec.Command("docker", append([]string{"--context", runtime.DockerContext}, args...)...).CombinedOutput()
	}
	db := "aiofcage-e2e-postgres"
	_, _ = docker("rm", "--force", db)
	if out, err := docker("run", "--detach", "--name", db, "--env", "POSTGRES_USER=e2e", "--env", "POSTGRES_DB=e2e",
		"--env", "POSTGRES_PASSWORD="+password, "--publish", "127.0.0.1:55433:5432",
		"public.ecr.aws/docker/library/postgres:17-alpine"); err != nil {
		t.Fatalf("postgres: %v %s", err, out)
	}
	t.Cleanup(func() { _, _ = docker("rm", "--force", db) })
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(500 * time.Millisecond) {
		if _, err := docker("exec", db, "psql", "-U", "e2e", "-d", "e2e", "-tAc", "select 1"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("postgres did not become ready")
		}
	}
	if out, err := docker("exec", db, "psql", "-U", "e2e", "-d", "e2e", "-c",
		"create table items (name text); insert into items values ('alpha'), ('beta'), ('gamma');"); err != nil {
		t.Fatalf("seed: %v %s", err, out)
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
	service, err := app.SaveMCPService(SaveMCPServiceRequest{Name: "e2e 数据库", Kind: "postgres", Preset: "custom",
		URL: "postgresql://e2e@localhost:55433/e2e?sslmode=disable", Token: password})
	if err != nil || !service.KeySaved || strings.Contains(service.URL, password) {
		t.Fatalf("service: %+v %v", service, err)
	}
	t.Cleanup(func() { _ = app.DeleteMCPService(service.ID) })
	// "测试连接" logs in through a throwaway connector: the right password works, a wrong one is named.
	if result, _ := app.TestMCPService(service.ID); !result.OK || !strings.Contains(result.Message, "登录成功") {
		t.Fatalf("test: %+v", result)
	}
	wrong, err := app.SaveMCPService(SaveMCPServiceRequest{Name: "e2e 错误密码", Kind: "postgres", Preset: "custom",
		URL: "postgresql://e2e@localhost:55433/e2e?sslmode=disable", Token: "not-the-password"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.DeleteMCPService(wrong.ID) })
	if result, _ := app.TestMCPService(wrong.ID); result.OK || !strings.Contains(result.Message, "用户名或密码不对") {
		t.Fatalf("wrong password: %+v", result)
	}
	view, err := app.CreateSandbox(CreateSandboxRequest{Name: "e2e-db-" + time.Now().Format("150405"), ProjectMode: ProjectNew,
		Bindings: []sandbox.Binding{{Agent: session.ClaudeCode}, {Agent: session.Codex}}, MCP: []string{service.ID}})
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	run := func(script string) string {
		var out bytes.Buffer
		_ = manager.Exec(ctx, view.ID, false, nil, &out, &out, "bash", "-c", script)
		return out.String()
	}
	token := "AIOFCAGE_MCP_" + strings.ToUpper(strings.ReplaceAll(service.ID, "-", "_")) + "_TOKEN"
	script := `U=http://gatekeeper:8890/mcp/` + service.ID + `; A="Authorization: Bearer $` + token + `"
S=$(curl -sS -D - -o /dev/null -X POST $U -H "$A" -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}' | tr -d '\r' | awk -F': ' 'tolower($1)=="mcp-session-id"{print $2}')
curl -sS -X POST $U -H "$A" -H "Mcp-Session-Id: $S" -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'
curl -sS -X POST $U -H "$A" -H "Mcp-Session-Id: $S" -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query","arguments":{"sql":"select string_agg(name, '\'','\'' order by name) as names from items"}}}'; echo
curl -sS -X POST $U -H "$A" -H "Mcp-Session-Id: $S" -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"query","arguments":{"sql":"delete from items"}}}'; echo
perl -e 'alarm 60; exec @ARGV' claude mcp list 2>&1 | grep ` + service.ID + `
cat ~/.claude.json ~/.codex/config.toml ~/.aiofcage/env; env`
	out := run(script)
	t.Logf("sandbox output:\n%s", out)
	if !strings.Contains(out, "alpha,beta,gamma") {
		t.Error("query through the gatekeeper failed")
	}
	if !strings.Contains(out, "read-only transaction") {
		t.Error("writes must be refused")
	}
	if !strings.Contains(out, "Connected") {
		t.Error("Claude Code did not connect to the database MCP")
	}
	if strings.Contains(out, password) {
		t.Fatal("the database password is visible inside the sandbox")
	}
	var events []EventView
	app.tick(ctx)
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		app.tick(ctx)
		if events, _ = app.RecentEvents(view.ID); countTool(events, "query") >= 2 {
			break
		}
	}
	if countTool(events, "query") < 2 {
		t.Errorf("tool calls not recorded: %+v", events)
	}
	results, err := app.SimulateAttacks(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		t.Logf("attack %-13s blocked=%v skipped=%v\n%s", r.ID, r.Blocked, r.Skipped, r.Evidence)
		if !r.Blocked && !r.Skipped || (r.ID == "db-direct" && r.Skipped) {
			t.Errorf("attack %s not blocked: %s", r.ID, r.Evidence)
		}
		if strings.Contains(r.Evidence, password) {
			t.Errorf("attack %s evidence contains the password", r.ID)
		}
	}
	if err := app.StopSandbox(view.ID); err != nil {
		t.Fatal(err)
	}
	if out, _ := docker("ps", "--all", "--quiet", "--filter", "label=io.aiofcage.sandbox="+view.ID, "--filter", "status=running"); len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("containers still running after stop: %s", out)
	}
}

func countTool(events []EventView, tool string) int {
	n := 0
	for _, e := range events {
		if e.ToolID == tool {
			n++
		}
	}
	return n
}
