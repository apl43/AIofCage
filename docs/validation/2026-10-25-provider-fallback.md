# 本机控制任务供应商故障切换验证

日期：2026-10-25

## 功能范围

- `run_task` 可以传入最多 3 个同 Agent 的备用供应商 ID，顺序就是切换顺序。
- 只有认证失败、限流、超时、连接失败或 DNS/网络错误触发切换；任务参数错误和未知业务错误直接失败。
- 切换前停止当前沙箱、更新供应商绑定并重新启动，保证新旧门卫策略不会同时生效。
- 任务元数据记录实际供应商、是否发生切换和切换原因；桌面交互会话仍使用手动供应商选择。

## 数据与安全边界

- 备用供应商必须属于同一个 Agent，不能借此改变沙箱的 Agent 类型。
- 只传递供应商 ID、名称和模型元数据；密钥仍由现有钥匙串与门卫链路读取，不进入任务结果。
- 失败诊断只用于分类，任务结果不返回原始 stderr，避免把凭据或上游响应带入比较结果。

## 代码验证

- `go test ./internal/service` 通过，覆盖故障分类和控制任务元数据链路。
- `make check`、`make test`、`go test -race ./internal/...` 在提交前执行。
- `node --check frontend/src/main.js`、`node --check frontend/src/js/agent-control.js` 和 `node --test frontend/tests/frames.test.js` 在提交前执行。
- `git diff --check` 在提交前执行。
