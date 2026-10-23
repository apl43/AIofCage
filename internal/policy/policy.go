// Package policy defines versioned authorization input; matching is not implemented yet.
package policy

type Rule struct {
	ID        string   `json:"id"`
	Effect    string   `json:"effect"`
	Host      string   `json:"host"`
	Port      uint16   `json:"port"`
	Methods   []string `json:"methods"`
	Path      string   `json:"path"`
	SecretRef string   `json:"secret_ref,omitempty"`
}

type Snapshot struct {
	Version   uint64 `json:"version"`
	SessionID string `json:"session_id"`
	Rules     []Rule `json:"rules"`
}

type Decision struct {
	Allowed    bool
	ReasonCode string
	RuleID     string
}

// DenyUnconfigured is the only current decision path. No rules are activated.
func DenyUnconfigured() Decision {
	return Decision{Allowed: false, ReasonCode: "policy_not_configured"}
}
