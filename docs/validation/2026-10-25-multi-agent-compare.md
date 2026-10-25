# 多 Agent 任务对比验证

日期：2026-10-25

## 功能范围

- 本机 stdio MCP 的 `run_task` 记录 Agent、供应商、模型、思考强度、状态、开始/结束时间、耗时和本任务请求/token 增量。
- `get_task` 保留原有完整答案查询，同时返回任务元数据。
- 新增 `compare_tasks`，可显式传入最多 16 个任务 ID，也可以省略 ID 比较当前控制会话的全部任务。
- 对比结果只返回状态、运行信息、用量、工具名称和最多 500 字的输出摘要，不返回完整答案。
- 任务只能在创建它的 stdio 控制会话内被查询和比较。

## 代码验证

- `go test ./internal/service` 通过，覆盖任务元数据排序、用量字段和输出脱敏范围。
- `node --check frontend/src/main.js` 通过。
- `node --check frontend/src/js/agent-control.js` 通过。
- `node --test frontend/tests/frames.test.js` 通过。
- `git diff --check` 在提交前执行。

## 限制

- 对比任务仍要求调用方先为每个 Agent 创建独立沙箱；不在同一会话中共享原生对话上下文。
- 任务强制停止或门卫无法返回用量时，比较结果中的请求/token 增量可能为零或缺失语义；状态和错误仍会保留。
