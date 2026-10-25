# 沙箱级 MCP 工具权限验证

日期：2026-10-25

本次验证覆盖固定 MCP 连接器（当前为 PostgreSQL 和合成业务案例）的沙箱级工具 allowlist。远程 HTTP/OAuth MCP 仍按服务级授权处理。

## 验证结果

- 固定连接器默认把目录中的工具授予新沙箱，旧沙箱没有 `mcp_permissions` 字段时保持这个兼容行为。
- 选择工具后，权限保存到沙箱配置；运行中的沙箱更新 MCP 配置时也会持久化新的工具范围。
- 工具名必须来自连接器固定目录；重复工具、未知工具、未启用服务的权限引用都会被拒绝。
- `mcp_permissions[service]` 为空时，服务仍保持启用，但 Gatekeeper 拒绝该服务的所有 `tools/call`，请求不会到达连接器，并留下 `mcp_tool_denied` 审计事件。
- 前端创建和编辑沙箱共用工具复选框。取消全部工具会保留服务选择并保存为空权限列表。

## 检查命令

```bash
go test ./internal/mcp ./internal/gatekeeper ./internal/runtime ./internal/service
node --check frontend/src/js/mcp.js
node --check frontend/src/js/sandboxes.js
node --test frontend/tests/frames.test.js
make check
make test
```

`make check` 和 `make test` 是本次分支提交前的完整检查；如果本机缺少 Colima、Docker 或交叉编译依赖，按命令输出记录阻塞原因，不把未执行写成通过。
