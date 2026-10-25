// Package events 定义可续传的脱敏事件订阅，不重放模型或工具请求。
package events

import (
	"context"

	"github.com/shaoxier/AIofCage/internal/audit"
)

// Handler 接收方完成处理才返回 nil；持久化采集须先落盘，失败必须中断订阅。
type Handler func(audit.Event) error

// Source 用完整事件 ID 续传，包括上次运行的 ID，由服务端报告 audit.gap。
// 接收方拒绝确认一条事件后，不得再投递后续事件。
type Source interface {
	Events(ctx context.Context, sandboxID, lastID string, handle Handler) error
}
