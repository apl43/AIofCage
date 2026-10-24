// Package audit defines redacted events; durable recording is not implemented yet.
package audit

import (
	"context"
	"time"
)

type Event struct {
	ID            string    `json:"id"`
	Time          time.Time `json:"ts"`
	Source        string    `json:"source"`
	RunID         string    `json:"gatekeeper_run_id,omitempty"`
	Sequence      uint64    `json:"seq,omitempty"`
	SessionID     string    `json:"session_id"`
	Action        string    `json:"action"`
	Decision      string    `json:"decision"`
	ReasonCode    string    `json:"reason_code"`
	RuleID        string    `json:"rule,omitempty"`
	PolicyVersion uint64    `json:"policy_version,omitempty"`
	ServiceID     string    `json:"service_id,omitempty"`
	ToolID        string    `json:"tool_id,omitempty"`

	// Network metadata. Path never includes the query string; SecretRef is a name, never a value.
	Host         string `json:"host,omitempty"`
	Method       string `json:"method,omitempty"`
	Path         string `json:"path,omitempty"`
	SecretRef    string `json:"secret_ref,omitempty"`
	Status       int    `json:"status,omitempty"`
	BytesIn      int64  `json:"bytes_in,omitempty"`
	BytesOut     int64  `json:"bytes_out,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	// UsageKind is "reported" when tokens came from the upstream response, "unknown" otherwise.
	UsageKind string `json:"usage_kind,omitempty"`
}

// Event values used by the gatekeeper.
const (
	SourceGatekeeper = "gatekeeper"
	SourceApp        = "app"

	ActionConnect      = "net.connect"
	ActionRequest      = "net.request"
	ActionMCPCall      = "mcp.call"
	ActionQuotaBlocked = "quota.blocked"
	ActionQuotaWarning = "quota.warning"
	ActionPolicyChange = "policy.change"
	ActionGap          = "audit.gap"

	DecisionAllow         = "allow"
	DecisionDeny          = "deny"
	DecisionInject        = "inject"
	DecisionError         = "error"
	DecisionNotApplicable = "not-applicable"
)

// Event deliberately has no body, headers, arbitrary payload, tool args, or secret values.
type Recorder interface {
	Append(context.Context, Event) error
}
