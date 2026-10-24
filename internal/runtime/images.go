package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/shaoxier/AIofCage/internal/mcp"
	"github.com/shaoxier/AIofCage/internal/session"
)

//go:embed assets
var assets embed.FS

const (
	gatekeeperBinary = "assets/gatekeeper/gatekeeper-linux-arm64"
	connectorBinary  = "assets/connector/connector-linux-arm64"
)

// AgentSpec pins the npm package installed for an agent. Versions are fixed so that images are
// reproducible and match what was verified.
type AgentSpec struct {
	Agent   session.Agent
	Name    string
	Package string
	Version string
	Command string
	// Scripts allows the package's install scripts. Claude Code's postinstall only copies its
	// native binary out of the platform optional dependency (no network); others skip scripts.
	Scripts bool
}

var agentCatalog = []AgentSpec{
	{session.Codex, "Codex", "@openai/codex", "0.164.0", "codex", false},
	{session.ClaudeCode, "Claude Code", "@anthropic-ai/claude-code", "2.1.309", "claude", true},
	{session.DSH, "dsh", "@deepseek-ai/dsh", "0.2.1-rc.2", "dsh", false},
}

func Spec(agent session.Agent) (AgentSpec, bool) {
	for _, spec := range agentCatalog {
		if spec.Agent == agent {
			return spec, true
		}
	}
	return AgentSpec{}, false
}

// Catalog lists the agents that can be installed in a sandbox.
func Catalog() []AgentSpec { return append([]AgentSpec(nil), agentCatalog...) }

type buildContext struct {
	tag   string
	files map[string][]byte
}

func digest(files map[string][]byte, names ...string) string {
	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(files[name]))
		h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func gatekeeperContext() (buildContext, error) {
	binary, err := assets.ReadFile(gatekeeperBinary)
	if err != nil {
		return buildContext{}, errors.New("门卫二进制没有打包进程序，请先运行 make gatekeeper-linux 再构建")
	}
	dockerfile, err := assets.ReadFile("assets/gatekeeper/Dockerfile")
	if err != nil {
		return buildContext{}, err
	}
	files := map[string][]byte{"Dockerfile": dockerfile, "gatekeeper": binary}
	return buildContext{tag: "aiofcage/gatekeeper:" + digest(files, "Dockerfile", "gatekeeper"), files: files}, nil
}

// connectorContext builds one image with every catalog MCP server, shared by all connectors.
func connectorContext() (buildContext, error) {
	binary, err := assets.ReadFile(connectorBinary)
	if err != nil {
		return buildContext{}, errors.New("连接器二进制没有打包进程序，请先运行 make connector-linux 再构建")
	}
	text, err := assets.ReadFile("assets/connector/Dockerfile.tmpl")
	if err != nil {
		return buildContext{}, err
	}
	var dockerfile bytes.Buffer
	if err := template.Must(template.New("connector").Parse(string(text))).Execute(&dockerfile,
		struct{ Connectors []mcp.ConnectorSpec }{mcp.Connectors()}); err != nil {
		return buildContext{}, err
	}
	files := map[string][]byte{"Dockerfile": dockerfile.Bytes(), "connector": binary}
	return buildContext{tag: "aiofcage/connector:" + digest(files, "Dockerfile", "connector"), files: files}, nil
}

func agentContext(agents []session.Agent) (buildContext, error) {
	var specs []AgentSpec
	for _, spec := range agentCatalog { // catalog order keeps shared layers cacheable
		for _, agent := range agents {
			if agent == spec.Agent {
				specs = append(specs, spec)
			}
		}
	}
	text, err := assets.ReadFile("assets/agent/Dockerfile.tmpl")
	if err != nil {
		return buildContext{}, err
	}
	var dockerfile bytes.Buffer
	if err := template.Must(template.New("agent").Parse(string(text))).Execute(&dockerfile, struct{ Agents []AgentSpec }{specs}); err != nil {
		return buildContext{}, err
	}
	files := map[string][]byte{"Dockerfile": dockerfile.Bytes()}
	for _, name := range []string{"aiofcage-init.py", "aiofcage-run", "bashrc"} {
		data, err := assets.ReadFile("assets/agent/" + name)
		if err != nil {
			return buildContext{}, err
		}
		files[name] = data
	}
	return buildContext{tag: "aiofcage/agent:" + digest(files, "Dockerfile", "aiofcage-init.py", "aiofcage-run", "bashrc"), files: files}, nil
}

// ensureImage builds the image inside the dedicated VM unless an image with the same content
// digest already exists.
func (m *Manager) ensureImage(ctx context.Context, build buildContext, progress func(string)) error {
	if _, err := m.docker.run(ctx, 15*time.Second, nil, "image", "inspect", build.tag); err == nil {
		return nil
	}
	dir, err := os.MkdirTemp("", "aiofcage-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, data := range build.files {
		mode := fs.FileMode(0o644)
		if name == "gatekeeper" || name == "connector" || name == "aiofcage-run" || name == "aiofcage-init.py" {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, mode); err != nil {
			return err
		}
	}
	if progress != nil {
		progress("正在构建镜像 " + build.tag + "（首次需要几分钟）")
	}
	if _, err := m.docker.run(ctx, 30*time.Minute, nil, "build", "--platform", "linux/arm64",
		"--label", "io.aiofcage.managed=true", "--tag", build.tag, dir); err != nil {
		return fmt.Errorf("构建镜像失败：%w", err)
	}
	return nil
}
