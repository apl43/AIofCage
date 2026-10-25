# OAuth MCP 生命周期验证

日期：2026-10-25

本次功能补齐 OAuth MCP 的本机生命周期：浏览器授权、访问令牌刷新、过期提示和本机断开。

## 验证结果

- 新建 OAuth 服务不会接受粘贴令牌，授权结果只保存到宿主钥匙串。
- access token 临近过期时，宿主用 refresh token 刷新后再测试或重新下发到运行中的沙箱。
- access token 和 refresh token 都不可用时，服务显示需要重新登录；运行沙箱刷新后不再向 Gatekeeper 下发该服务的旧 token。
- 本机断开会删除钥匙串中的 OAuth 凭据，并刷新使用该服务的运行沙箱；服务配置本身保留，用户可以再次登录。
- 断开仅清理本机凭据；上游是否撤销授权取决于服务是否提供可用的 OAuth revocation endpoint。

## 检查命令

```bash
go test ./internal/service ./internal/oauth
node --check frontend/src/js/mcp.js
make check
make test
```
