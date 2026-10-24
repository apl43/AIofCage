//go:build !windows

package runtime

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunnerCancellationStopsPipeHoldingChildren(t *testing.T) {
	start := time.Now()
	_, err := (runner{path: "/bin/sh"}).run(context.Background(), 200*time.Millisecond, nil, "-c", "sleep 30 & wait")
	if err == nil || !strings.Contains(err.Error(), "超时") || time.Since(start) > 4*time.Second {
		t.Fatalf("子进程未随命令及时取消: elapsed=%s err=%v", time.Since(start), err)
	}
}
