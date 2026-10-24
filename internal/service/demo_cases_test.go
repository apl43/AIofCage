package service

import (
	"context"
	"testing"

	"github.com/shaoxier/AIofCage/internal/mcp"
)

func TestBuiltinMCPNeverUsesCredentials(t *testing.T) {
	a, vault, st := newApp(t)
	view, err := a.SaveMCPService(SaveMCPServiceRequest{Name: "业务案例", Kind: string(mcp.KindDemo), URL: "builtin://sales", Preset: "demo-sales"})
	if err != nil || view.KeySaved || view.KeyRef != "" || len(vault) != 0 {
		t.Fatalf("内置案例使用了凭据: %v %+v", err, view)
	}
	s, _ := st.MCPService(view.ID)
	command, redact, err := s.Launch("")
	if err != nil || len(command) != 3 || command[2] != "sales" || len(redact) != 0 || len(s.Tools()) != 3 {
		t.Fatalf("启动配置错误: %v %v", err, command)
	}
	if _, err := a.SaveMCPService(SaveMCPServiceRequest{ID: s.ID, Name: s.Name, URL: s.URL, Token: "unexpected"}); err == nil {
		t.Fatal("内置案例接收了密钥")
	}
	if _, err := a.SaveMCPService(SaveMCPServiceRequest{Name: "bad", Kind: string(mcp.KindDemo), URL: "builtin://../../secret"}); err == nil {
		t.Fatal("案例路径越界")
	}
	_ = vault.Put(context.Background(), "unrelated", []byte("keep"))
	if err := a.DeleteMCPService(s.ID); err != nil || len(vault) != 1 {
		t.Fatal("删除内置案例误删凭据", err)
	}
}
