package runtime

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shaoxier/AIofCage/internal/connector"
	"github.com/shaoxier/AIofCage/internal/gatekeeper"
	"github.com/shaoxier/AIofCage/internal/mcp"
	"github.com/shaoxier/AIofCage/internal/sandbox"
)

const (
	roleLabel        = "io.aiofcage.role"
	connectorRole    = "connector"
	connectorPort    = "8080"
	connectorUser    = "1000:1000"
	launchPath       = "/run/aiofcage/launch.json"
	connectorCommand = "/usr/local/bin/aiofcage-connector"
)

// connectorName is the container of one connector service in one sandbox.
func connectorName(sandboxID, serviceID string) string {
	return "aiofcage-" + sandboxID + "-mcp-" + serviceID
}

func connectorServices(services map[string]mcp.Service) []mcp.Service {
	var result []mcp.Service
	for _, id := range sortedKeys(services) {
		if services[id].IsConnector() {
			result = append(result, services[id])
		}
	}
	return result
}

// listConnectors returns the names of a sandbox's connector containers, running or not.
func (m *Manager) listConnectors(ctx context.Context, sandboxID string) ([]string, error) {
	out, err := m.docker.run(ctx, 15*time.Second, nil, "ps", "--all", "--format", "{{.Names}}",
		"--filter", "label="+labelKey+"="+sandboxID, "--filter", "label="+roleLabel+"="+connectorRole)
	if err != nil {
		return nil, err
	}
	names := strings.Fields(string(out))
	sort.Strings(names)
	return names, nil
}

// syncConnectors makes a sandbox's connector containers match its enabled connector services,
// delivers each one its secret, and returns the gatekeeper routes to them. Connectors sit only on
// the sandbox's egress network: the gatekeeper can reach them, the agent cannot.
func (m *Manager) syncConnectors(ctx context.Context, sbx sandbox.Sandbox, services map[string]mcp.Service) ([]gatekeeper.Connector, error) {
	wanted := connectorServices(services)
	keep := map[string]bool{}
	for _, service := range wanted {
		keep[connectorName(sbx.ID, service.ID)] = true
	}
	existing, err := m.listConnectors(ctx, sbx.ID)
	if err != nil {
		return nil, err
	}
	for _, name := range existing {
		if !keep[name] {
			if _, err := m.docker.run(ctx, 30*time.Second, nil, "rm", "--force", name); err != nil {
				return nil, err
			}
			m.mu.Lock()
			delete(m.connectorTokens, name)
			m.mu.Unlock()
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	build, err := connectorContext()
	if err != nil {
		return nil, err
	}
	if err := m.ensureImage(ctx, build, func(message string) { m.progress(sbx.ID, message) }); err != nil {
		return nil, err
	}
	n := names(sbx.ID)
	var routes []gatekeeper.Connector
	for _, service := range wanted {
		m.progress(sbx.ID, "启动连接器「"+service.Name+"」")
		name := connectorName(sbx.ID, service.ID)
		if err := m.ensureConnector(ctx, sbx.ID, name, build.tag, n.egress); err != nil {
			return nil, err
		}
		secret, err := m.vault.Read(ctx, service.KeyRef)
		if err != nil {
			return nil, fmt.Errorf("MCP 服务「%s」的密码不可用：%w", service.Name, err)
		}
		command, redact, err := service.Launch(string(secret))
		if err != nil {
			return nil, err
		}
		token, err := m.connectorToken(name)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(connector.Launch{Token: token, Command: command, Redact: redact})
		if err != nil {
			return nil, err
		}
		// The secret travels on stdin into a tmpfs file the connector reads once and removes; it
		// never appears in docker inspect, environment variables or arguments.
		if _, err := m.docker.run(ctx, 30*time.Second, payload, "exec", "--interactive", "--user", connectorUser,
			name, connectorCommand, "deliver", launchPath); err != nil {
			return nil, fmt.Errorf("向连接器「%s」投递配置失败：%w", service.Name, err)
		}
		ip, err := m.containerIP(ctx, name, n.egress)
		if err != nil {
			return nil, err
		}
		placeholder := sbx.Placeholders[mcp.PlaceholderKey(service.ID)]
		if placeholder == "" {
			return nil, fmt.Errorf("MCP 服务「%s」缺少占位符", service.Name)
		}
		routes = append(routes, gatekeeper.Connector{ID: service.ID, Placeholder: placeholder,
			Upstream: "http://" + ip + ":" + connectorPort, Token: token, Tools: service.Tools()})
	}
	return routes, nil
}

func (m *Manager) ensureConnector(ctx context.Context, sandboxID, name, image, network string) error {
	info := m.container(ctx, name)
	if info.exists && (info.image != image || info.spec != specVersion) {
		if _, err := m.docker.run(ctx, 30*time.Second, nil, "rm", "--force", name); err != nil {
			return err
		}
		info.exists = false
		m.mu.Lock()
		delete(m.connectorTokens, name)
		m.mu.Unlock()
	}
	if !info.exists {
		if _, err := m.docker.run(ctx, 30*time.Second, nil, "create", "--name", name,
			"--label", labelKey+"="+sandboxID, "--label", specLabel+"="+specVersion, "--label", roleLabel+"="+connectorRole,
			"--network", network, "--read-only", "--init",
			"--tmpfs", "/run/aiofcage:uid=1000,gid=1000,mode=0700,size=1m",
			"--tmpfs", "/tmp:uid=1000,gid=1000,mode=1777,size=64m",
			"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
			"--memory", "512m", "--cpus", "1", "--pids-limit", "256", image); err != nil {
			return err
		}
	}
	if !info.running {
		// A fresh start empties the tmpfs, so the connector waits for a new launch file.
		if _, err := m.docker.run(ctx, 30*time.Second, nil, "start", name); err != nil {
			return err
		}
	}
	return nil
}

// connectorToken returns the gatekeeper→connector credential for a container, creating one the
// first time. Tokens live only in memory here, in the gatekeeper and in the connector.
func (m *Manager) connectorToken(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token := m.connectorTokens[name]; token != "" {
		return token, nil
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	m.connectorTokens[name] = token
	return token, nil
}

func (m *Manager) containerIP(ctx context.Context, name, network string) (string, error) {
	out, err := m.docker.run(ctx, 15*time.Second, nil, "container", "inspect", "--format",
		`{{with index .NetworkSettings.Networks "`+network+`"}}{{.IPAddress}}{{end}}`, name)
	ip := strings.TrimSpace(string(out))
	if err != nil || ip == "" {
		return "", fmt.Errorf("无法读取连接器 %s 的地址", name)
	}
	return ip, nil
}
