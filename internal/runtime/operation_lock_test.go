package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shaoxier/AIofCage/internal/store"
)

func TestOperationLockCoordinatesSeparateManagers(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &Manager{store: st, locks: map[string]*sync.Mutex{}}
	b := &Manager{store: st, locks: map[string]*sync.Mutex{}}
	unlock, err := a.lock(context.Background(), "same")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	for name, manager := range map[string]*Manager{"同一运行器": a, "独立运行器": b} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if release, err := manager.lock(ctx, "same"); !errors.Is(err, context.DeadlineExceeded) {
				if release != nil {
					release()
				}
				t.Fatal("等待锁时未响应超时", err)
			}
		})
	}
}
