package runtime

import (
	"errors"
	goruntime "runtime"
	"strings"
)

// CheckDependencies is read-only and deliberately does not require an existing VM.
func CheckDependencies() ([]Check, string) {
	return checkDependencies(goruntime.GOOS, goruntime.GOARCH, findTool)
}

func checkDependencies(goos, arch string, find func(string) (string, error)) ([]Check, string) {
	platform := Check{ID: "platform", Name: "macOS Apple Silicon", Status: "detected", Detail: goos + "/" + arch}
	if goos != "darwin" || arch != "arm64" {
		platform.Status = "unsupported"
		platform.Detail = "需要 Apple Silicon（M 系列芯片）的 Mac"
	}
	checks := []Check{platform}
	missing := map[string]bool{}
	incompatible := false
	for _, tool := range []struct{ id, name string }{{"colima", "Colima"}, {"docker", "Docker CLI"}, {"limactl", "Lima（Colima 依赖）"}} {
		check := Check{ID: tool.id, Name: tool.name, Status: "detected", Detail: "已安装"}
		if _, err := find(tool.id); err != nil {
			check.Status, check.Detail = "missing", "尚未安装"
			if errors.Is(err, errToolArchitecture) {
				check.Status, check.Detail = "unsupported", err.Error()
				incompatible = true
			}
			missing[tool.id] = true
		}
		checks = append(checks, check)
	}
	var packages []string
	if missing["colima"] {
		packages = append(packages, "colima")
	}
	if missing["docker"] {
		packages = append(packages, "docker")
	}
	if missing["limactl"] && !missing["colima"] {
		packages = append(packages, "lima")
	}
	command := ""
	if len(packages) > 0 {
		command = "brew install " + strings.Join(packages, " ")
		if incompatible {
			command = "/opt/homebrew/bin/" + command
		}
	}
	return checks, command
}
