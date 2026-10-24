// Package mcp describes MCP services a sandbox may use without holding their secrets.
//
// Remote streamable-HTTP servers authenticate with a bearer token: either a static token (e.g.
// GitHub's official remote MCP) or one obtained by an OAuth login on the host (e.g. Notion,
// Linear). The sandbox only gets a placeholder and the gatekeeper injects the real token for the
// service's exact host and path. Connector services (e.g. a PostgreSQL database): a stdio MCP
// server runs in a trusted connector container that holds the password; the agent reaches it
// only through the gatekeeper's MCP entrypoint, which checks the placeholder and the tool.
package mcp

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shaoxier/AIofCage/internal/democases"
	"github.com/shaoxier/AIofCage/internal/policy"
)

type Service struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is empty or "http" for remote HTTP services, otherwise a connector kind.
	Kind Kind `json:"kind,omitempty"`
	// URL is the service address for HTTP services and the connection URL without password for
	// connector services.
	URL       string    `json:"url"`
	KeyRef    string    `json:"key_ref"`
	Preset    string    `json:"preset,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

var nameIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// KeyRefFor is the Keychain item holding the service token.
func KeyRefFor(id string) string { return "aiofcage-mcp-" + id }

// SecretID is the gatekeeper secret ID; it lives in a separate namespace from provider IDs.
func SecretID(id string) string { return "mcp-" + id }

// PlaceholderKey is the key of this service's placeholder in sandbox.Placeholders.
func PlaceholderKey(id string) string { return "mcp:" + id }

// EnvVar is the variable carrying the placeholder for agents that read the token from the
// environment (Codex's bearer_token_env_var).
func EnvVar(id string) string {
	return "AIOFCAGE_MCP_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_TOKEN"
}

// ValidID accepts the IDs used as MCP server names in agent configs.
func ValidID(id string) bool { return nameIDPattern.MatchString(id) }

func (s Service) IsBuiltin() bool { return s.Kind == KindDemo }

func (s *Service) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.URL = strings.TrimSpace(s.URL)
}

func (s Service) Validate() error {
	if !ValidID(s.ID) {
		return errors.New("MCP 服务 ID 须以小写字母开头，只含小写字母、数字和连字符")
	}
	if s.Name == "" || len([]rune(s.Name)) > 60 {
		return errors.New("MCP 服务名称须为 1 至 60 个字符")
	}
	if s.IsBuiltin() {
		if !strings.HasPrefix(s.URL, "builtin://") || s.KeyRef != "" {
			return errors.New("内置案例不接受密钥或自定义地址")
		}
		_, err := democases.Find(strings.TrimPrefix(s.URL, "builtin://"))
		return err
	}
	if s.IsConnector() {
		if _, ok := ConnectorFor(s.Kind); !ok {
			return errors.New("不支持的 MCP 服务类型")
		}
		_, password, err := SplitDatabaseURL(s.Kind, s.URL)
		if err != nil {
			return err
		}
		if password != "" {
			return errors.New("连接串里不能保存密码")
		}
	} else if s.Kind != "" && s.Kind != KindHTTP && s.Kind != KindOAuth {
		return errors.New("不支持的 MCP 服务类型")
	} else if _, _, err := s.Endpoint(); err != nil {
		return err
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(s.KeyRef) {
		return errors.New("钥匙串条目名称格式无效")
	}
	return nil
}

func (s Service) Endpoint() (string, string, error) {
	if s.IsConnector() {
		return "", "", errors.New("连接器服务没有远程地址")
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Port() != "" && u.Port() != "443") {
		return "", "", errors.New("MCP 地址须为 https:// 开头的网址，不带查询参数")
	}
	host := strings.ToLower(u.Hostname())
	if !policy.ValidHost(host, false) {
		return "", "", errors.New("MCP 地址的域名无效（不支持 IP 地址）")
	}
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	if !policy.ValidPath(p) {
		return "", "", errors.New("MCP 地址的路径无效")
	}
	return host, p, nil
}

// Rules allows the streamable-HTTP methods on exactly the service endpoint (and sub-paths, which
// some servers use for sessions), with the token injected.
func (s Service) Rules() []policy.Rule {
	if s.IsConnector() {
		return nil // reached through the MCP entrypoint, never through the proxy
	}
	host, p, err := s.Endpoint()
	if err != nil {
		return nil
	}
	base := strings.TrimSuffix(p, "/")
	pattern := base + "/*"
	if base == "" {
		pattern = "/*"
	}
	return []policy.Rule{{ID: "mcp-" + s.ID, Effect: policy.Allow, Host: host,
		Methods: []string{"GET", "POST", "DELETE"}, Path: pattern, SecretRef: SecretID(s.ID)}}
}

type Preset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind Kind   `json:"kind,omitempty"`
	URL  string `json:"url"`
	Note string `json:"note,omitempty"`
	Docs string `json:"docs,omitempty"`
}

//go:embed presets.json
var presetsJSON []byte

func Presets() []Preset {
	var presets []Preset
	if err := json.Unmarshal(presetsJSON, &presets); err != nil {
		panic("mcp presets.json 格式错误: " + err.Error())
	}
	return presets
}
