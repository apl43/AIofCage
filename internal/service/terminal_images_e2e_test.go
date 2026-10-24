package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/session"
	"github.com/shaoxier/AIofCage/internal/store"
)

// 真实容器附件验证只使用临时空白沙箱，不读取凭据、不调用模型。
func TestTerminalImagesEndToEnd(t *testing.T) {
	if os.Getenv("AIOFCAGE_TERMINAL_E2E") == "" {
		t.Skip("设置 AIOFCAGE_TERMINAL_E2E=1 验证真实沙箱图片附件")
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := runtime.NewManager(st, memoryVault{})
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(st, memoryVault{}, manager, nil, nil)
	s, err := app.CreateSandbox(CreateSandboxRequest{Name: "uat-images-" + time.Now().Format("150405"), ProjectMode: ProjectNew})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := app.DeleteSandbox(s.ID, false); err != nil {
			t.Errorf("清理失败：%v", err)
		}
		_ = os.RemoveAll(s.ProjectPath)
	}()
	if err := app.StartSandbox(s.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	execPython := func(script string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		command := append([]string{"python3", "-c", script}, args...)
		if err := manager.Exec(ctx, s.ID, false, nil, &out, &out, command...); err != nil {
			t.Fatalf("容器检查失败：%v %s", err, out.String())
		}
		return strings.TrimSpace(out.String())
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	term := &terminal{id: "t-imagefixture", sandboxID: s.ID, agent: session.ClaudeCode, pty: writer}
	app.terms.items = map[string]*terminal{term.id: term}
	data := terminalTestPNG(t)
	input := []TerminalImage{{Data: base64.StdEncoding.EncodeToString(data)}}
	if err := app.SendTerminalMessage(term.id, "第一行\n第二行", input); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2048)
	n, err := reader.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	message := string(buffer[:n])
	if !strings.Contains(message, "第一行\n第二行") || strings.Count(message, "\r") != 1 {
		t.Fatal("消息没有完整提交")
	}
	start := strings.Index(message, "/home/agent/.aiofcage/attachments/")
	if start < 0 {
		t.Fatal("消息缺少沙箱图片路径")
	}
	path := strings.TrimSuffix(message[start:], "\x1b[201~\r")
	probe := `import os,base64,json,stat,sys
p=sys.argv[1]
print(json.dumps({"data":base64.b64encode(open(p,"rb").read()).decode(),"mode":stat.S_IMODE(os.stat(p).st_mode),"directory_mode":stat.S_IMODE(os.stat(os.path.dirname(p)).st_mode)}))`
	var result struct {
		Data          string `json:"data"`
		Mode          int    `json:"mode"`
		DirectoryMode int    `json:"directory_mode"`
	}
	if err := json.Unmarshal([]byte(execPython(probe, path)), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data != input[0].Data || result.Mode != 0o600 || result.DirectoryMode != 0o700 {
		t.Fatal("图片内容或权限不正确")
	}
	entries, _ := os.ReadDir(s.ProjectPath)
	if len(entries) != 0 {
		t.Fatal("附件写入了项目目录")
	}
	count := `from pathlib import Path; print(len(list(Path('/home/agent/.aiofcage/attachments').glob('*/*'))))`
	if execPython(count) != "1" {
		t.Fatal("附件数量不正确")
	}
	// PTY 失败会删除本次新保存的附件，成功提交的旧附件仍可被工具读取。
	_ = writer.Close()
	if err := app.SendTerminalMessage(term.id, "应失败", input); err == nil {
		t.Fatal("关闭的写端不应发送成功")
	}
	if execPython(count) != "1" {
		t.Fatal("失败发送遗留了附件，或误删了已发送附件")
	}
	// 用户可写的 home 可能含有符号链接，附件目录不能借此覆盖项目文件。
	execPython(`import os,shutil,sys; shutil.rmtree('/home/agent/.aiofcage/attachments'); os.symlink(sys.argv[1],'/home/agent/.aiofcage/attachments')`, s.ProjectPath)
	if _, err := manager.WriteTerminalImages(ctx, s.ID, "ABCDEFGHIJKL1234567890ABCD", []runtime.TerminalAttachment{{Data: data, Extension: "png"}}); err == nil {
		t.Fatal("附件写入跟随了符号链接")
	}
	entries, _ = os.ReadDir(s.ProjectPath)
	if len(entries) != 0 {
		t.Fatal("符号链接导致项目被写入")
	}
	t.Log("真实容器图片字节、权限、项目隔离、失败清理及符号链接防护通过；未调用模型")
}
