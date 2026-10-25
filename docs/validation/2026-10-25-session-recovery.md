# 会话恢复验证

日期：2026-10-25

## 代码验证

- `go test ./internal/service` 通过，覆盖恢复元数据保存、读取、排序和主动清理。
- `node --check frontend/src/js/terminal.js` 通过。
- `node --check frontend/src/js/sandbox-detail.js` 通过。
- `node --check frontend/src/js/sandboxes.js` 通过。
- `node --check frontend/src/main.js` 通过。
- `git diff --check` 在提交前执行。

## 数据边界

- 记录仅包含桌面标签 ID、沙箱 ID、Agent、供应商 ID、模型、思考强度、原生 session ID 和更新时间。
- 消息正文、思考内容、工具参数、图片和供应商凭据不写入恢复记录。
- 应用退出和沙箱停止会关闭当前进程，但保留恢复记录；用户主动关闭会话或删除沙箱会清理记录。

## 恢复条件

- 恢复前必须启动对应沙箱，并确认会话桥接组件存在。
- 供应商配置必须与保存记录匹配；配置变化时界面显示原因并要求新建会话。
- 恢复后继续使用原生 session ID，模型和思考强度保持原会话设置。
