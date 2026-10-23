// Package runtime owns sandbox lifecycle. It must never fall back to host execution.
package runtime

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	"github.com/shaoxier/AIofCage/internal/session"
)

const (
	ColimaProfile = "aiofcage"
	DockerContext = "colima-aiofcage"
)

var ErrNotReady = errors.New("隔离运行时尚未实现；会话未启动，不会在宿主执行 Agent")

type Check struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type Report struct {
	Ready  bool    `json:"ready"`
	Checks []Check `json:"checks"`
}

type Backend interface {
	Inspect(context.Context) Report
	Start(context.Context, session.Session) error
	Stop(context.Context, string) error
}

// Unavailable reports prerequisites without creating VMs, containers, or networks.
type Unavailable struct{}

func NewUnavailable() *Unavailable { return &Unavailable{} }

func (*Unavailable) Inspect(context.Context) Report {
	platform := "unsupported"
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		platform = "detected"
	}
	checks := []Check{{ID: "platform", Name: "macOS Apple Silicon", Status: platform, Detail: runtime.GOOS + "/" + runtime.GOARCH}}
	for _, binary := range []string{"colima", "docker"} {
		status, detail := "missing", "未在 PATH 中找到，请按开发说明安装"
		if _, err := exec.LookPath(binary); err == nil {
			status, detail = "detected", "仅检测到命令；版本、服务与专用实例尚未验证"
		}
		checks = append(checks, Check{ID: binary, Name: binary, Status: status, Detail: detail})
	}
	checks = append(checks, Check{ID: "sandbox", Name: "专用沙箱运行时", Status: "not_implemented", Detail: "尚未实现 " + ColimaProfile + " 实例编排与安全自检"})
	return Report{Ready: false, Checks: checks}
}

func (*Unavailable) Start(context.Context, session.Session) error { return ErrNotReady }
func (*Unavailable) Stop(context.Context, string) error           { return ErrNotReady }
