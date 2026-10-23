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
}

// Event deliberately has no body, headers, arbitrary payload, tool args, or secret values.
type Recorder interface {
	Append(context.Context, Event) error
}
