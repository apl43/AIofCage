# 智能体链路诊断验证

日期：2026-10-25

本次功能补齐结构化会话的启动前检查和失败提示，覆盖运行环境、沙箱状态、供应商绑定、钥匙串凭据、Gatekeeper 和会话桥接组件。

## 验证结果

- 会话启动失败时，界面显示诊断检查列表，区分通过和需要处理的项。
- 诊断不发送模型请求，不消耗供应商额度。
- 供应商认证失败、限流、请求超时、连接拒绝和网络不可达会转换成对应的中文处理建议。
- 智能体标准错误只保留有限尾部；展示前替换已知供应商密钥、MCP 令牌和 OAuth access/refresh token。
- 诊断和错误提示不写入审计正文，模型响应仍通过会话事件显示。

## 检查命令

```bash
go test ./internal/service
node --check frontend/src/js/terminal.js
node --check frontend/src/js/conversation-view.js
make check
make test
```
