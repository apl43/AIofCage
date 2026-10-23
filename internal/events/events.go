// Package events defines resumable metadata subscriptions, not request replay.
package events

import (
	"context"

	"github.com/shaoxier/AIofCage/internal/audit"
)

type Cursor struct {
	RunID    string
	Sequence uint64
}

// Source must report audit.gap when the cursor cannot be resumed completely.
type Source interface {
	Subscribe(context.Context, Cursor) (<-chan audit.Event, error)
}
