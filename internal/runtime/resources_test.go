package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaoxier/AIofCage/internal/store"
)

func TestResourcesPreserveActualAndRejectShrink(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	vm := &VMInfo{CPUs: 2, Memory: 4 << 30, Disk: 60 << 30, Status: "Running"}
	e := &Environment{store: st}
	c, err := e.resources(vm)
	if err != nil || c != resourcesFromVM(vm) {
		t.Fatal("没有保留实际配置", err, c)
	}
	for _, bad := range []ResourceConfig{{0, 4, 60}, {9, 4, 60}, {2, 7, 60}, {2, 4, 40}, {2, 4, 2048}} {
		if validateResources(bad, 8, 8, vm) == nil {
			t.Fatal("允许无效或缩容配置", bad)
		}
	}
	if err := validateResources(ResourceConfig{2, 4, 80}, 8, 8, vm); err != nil {
		t.Fatal(err)
	}
}

func TestSuspendRefusesActiveContainers(t *testing.T) {
	dir := t.TempDir()
	colima := filepath.Join(dir, "colima")
	docker := filepath.Join(dir, "docker")
	if err := os.WriteFile(colima, []byte("#!/bin/sh\nprintf '%s\\n' '{\"name\":\"aiofcage\",\"status\":\"Running\",\"arch\":\"aarch64\",\"cpus\":4,\"memory\":6442450944,\"disk\":42949672960}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docker, []byte("#!/bin/sh\necho active-container\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{docker: runner{path: docker}, env: &Environment{colima: runner{path: colima}}}
	// 活跃容器检查先于停止动作和配置检查，不能用失败读数当作没有容器。
	if err := m.SuspendEnvironment(context.Background()); err == nil || !strings.Contains(err.Error(), "仍有运行容器") {
		t.Fatal("运行容器仍在时停止了环境", err)
	}
}

func TestEnvironmentResourceSettingsReadOnly(t *testing.T) {
	if os.Getenv("AIOFCAGE_RESOURCE_CHECK") != "1" {
		t.Skip("设置 AIOFCAGE_RESOURCE_CHECK=1 只读验证实际虚拟机及临时存储")
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewEnvironment(st)
	if err != nil {
		t.Fatal(err)
	}
	before := e.ResourceSettings(context.Background())
	if before.Error != "" || before.Actual == nil {
		t.Fatalf("读取失败: %+v", before)
	}
	c := before.Desired
	c.CPUs = 2
	if c.MemoryGiB == 6 {
		c.MemoryGiB = 4
	} else {
		c.MemoryGiB = 6
	}
	if err := e.SaveResources(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	after := e.ResourceSettings(context.Background())
	if after.Error != "" || !after.Pending || after.Desired != c || *after.Actual != *before.Actual {
		t.Fatalf("保存触发了运行环境变更或没有保留配置: %+v", after)
	}
	t.Logf("真实资源读取、临时配置保存和待应用标识通过；当前VM保持 %dCPU/%dGiB/%dGiB", before.Actual.CPUs, before.Actual.Memory>>30, before.Actual.Disk>>30)
}
