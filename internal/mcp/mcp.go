// Package mcp owns protocol-level service, tool, and resource authorization.
package mcp

import "context"

type Binding struct {
	SessionID string   `json:"session_id"`
	ServiceID string   `json:"service_id"`
	Transport string   `json:"transport"`
	AuthKind  string   `json:"auth_kind"`
	SecretRef string   `json:"secret_ref,omitempty"`
	Tools     []string `json:"tools"`
}

// Binding is a draft, not an executable policy; typed resource/argument rules are still pending.
// An implementation must validate full policy before accepting a connection.
type Gateway interface {
	Connect(context.Context, Binding) error
	Revoke(ctx context.Context, sessionID, serviceID string) error
}
