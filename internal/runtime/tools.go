package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Apps launched from Finder get a minimal PATH, so look in the usual Homebrew and Docker
// locations explicitly. Child processes get the same directories prepended.
var toolDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}

func findTool(name string) (string, error) {
	for _, dir := range toolDirs {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("未找到 %s，请先安装", name)
}

// childEnv removes proxy and Docker selection variables so every command talks to the dedicated
// instance directly and nothing from the user's shell leaks into the VM.
func childEnv() []string {
	var env []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		lower := strings.ToLower(key)
		if strings.HasSuffix(lower, "_proxy") || lower == "docker_host" || lower == "docker_context" || key == "PATH" {
			continue
		}
		env = append(env, item)
	}
	path := strings.Join(toolDirs, ":")
	if current := os.Getenv("PATH"); current != "" {
		path += ":" + current
	}
	return append(env, "PATH="+path)
}

// commandError keeps only a short, fixed-size summary of stderr. Callers never pass secrets as
// arguments, and stdin is not echoed.
type commandError struct {
	tool   string
	action string
	detail string
}

func (e *commandError) Error() string {
	if e.detail == "" {
		return fmt.Sprintf("%s %s 失败", e.tool, e.action)
	}
	return fmt.Sprintf("%s %s 失败：%s", e.tool, e.action, e.detail)
}

func summarize(stderr []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if len(last) > 300 {
		last = last[:300] + "…"
	}
	return last
}

type runner struct {
	path string
	args []string // fixed leading arguments, e.g. --context
}

func (r runner) run(ctx context.Context, timeout time.Duration, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.path, append(append([]string{}, r.args...), args...)...)
	cmd.Env = childEnv()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		action := ""
		if len(args) > 0 {
			action = args[0]
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return stdout.Bytes(), &commandError{filepath.Base(r.path), action, "超时"}
		}
		return stdout.Bytes(), &commandError{filepath.Base(r.path), action, summarize(stderr.Bytes())}
	}
	return stdout.Bytes(), nil
}

// stream runs a command with caller-provided stdio, for interactive exec from the CLI.
func (r runner) stream(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, r.path, append(append([]string{}, r.args...), args...)...)
	cmd.Env = childEnv()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return cmd.Run()
}
