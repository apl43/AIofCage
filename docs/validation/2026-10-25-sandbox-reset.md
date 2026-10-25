# 沙箱重置与恢复验证

日期：2026-10-25

## 代码验证

- `go test ./internal/runtime` 通过。
- `node --check frontend/src/js/sandboxes.js` 通过。
- `node --check frontend/src/js/sandbox-detail.js` 通过。
- `git diff --check` 在提交前执行。

## 交互与边界

- 沙箱详情和沙箱列表的更多操作都提供“重置运行环境”。
- 重置对话框默认保留 Agent home；运行中的沙箱默认勾选重置后恢复运行。
- 重置过程清空门卫授权，删除连接器、Agent、门卫和网络，再按保存的沙箱配置重新启动。
- 项目目录、沙箱名称、供应商绑定、MCP 工具权限、额度和版本配置保留。
- 不保留 Agent home 时，Agent 配置和原生 CLI 会话历史随 home 数据卷删除。
- 重置失败时保存的配置不删除，操作状态会显示失败原因；如果拆除已经完成，用户可再次启动重建运行对象。

## 当前限制

- 重置本身需要 Docker/Colima 运行环境可用；环境缺失时由桌面层直接提示。
- 仅保留 Agent home 不等于恢复应用内结构化会话；应用重启后的会话恢复仍由后续功能单独处理。
