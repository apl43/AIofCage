// Package policy validates and evaluates network authorization rules. Deny always wins over allow,
// and anything not explicitly allowed is denied.
package policy

import (
	"errors"
	"fmt"
	"net/netip"
	"path"
	"regexp"
	"strings"
)

// Rule authorizes or denies HTTPS requests. Host is an exact lowercase name; deny rules may also use
// a "*.example.com" suffix wildcard. Path is exact, a prefix ending in "/*", or empty for any path.
type Rule struct {
	ID        string   `json:"id"`
	Effect    string   `json:"effect"`
	Host      string   `json:"host"`
	Port      uint16   `json:"port,omitempty"`
	Methods   []string `json:"methods,omitempty"`
	Path      string   `json:"path,omitempty"`
	SecretRef string   `json:"secret_ref,omitempty"`
}

type Snapshot struct {
	Version   uint64 `json:"version"`
	SessionID string `json:"session_id"`
	Rules     []Rule `json:"rules"`
}

type Request struct {
	Host   string
	Port   uint16
	Method string
	Path   string
}

type Decision struct {
	Allowed    bool
	ReasonCode string
	RuleID     string
	SecretRef  string
}

const (
	Allow = "allow"
	Deny  = "deny"

	// Only HTTPS through the MITM gatekeeper is supported in the first version.
	DefaultPort = 443
	maxRules    = 256
)

var (
	idPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	labelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	methods      = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "OPTIONS": true}
)

// DenyUnconfigured is returned before any snapshot has been activated.
func DenyUnconfigured() Decision {
	return Decision{Allowed: false, ReasonCode: "policy_not_configured"}
}

// ValidID reports whether value can be used as a rule, secret, or session identifier.
func ValidID(value string) bool { return idPattern.MatchString(value) }

// ValidHost accepts lowercase DNS names with at least one dot. IP literals are rejected so that
// authorization always refers to names the gatekeeper resolves and checks itself.
func ValidHost(host string, allowWildcard bool) bool {
	if allowWildcard && strings.HasPrefix(host, "*.") {
		host = host[2:]
	}
	if host == "" || len(host) > 253 || host != strings.ToLower(host) || !strings.Contains(host, ".") {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !labelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

// ValidPath accepts the request paths the gatekeeper is willing to evaluate: absolute, already
// clean, and without encoded or ambiguous characters.
func ValidPath(value string) bool {
	return strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsAny(value, "%?#\\")
}

func (s *Snapshot) Validate() error {
	if s.Version == 0 {
		return errors.New("policy version 必须大于 0")
	}
	if s.SessionID != "" && !ValidID(s.SessionID) {
		return errors.New("session_id 格式无效")
	}
	if len(s.Rules) > maxRules {
		return fmt.Errorf("规则不能超过 %d 条", maxRules)
	}
	seen := map[string]bool{}
	for i, rule := range s.Rules {
		if err := rule.validate(); err != nil {
			return fmt.Errorf("第 %d 条规则：%w", i+1, err)
		}
		if seen[rule.ID] {
			return fmt.Errorf("规则 ID 重复：%s", rule.ID)
		}
		seen[rule.ID] = true
	}
	return nil
}

func (r Rule) validate() error {
	if !ValidID(r.ID) {
		return errors.New("规则 ID 格式无效")
	}
	if r.Effect != Allow && r.Effect != Deny {
		return errors.New("effect 只能是 allow 或 deny")
	}
	if !ValidHost(r.Host, r.Effect == Deny) {
		return errors.New("host 须为小写域名；只有 deny 规则可用 *. 通配")
	}
	if r.Port != 0 && r.Port != DefaultPort {
		return errors.New("第一版只支持 443 端口")
	}
	if r.Effect == Allow && len(r.Methods) == 0 {
		return errors.New("allow 规则必须列出 HTTP 方法")
	}
	for _, method := range r.Methods {
		if !methods[method] {
			return fmt.Errorf("不支持的 HTTP 方法：%s", method)
		}
	}
	if r.Path != "" && r.Path != "/*" {
		base := strings.TrimSuffix(r.Path, "/*")
		if !ValidPath(base) || strings.Contains(base, "*") {
			return errors.New("path 须为规范化的绝对路径，前缀匹配以 /* 结尾")
		}
	}
	if r.SecretRef != "" && (r.Effect != Allow || !ValidID(r.SecretRef)) {
		return errors.New("secret_ref 只能用于 allow 规则且须为有效 ID")
	}
	return nil
}

// Evaluate applies deny rules first, then the first matching allow rule.
func (s *Snapshot) Evaluate(request Request) Decision {
	request = normalize(request)
	for _, rule := range s.Rules {
		if rule.Effect == Deny && rule.matches(request) {
			return Decision{Allowed: false, ReasonCode: "rule_denied", RuleID: rule.ID}
		}
	}
	for _, rule := range s.Rules {
		if rule.Effect == Allow && rule.matches(request) {
			return Decision{Allowed: true, ReasonCode: "allowed", RuleID: rule.ID, SecretRef: rule.SecretRef}
		}
	}
	return Decision{Allowed: false, ReasonCode: "default_deny"}
}

// AllowsConnect decides whether a tunnel to host:port may be opened at all. Individual requests
// inside the tunnel are still evaluated with Evaluate after TLS interception.
func (s *Snapshot) AllowsConnect(host string, port uint16) Decision {
	request := normalize(Request{Host: host, Port: port})
	for _, rule := range s.Rules {
		if rule.Effect == Deny && len(rule.Methods) == 0 && (rule.Path == "" || rule.Path == "/*") &&
			rule.hostMatches(request.Host) && rule.port() == request.Port {
			return Decision{Allowed: false, ReasonCode: "rule_denied", RuleID: rule.ID}
		}
	}
	for _, rule := range s.Rules {
		if rule.Effect == Allow && rule.hostMatches(request.Host) && rule.port() == request.Port {
			return Decision{Allowed: true, ReasonCode: "allowed", RuleID: rule.ID}
		}
	}
	return Decision{Allowed: false, ReasonCode: "default_deny"}
}

// SecretHosts lists hosts that have at least one rule injecting a secret, with the secret IDs.
func (s *Snapshot) SecretHosts() map[string][]string {
	result := map[string][]string{}
	for _, rule := range s.Rules {
		if rule.Effect == Allow && rule.SecretRef != "" {
			result[rule.Host] = appendUnique(result[rule.Host], rule.SecretRef)
		}
	}
	return result
}

func normalize(request Request) Request {
	request.Host = strings.ToLower(strings.TrimSuffix(request.Host, "."))
	if request.Port == 0 {
		request.Port = DefaultPort
	}
	return request
}

func (r Rule) port() uint16 {
	if r.Port == 0 {
		return DefaultPort
	}
	return r.Port
}

func (r Rule) hostMatches(host string) bool {
	if strings.HasPrefix(r.Host, "*.") {
		return strings.HasSuffix(host, r.Host[1:])
	}
	return host == r.Host
}

func (r Rule) matches(request Request) bool {
	if !r.hostMatches(request.Host) || r.port() != request.Port {
		return false
	}
	if len(r.Methods) > 0 && !contains(r.Methods, request.Method) {
		return false
	}
	return pathMatches(r.Path, request.Path)
}

func pathMatches(pattern, value string) bool {
	switch {
	case pattern == "" || pattern == "/*":
		return true
	case strings.HasSuffix(pattern, "/*"):
		prefix := strings.TrimSuffix(pattern, "*")
		return value == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(value, prefix)
	default:
		return value == pattern
	}
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	if contains(values, value) {
		return values
	}
	return append(values, value)
}
