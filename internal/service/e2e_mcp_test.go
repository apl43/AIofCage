package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
		"create table items (name text); insert into items values ('alpha'), ('beta'), ('gamma');"+
			"create role e2e_reader login password '"+strings.ReplaceAll(password, "'", "''")+"';"+
			"grant connect on database e2e to e2e_reader; grant usage on schema public to e2e_reader; grant select on items to e2e_reader;"); err != nil {
		t.Fatalf("seed: %v %s", err, strings.ReplaceAll(string(out), password, "[redacted]"))
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
		URL: "postgresql://e2e_reader@localhost:55433/e2e?sslmode=disable", Token: password})
	if err != nil || !service.KeySaved || strings.Contains(service.URL, password) {
		t.Fatalf("service: %+v %v", service, err)
	}
	t.Cleanup(func() { _ = app.DeleteMCPService(service.ID) })
	// "测试连接" logs in through a throwaway connector: the right password works, a wrong one is named.
	if result, _ := app.TestMCPService(service.ID); !result.OK || !strings.Contains(result.Message, "登录成功") {
		t.Fatalf("test: %+v", result)
	}
	admin, err := app.SaveMCPService(SaveMCPServiceRequest{Name: "e2e 管理账号拒绝", Kind: "postgres", Preset: "custom",
		URL: "postgresql://e2e@localhost:55433/e2e?sslmode=disable", Token: password})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.DeleteMCPService(admin.ID) })
	if result, _ := app.TestMCPService(admin.ID); result.OK || !strings.Contains(result.Message, "权限过高") {
		t.Fatalf("管理账号未被拒绝：%+v", result)
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
curl -sS -X POST $U -H "$A" -H "Mcp-Session-Id: $S" -d '{"jsonrpc":"2.0","id":4,"method":"resources/list"}'; echo
curl -sS -X POST $U -H "$A" -H "Mcp-Session-Id: $S" -d '{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"postgres://e2e_reader@host.lima.internal:55433/items/schema"}}'; echo
perl -e 'alarm 60; exec @ARGV' claude mcp list 2>&1 | grep ` + service.ID + `
cat ~/.claude.json ~/.codex/config.toml ~/.aiofcage/env; env`
	out := run(script)
	t.Log("已采集沙箱查询、拒绝结果、CLI 连接及配置，原始配置不写入测试日志")
	if !strings.Contains(out, "alpha,beta,gamma") {
		t.Error("query through the gatekeeper failed")
	}
	if !strings.Contains(out, "只读") {
		t.Error("writes must be refused")
	}
	if !strings.Contains(out, "Connected") {
		t.Error("Claude Code did not connect to the database MCP")
	}
	if !strings.Contains(out, "items/schema") || !strings.Contains(out, "column_name") {
		t.Error("资源发现或表结构读取失败")
	}
	if strings.Contains(out, password) {
		t.Fatal("the database password is visible inside the sandbox")
	}
	// 直接从沙箱调用网关，证明拒绝依赖服务端策略，而非模型或 Harness 的行为。
	callTool := func(sql string) (bool, string) {
		t.Helper()
		request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 10, "method": "tools/call",
			"params": map[string]any{"name": "query", "arguments": map[string]string{"sql": sql}}})
		probe := `import os,json,urllib.request
u="http://gatekeeper:8890/mcp/` + service.ID + `"
h={"Authorization":"Bearer "+os.environ["` + token + `"],"Content-Type":"application/json"}
def send(body):
 r=urllib.request.urlopen(urllib.request.Request(u,data=body,headers=h),timeout=20)
 return r, json.load(r)
r,_=send(b'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"security-regression","version":"1"}}}')
h["Mcp-Session-Id"]=r.headers["Mcp-Session-Id"]
_,reply=send(__import__('base64').b64decode('` + base64.StdEncoding.EncodeToString(request) + `'))
print(json.dumps(reply,ensure_ascii=False))
urllib.request.urlopen(urllib.request.Request(u,headers=h,method="DELETE"),timeout=10).close()`
		var proof bytes.Buffer
		if err := manager.Exec(ctx, view.ID, false, nil, &proof, &proof, "python3", "-c", probe); err != nil {
			t.Fatalf("越权回归执行失败：%v", err)
		}
		var reply struct {
			Result *struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		if json.Unmarshal(bytes.TrimSpace(proof.Bytes()), &reply) != nil || reply.Result == nil {
			t.Fatalf("工具结果无效：%s", proof.String())
		}
		return reply.Result.IsError, proof.String()
	}
	for _, sql := range []string{
		"SELECT pg_read_file('/etc/hostname')", "SELECT * FROM pg_ls_dir('/')",
		"COPY (SELECT 'noop') TO PROGRAM 'true'", "COMMIT; DELETE FROM items",
		"SELECT 1; COMMIT; SET TRANSACTION READ WRITE; DELETE FROM items",
		"WITH gone AS (DELETE FROM items RETURNING *) SELECT * FROM gone",
		"SELECT set_config('transaction_read_only','off',false)",
	} {
		if failed, result := callTool(sql); !failed {
			t.Fatalf("越权 SQL 未拒绝：%s，结果：%s", sql, result)
		}
		t.Logf("已拒绝：%s", sql)
	}
	for _, permission := range []struct{ grant, revoke string }{
		{"GRANT INSERT ON items TO e2e_reader", "REVOKE INSERT ON items FROM e2e_reader"},
		{"CREATE ROLE e2e_danger CREATEDB NOLOGIN; GRANT e2e_danger TO e2e_reader", "REVOKE e2e_danger FROM e2e_reader"},
	} {
		if out, err := docker("exec", db, "psql", "-U", "e2e", "-d", "e2e", "-v", "ON_ERROR_STOP=1", "-c", permission.grant); err != nil {
			t.Fatalf("配置权限回归失败：%v %s", err, out)
		}
		if failed, result := callTool("SELECT count(*) FROM items"); !failed || !strings.Contains(result, "权限过高") {
			t.Fatalf("已有连接未发现新增写权限或管理角色：%s", result)
		}
		if out, err := docker("exec", db, "psql", "-U", "e2e", "-d", "e2e", "-c", permission.revoke); err != nil {
			t.Fatalf("恢复权限失败：%v %s", err, out)
		}
	}
	if failed, result := callTool("SELECT count(*) FROM items"); failed {
		t.Fatalf("权限恢复后正常查询不可用：%s", result)
	}
	if out, err := docker("exec", db, "psql", "-U", "e2e", "-d", "e2e", "-tAc", "SELECT count(*) FROM items"); err != nil || strings.TrimSpace(string(out)) != "3" {
		t.Fatal("越权测试后数据发生变化")
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
		if r.Status == checkProblem || (r.ID == "db-direct" && r.Skipped) {
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
