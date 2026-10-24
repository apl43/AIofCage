package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/store"
)

// 单独验证终端生命周期，不读取用户凭据，也不发送模型请求。
func TestTerminalLifecycleEndToEnd(t *testing.T) {
	if os.Getenv("AIOFCAGE_TERMINAL_E2E") == "" {
		t.Skip("设置 AIOFCAGE_TERMINAL_E2E=1 验证真实空白沙箱终端")
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secrets := memoryVault{}
	manager, err := runtime.NewManager(st, secrets)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, secrets, manager, nil, nil)
	s, err := app.CreateSandbox(CreateSandboxRequest{Name: "uat-terminal-" + time.Now().Format("150405"), ProjectMode: ProjectNew})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		app.CloseAllTerminals()
		if err := app.DeleteSandbox(s.ID, false); err != nil {
			t.Errorf("清理沙箱失败：%v", err)
		}
		if err := os.RemoveAll(s.ProjectPath); err != nil {
			t.Errorf("清理测试目录失败：%v", err)
		}
	}()
	if err := app.StartSandbox(s.ID); err != nil {
		t.Fatal(err)
	}
	checkBuiltInTerminal(t, app, s.ID)
	if err := app.StartTerminal("t-beforeclose00", s.ID, "", 80, 24); err != nil {
		t.Fatal(err)
	}
	// 并发启动与应用退出必须有明确先后，退出后不能再留下会话。
	var starting sync.WaitGroup
	for i := range 4 {
		starting.Add(1)
		go func(i int) {
			defer starting.Done()
			if err := app.StartTerminal(fmt.Sprintf("t-race%08d", i), s.ID, "", 80, 24); err != nil && !strings.Contains(err.Error(), "应用正在退出") {
				t.Errorf("并发启动终端失败：%v", err)
			}
		}(i)
	}
	app.CloseAllTerminals()
	starting.Wait()
	if err := app.StartTerminal("t-afterclose00", s.ID, "", 80, 24); err == nil {
		t.Fatal("退出后仍可启动终端")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var output bytes.Buffer
	probe := `import pathlib
count=0
for path in pathlib.Path('/proc').glob('[0-9]*/environ'):
    try:
        count += any(part.startswith(b'AIOFCAGE_TERMINAL_ID=') for part in path.read_bytes().split(b'\0'))
    except OSError:
        pass
print(count)`
	if err := manager.Exec(ctx, s.ID, false, nil, &output, &output, "python3", "-c", probe); err != nil || strings.TrimSpace(output.String()) != "0" {
		t.Fatalf("退出后仍有终端会话进程：%v %s", err, output.String())
	}
	t.Log("PTY 输入输出、中文、缩放、退出码、子进程清理及退出竞态通过；未调用模型")
}
