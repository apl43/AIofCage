# 审计时间线验证

日期：2026-10-25

本次功能在已有 JSONL 脱敏审计基础上增加按沙箱导出时间线，日志页继续提供全部、拦截、注入、放行和系统筛选。

## 验证结果

- 日志页按时间顺序展示沙箱、决策、原因、目标、供应商、token 和耗时等元数据。
- 选择沙箱后可以导出当前保存的最近脱敏事件 JSON，文件权限为用户私有。
- 导出沿用 `EventView`，不会新增请求正文、MCP 参数、响应内容或密钥。
- 审计缺口事件保留在时间线中，用户可以看到日志完整性边界。

## 检查命令

```bash
go test ./internal/service
node --check frontend/src/js/logs.js
make check
make test
```
