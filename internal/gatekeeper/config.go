package gatekeeper

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/shaoxier/AIofCage/internal/policy"
)

// Config is delivered by the desktop app through PUT /config. It is the only place real secret
// values enter the gatekeeper, and it is never serialized back out.
type Config struct {
	Policy  policy.Snapshot `json:"policy"`
	Secrets []Secret        `json:"secrets"`
	Limits  Limits          `json:"limits"`
}

// Secret maps a placeholder visible inside the sandbox to the real credential for one provider.
type Secret struct {
	ID          string `json:"id"`
	Placeholder string `json:"placeholder"`
	Header      string `json:"header"`
	Prefix      string `json:"prefix"`
	Value       string `json:"value"`
}

// Limits are enforced per sandbox. Baselines let the app carry usage across gatekeeper restarts
// so that restarting a sandbox does not reset its budget.
type Limits struct {
	MaxRequests      uint64 `json:"max_requests,omitempty"`
	MaxTokens        uint64 `json:"max_tokens,omitempty"`
	BaselineRequests uint64 `json:"baseline_requests,omitempty"`
	BaselineTokens   uint64 `json:"baseline_tokens,omitempty"`
}

const (
	maxSecrets     = 32
	maxSecretBytes = 4096
)

var (
	placeholderPattern = regexp.MustCompile(`^cage-[A-Za-z0-9_-]{16,128}$`)
	// Credential headers the gatekeeper understands. Anything else is not treated as a credential.
	credentialHeaders = []string{"Authorization", "X-Api-Key"}
)

// compiled is the immutable, validated form used on the request path.
type compiled struct {
	config      Config
	secrets     map[string]Secret
	secretHosts map[string][]Secret
}

func compile(config Config) (*compiled, error) {
	if err := config.Policy.Validate(); err != nil {
		return nil, err
	}
	if len(config.Secrets) > maxSecrets {
		return nil, fmt.Errorf("密钥不能超过 %d 个", maxSecrets)
	}
	result := &compiled{config: config, secrets: map[string]Secret{}, secretHosts: map[string][]Secret{}}
	placeholders := map[string]bool{}
	for i, secret := range config.Secrets {
		secret.Header = http.CanonicalHeaderKey(secret.Header)
		config.Secrets[i] = secret
		// Messages only name the secret ID; values and placeholders never appear in errors.
		if !policy.ValidID(secret.ID) {
			return nil, fmt.Errorf("第 %d 个密钥的 ID 无效", i+1)
		}
		if _, ok := result.secrets[secret.ID]; ok {
			return nil, fmt.Errorf("密钥 ID 重复：%s", secret.ID)
		}
		if !placeholderPattern.MatchString(secret.Placeholder) || placeholders[secret.Placeholder] {
			return nil, fmt.Errorf("密钥 %s 的占位符无效或重复", secret.ID)
		}
		if !isCredentialHeader(secret.Header) {
			return nil, fmt.Errorf("密钥 %s 的注入头只能是 Authorization 或 X-Api-Key", secret.ID)
		}
		if secret.Prefix != "" && secret.Prefix != "Bearer " {
			return nil, fmt.Errorf("密钥 %s 的前缀只能为空或 \"Bearer \"", secret.ID)
		}
		if secret.Value == "" || len(secret.Value) > maxSecretBytes || strings.ContainsAny(secret.Value, " \t\r\n") ||
			secret.Value == secret.Placeholder || strings.HasPrefix(secret.Value, "cage-") {
			return nil, fmt.Errorf("密钥 %s 的值无效", secret.ID)
		}
		placeholders[secret.Placeholder] = true
		result.secrets[secret.ID] = secret
	}
	for host, refs := range config.Policy.SecretHosts() {
		for _, ref := range refs {
			secret, ok := result.secrets[ref]
			if !ok {
				return nil, fmt.Errorf("规则引用了不存在的密钥：%s", ref)
			}
			result.secretHosts[host] = append(result.secretHosts[host], secret)
		}
	}
	if config.Limits.BaselineRequests > 1<<40 || config.Limits.BaselineTokens > 1<<50 {
		return nil, errors.New("用量基线超出范围")
	}
	result.config = config
	return result, nil
}

func isCredentialHeader(name string) bool {
	for _, header := range credentialHeaders {
		if header == name {
			return true
		}
	}
	return false
}
