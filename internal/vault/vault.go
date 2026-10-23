// Package vault owns secret persistence. It must never bind secret read methods to Wails.
package vault

import "context"

// Store values belong only in Keychain and trusted process memory, never application metadata.
// No implementation is provided until Keychain and secure delivery are validated.
type Store interface {
	Put(ctx context.Context, reference string, value []byte) error
	Read(ctx context.Context, reference string) ([]byte, error)
	Delete(ctx context.Context, reference string) error
}
