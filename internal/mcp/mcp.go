// Package mcp describes MCP services a sandbox may use without holding their secrets.
//
// First version: remote streamable-HTTP MCP servers with a static bearer token (e.g. GitHub's
// official remote MCP). The sandbox only gets a placeholder; the gatekeeper injects the real token
// for the service's exact host and path. Tool-level authorization through a protocol gateway,
// OAuth services and password-based connectors remain on the roadmap (see docs).
package mcp

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shaoxier/AIofCage/internal/policy"
)

type Service struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
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
	if _, _, err := s.Endpoint(); err != nil {
		return err
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(s.KeyRef) {
		return errors.New("钥匙串条目名称格式无效")
	}
	return nil
}

func (s Service) Endpoint() (string, string, error) {
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
