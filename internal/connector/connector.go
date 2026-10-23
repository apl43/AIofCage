// Package connector defines trusted catalog entries, never project-supplied shell commands.
package connector

import "context"

type Definition struct {
	ServiceID string
	Image     string // Must be an approved immutable image digest before execution.
	Argv      []string
	SecretRef string
}

// Runner will be wired only after private networking and credential delivery pass validation.
type Runner interface {
	Start(ctx context.Context, sessionID string, definition Definition) error
	Stop(ctx context.Context, sessionID, serviceID string) error
}
