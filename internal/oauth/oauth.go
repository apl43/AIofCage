// Package oauth will run browser authorization and serialized refresh on the trusted host.
package oauth

import "context"

// Grant contains only opaque references. Tokens must not appear in UI DTOs.
type Grant struct {
	ServiceID string
	AccountID string
	SecretRef string
}

type Client interface {
	Authorize(ctx context.Context, serviceID string) (Grant, error)
	Refresh(context.Context, Grant) error
	Revoke(context.Context, Grant) error
}
