# 工程骨架与模块说明

当前交付的是桌面入口、内存会话、模块接口与始终拒绝转发的门卫。目的是让队友可以在同一工程里分模块开发；隔离、Keychain、模型、MCP 和审计采集仍未实现，G0—G5 也没有因此变成已验证。

## 入口与调用关系

```text
main.go / Wails
    ├── frontend/src：会话、服务连接、策略、审计、环境设置
    └── internal/service.App
          ├── session.MemoryStore：内存草稿
          └── runtime.Unavailable：依赖探测；启动始终返回未就绪

cmd/gatekeeper
    └── internal/gatekeeper
          ├── management：鉴权与未就绪健康状态
          ├── proxy：拒绝所有请求，包括 CONNECT
          └── mcp：拒绝所有请求
```

桌面与门卫目前是两个独立入口，桌面不会自动启动或连接门卫。前端直接使用 Wails 注入的 `window.go.service.App`，不依赖生成的前端绑定文件，也不运行 Node 服务。静态资源编译进桌面二进制。

## 模块边界与实际状态

| 模块 | 已有内容 | 下一步及限制 |
|---|---|---|
| `service` | 环境概览、选择目录、创建/列出会话草稿、拒绝启动 | 生命周期协调、持久化、控制面和用户授权；不绑定 vault 的读方法 |
| `session` | 会话/Agent/状态类型、线程安全内存存储 | 持久化与重启对账；草稿不代表任何容器存在 |
| `runtime` | 后端接口；平台与命令路径探测；不可用后端 | 专用 Colima、网络、挂载、就绪检查和安全停机；永不回退宿主执行 |
| `gatekeeper` | 三个独立 HTTP 监听、管理认证、固定拒绝响应、优雅停机 | 策略激活、MITM、凭据注入、事件与额度；不可直接作为可用代理 |
| `policy` | 版本化规则草案与默认拒绝结果 | 规范化、deny 优先、规则验证与匹配；目前没有规则会被执行 |
| `vault` | 秘密存取接口 | Keychain 与安全交付；无文件、环境变量或内存假存储兜底 |
| `audit` / `events` | 脱敏事件、Recorder 与游标订阅接口 | JSONL、SSE、去重补发、缺口；当前无实际事件采集 |
| `mcp` | 服务绑定草案与网关接口 | 协议 SDK、工具/参数/资源授权及会话映射；绑定草案不是可执行权限 |
| `oauth` | 宿主授权/续期/撤销接口，只有秘密引用 | PKCE、发现、注册、回调、串行刷新及 Keychain |
| `connector` | 固定服务定义与运行接口 | 镜像摘要审查、私有网络和秘密交付；不运行传入的命令或镜像 |
| `buildinfo` | 桌面与门卫共用的版本与 `skeleton` 阶段 | 发布时注入真实版本 |

## 桌面开发接口

| 方法 | 输入/输出 | 当前行为 |
|---|---|---|
| `GetOverview()` | 版本、阶段、环境检查和目标 Agent 列表 | 只探测平台及 PATH；发现 colima/docker 命令不代表服务可用 |
| `SelectProject()` | 返回系统目录选择结果；取消返回空字符串 | 不扫描文件、不复制、不挂载 |
| `CreateSession(request)` | `name`、`project_path`、`agent` → 会话草稿 | 名称 1—80 字符、绝对路径、固定 Agent 枚举；只存内存 |
| `ListSessions()` | 会话列表 | 新建优先；初始为空，不返回演示会话 |
| `StartSession(id)` | 错误 | 会话不存在报错；存在也始终返回未就绪，不改变草稿状态 |

例如登记 `/example/project` 和 `codex` 只保存路径文字；并不验证目录内容或授予文件权限。未来接入运行时前必须重新解析真实路径、符号链接、导入状态和共享范围，不能信任草稿的路径文字直接挂载。

## 门卫本机开发

`make gatekeeper` 生成 `bin/gatekeeper`。以下仅启动本机拒绝服务，不能运行 Agent：

```bash
export CAGE_ADMIN_TOKEN="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')"
./bin/gatekeeper
```

`CAGE_ADMIN_TOKEN` 是临时控制面 token，不是模型或 MCP 凭据；默认监听分别为 `127.0.0.1:8899`、`127.0.0.1:8888`、`127.0.0.1:8890`。程序拒绝空 token、过短/错误编码 token、通配地址、域名监听和非法端口。当前允许显式指定具体接口 IP，容器网络绑定与宿主发布范围仍须由未来运行时验证。

| 入口 | 当前响应 |
|---|---|
| 管理：无效、缺失或重复 Authorization | 401，不回显 token |
| 管理：带 Origin 的请求 | 403，不提供浏览器跨域访问 |
| `GET /health`（有效 Bearer token） | 503，`ready=false`、`reason=not_configured`；不应以进程存活代替就绪 |
| `PUT /config`、`GET /events`、`GET /usage`（有效 token） | 501，未实现；不会装载任何配置或秘密 |
| 代理任意方法、CONNECT 与 MCP 任意请求 | 503，始终拒绝，无上游网络访问 |

结束进程使用 Ctrl+C。应用不创建持久化会话、容器或凭据；`make clean` 只删除两个本地二进制，不触及项目内容。未来 SSE 接入需重新处理当前 HTTP 写超时，不能沿用短超时后宣称长连接可用。

## 验证与接续开发

`make check` 执行带桌面构建标签的 Go vet/build；`make test` 执行后端竞态检查。当前测试验证：未配置时不能启动、不能通过管理配置启用代理，管理认证/Origin/重复认证检查，代理/CONNECT/MCP 拒绝，以及无效监听地址被拒绝。测试用 HTTP 记录器与内存会话，不读写用户文件、不启动容器、不调用真实外部服务。

前端语法可运行 `node --check frontend/src/main.js`，Node 仅用于可选开发检查，应用本身无需 Node。桌面需 macOS 工具链；独立门卫可用 `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/gatekeeper` 编译到 Linux。没有 `.api` 及其生成类型，本工程目前不涉及 goctl 生成。

建议先实现和验证 runtime 的专用环境/拒绝绕过，再接门卫管理与事件，然后按目标文档处理模型和 MCP。角色建议为：边界负责人处理 runtime/gatekeeper/vault/connector；平台负责人处理 service/session/policy/audit/events/oauth/mcp；界面负责人处理 frontend 和桌面交互。实际成员姓名与完成情况尚未填写，不能把角色建议算成比赛分工证据。
