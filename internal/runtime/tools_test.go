package runtime

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeMachOTool(t *testing.T, path string, cpus ...macho.Cpu) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if len(cpus) > 1 {
		binary.Write(&data, binary.BigEndian, []uint32{macho.MagicFat, uint32(len(cpus))})
		for i, cpu := range cpus {
			binary.Write(&data, binary.BigEndian, []uint32{uint32(cpu), 0, uint32(8 + len(cpus)*20 + i*32), 32, 0})
		}
	}
	for _, cpu := range cpus {
		binary.Write(&data, binary.LittleEndian, []uint32{macho.Magic64, uint32(cpu), 0, uint32(macho.TypeExec), 0, 0, 0, 0})
	}
	if err := os.WriteFile(path, data.Bytes(), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindRuntimeToolsSkipsIntelInstallations(t *testing.T) {
	root := t.TempDir()
	intel, native := filepath.Join(root, "intel"), filepath.Join(root, "native")
	for _, name := range []string{"colima", "limactl"} {
		writeMachOTool(t, filepath.Join(intel, name), macho.CpuAmd64)
		writeMachOTool(t, filepath.Join(native, name), macho.CpuArm64)
		got, err := findToolIn(name, []string{intel, native}, true)
		if err != nil || got != filepath.Join(native, name) {
			t.Fatalf("%s picked incompatible dependency: path=%s err=%v", name, got, err)
		}
		if _, err := findToolIn(name, []string{intel}, true); !errors.Is(err, errToolArchitecture) {
			t.Fatalf("%s must report an architecture error: %v", name, err)
		}
	}
	// Docker can run through Rosetta and should not acquire Lima's restriction.
	writeMachOTool(t, filepath.Join(intel, "docker"), macho.CpuAmd64)
	if _, err := findToolIn("docker", []string{intel}, true); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeToolsAcceptUniversalBinariesAndWrappers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "limactl")
	writeMachOTool(t, path, macho.CpuAmd64, macho.CpuArm64)
	if got, err := findToolIn("limactl", []string{dir}, true); err != nil || got != path {
		t.Fatalf("universal binary rejected: %s %v", got, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := findToolIn("limactl", []string{dir}, true); err != nil {
		t.Fatal("executable wrapper rejected", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := findToolIn("limactl", []string{dir}, true); err == nil {
		t.Fatal("non-executable file accepted")
	}
}

func TestFinderToolSearchIncludesUserInstallations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	userBin := filepath.Join(home, ".local", "bin")
	for _, dir := range toolSearchDirs() {
		if dir == userBin {
			return
		}
	}
	t.Fatal("Finder launch cannot discover user-installed dependencies")
}

func TestColimaChildrenResolveTheSelectedNativeLima(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	writeMachOTool(t, filepath.Join(first, "colima"), macho.CpuArm64)
	writeMachOTool(t, filepath.Join(first, "limactl"), macho.CpuAmd64)
	writeMachOTool(t, filepath.Join(second, "limactl"), macho.CpuArm64)
	env := childEnvFor([]string{"HOME=" + root, "PATH=" + first, "HTTP_PROXY=http://proxy.invalid", "https_proxy=http://proxy.invalid", "DOCKER_HOST=other", "DOCKER_CONTEXT=other"}, []string{first, second, "/usr/bin", "/bin"}, true)
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasSuffix(strings.ToLower(key), "_proxy") || key == "DOCKER_HOST" || key == "DOCKER_CONTEXT" {
			t.Fatalf("child inherited an override: %s", key)
		}
	}
	cmd := exec.Command("/bin/sh", "-c", "command -v limactl")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != filepath.Join(second, "limactl") {
		t.Fatalf("Colima would launch the wrong Lima: %s %v", out, err)
	}
}
