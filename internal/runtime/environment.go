package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/shaoxier/AIofCage/internal/sandbox"
)

// Environment manages the dedicated Colima VM. It never touches the user's default profile or
// switches the default Docker context.
type Environment struct {
	colima  runner
	limactl string
	docker  runner
}

type VMInfo struct {
	CPUs   int    `json:"cpus"`
	Memory uint64 `json:"memory"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Arch   string `json:"arch"`
	Disk   int64  `json:"disk"`
}

func NewEnvironment() (*Environment, error) {
	colima, err := findTool("colima")
	if err != nil {
		return nil, err
	}
	limactl, err := findTool("limactl")
	if err != nil {
		return nil, err
	}
	docker, err := findTool("docker")
	if err != nil {
		return nil, err
	}
	return &Environment{
		colima:  runner{path: colima},
		limactl: limactl,
		docker:  runner{path: docker, args: []string{"--context", DockerContext}},
	}, nil
}

func (e *Environment) vm(ctx context.Context) (*VMInfo, error) {
	out, err := e.colima.run(ctx, 30*time.Second, nil, "list", "--json")
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		var info VMInfo
		if json.Unmarshal(scanner.Bytes(), &info) == nil && info.Name == ColimaProfile {
			return &info, nil
		}
	}
	return nil, nil
}

// Ensure creates or starts the dedicated VM and then verifies its sharing boundary.
func (e *Environment) Ensure(ctx context.Context) error {
	if goruntime.GOOS != "darwin" || goruntime.GOARCH != "arm64" {
		return errors.New("仅支持 Apple Silicon 的 macOS")
	}
	workspace := sandbox.Workspace()
	if info, err := os.Lstat(workspace); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("~/CageWorkspace 不能是符号链接")
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fmt.Errorf("无法创建工作区：%w", err)
	}
	vm, err := e.vm(ctx)
	if err != nil {
		return err
	}
	switch {
	case vm == nil:
		// Same flags as the validation package: no user template, no SSH config or agent
		// forwarding, no context switch, only the workspace shared.
		_, err = e.colima.run(ctx, 20*time.Minute, nil, "start", ColimaProfile,
			"--arch", "aarch64", "--vm-type", "vz", "--mount-type", "virtiofs",
			"--cpu", "4", "--memory", "6", "--disk", "40", "--mount", workspace+":w",
			"--activate=false", "--ssh-config=false", "--ssh-agent=false", "--template=false",
			"--kubernetes=false", "--runtime", "docker", "--binfmt=false")
	case vm.Status != "Running":
		_, err = e.colima.run(ctx, 10*time.Minute, nil, "start", ColimaProfile, "--activate=false", "--ssh-config=false")
	}
	if err != nil {
		return fmt.Errorf("启动专用虚拟机失败：%w", err)
	}
	return e.Verify(ctx)
}

// Verify checks the actual VM configuration and mounts rather than trusting our own flags.
func (e *Environment) Verify(ctx context.Context) error {
	vm, err := e.vm(ctx)
	if err != nil {
		return err
	}
	if vm == nil || vm.Status != "Running" {
		return errors.New("专用虚拟机 aiofcage 未运行")
	}
	if vm.Arch != "aarch64" && vm.Arch != "arm64" {
		return errors.New("专用虚拟机必须是 arm64")
	}
	workspace, err := filepath.EvalSymlinks(sandbox.Workspace())
	if err != nil {
		return errors.New("工作区不存在")
	}
	home, _ := os.UserHomeDir()
	cmd := exec.CommandContext(ctx, e.limactl, "list", "--all-fields", "--json", "colima-"+ColimaProfile)
	cmd.Env = append(childEnv(), "LIMA_HOME="+filepath.Join(home, ".colima", "_lima"))
	out, err := cmd.Output()
	if err != nil {
		return errors.New("无法读取虚拟机配置")
	}
	var lima struct {
		Config struct {
			VMType    string `json:"vmType"`
			MountType string `json:"mountType"`
			Mounts    []struct {
				Location string `json:"location"`
				Writable bool   `json:"writable"`
			} `json:"mounts"`
			SSH struct {
				ForwardAgent bool `json:"forwardAgent"`
			} `json:"ssh"`
		} `json:"config"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &lima) != nil {
		return errors.New("虚拟机配置格式无法识别，拒绝继续")
	}
	mounts := lima.Config.Mounts
	if len(mounts) != 1 || !mounts[0].Writable || expand(mounts[0].Location, home) != workspace {
		return errors.New("虚拟机共享目录不是唯一的 ~/CageWorkspace，拒绝继续")
	}
	if lima.Config.VMType != "vz" || lima.Config.MountType != "virtiofs" || lima.Config.SSH.ForwardAgent {
		return errors.New("虚拟机配置不符合 vz + virtiofs、且不转发 SSH agent 的要求")
	}
	procMounts, err := e.colima.run(ctx, 30*time.Second, nil, "ssh", "--profile", ColimaProfile, "--", "cat", "/proc/mounts")
	if err != nil {
		return err
	}
	var shared []string
	for _, line := range strings.Split(string(procMounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && (fields[2] == "virtiofs" || fields[2] == "9p" || fields[2] == "fuse.sshfs") {
			shared = append(shared, strings.ReplaceAll(fields[1], `\040`, " "))
		}
	}
	if len(shared) != 1 || shared[0] != workspace {
		return errors.New("虚拟机中实际挂载的共享目录与白名单不一致")
	}
	out, err = e.docker.run(ctx, 15*time.Second, nil, "context", "inspect", DockerContext)
	if err != nil {
		return fmt.Errorf("Docker context %s 不存在", DockerContext)
	}
	var contexts []struct {
		Endpoints struct {
			Docker struct {
				Host string `json:"Host"`
			} `json:"docker"`
		} `json:"Endpoints"`
	}
	expected := "unix://" + filepath.Join(home, ".colima", ColimaProfile, "docker.sock")
	if json.Unmarshal(out, &contexts) != nil || len(contexts) != 1 || contexts[0].Endpoints.Docker.Host != expected {
		return errors.New("同名 Docker context 没有指向专用虚拟机")
	}
	return nil
}

func expand(location, home string) string {
	if strings.HasPrefix(location, "~/") {
		location = filepath.Join(home, location[2:])
	}
	resolved, err := filepath.EvalSymlinks(location)
	if err != nil {
		return location
	}
	return resolved
}

// Inspect reports readiness without changing anything.
func (e *Environment) Inspect(ctx context.Context) Report {
	checks := []Check{{ID: "platform", Name: "macOS Apple Silicon", Status: "detected", Detail: goruntime.GOOS + "/" + goruntime.GOARCH}}
	if goruntime.GOOS != "darwin" || goruntime.GOARCH != "arm64" {
		checks[0].Status = "unsupported"
	}
	vm, err := e.vm(ctx)
	switch {
	case err != nil:
		checks = append(checks, Check{ID: "vm", Name: "专用虚拟机", Status: "error", Detail: err.Error()})
	case vm == nil:
		checks = append(checks, Check{ID: "vm", Name: "专用虚拟机", Status: "missing", Detail: "尚未创建，首次启动沙箱时自动创建"})
	case vm.Status != "Running":
		checks = append(checks, Check{ID: "vm", Name: "专用虚拟机", Status: "stopped", Detail: "已创建，启动沙箱时自动启动"})
	default:
		status, detail := "ready", "运行中，只共享 ~/CageWorkspace"
		if err := e.Verify(ctx); err != nil {
			status, detail = "error", err.Error()
		}
		checks = append(checks, Check{ID: "vm", Name: "专用虚拟机", Status: status, Detail: detail})
	}
	ready := true
	for _, check := range checks {
		if check.Status != "ready" && check.Status != "detected" {
			ready = false
		}
	}
	return Report{Ready: ready, Checks: checks, VM: vm}
}
