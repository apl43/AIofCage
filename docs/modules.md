# 工程骨架与模块说明

当前交付的是桌面入口、内存会话、模块接口，以及可配置、可转发的门卫（S2）。目的是让队友可以在同一工程里分模块开发；隔离、Keychain、模型、MCP 和审计采集仍未实现，G0—G5 也没有因此变成已验证。

## 入口与调用关系

```text
main.go / Wails
    ├── frontend/src：会话、服务连接、策略、审计、环境设置
    └── internal/service.App
          ├── session.MemoryStore：内存草稿
          └── runtime.Unavailable：依赖探测；启动始终返回未就绪

cmd/gatekeeper
    └── internal/gatekeeper
          ├── management：鉴权、/health、PUT /config、GET /events（SSE）、GET /usage、GET /ca
          ├── proxy：goproxy MITM；按 policy 授权、占位符换真 key、额度、脱敏事件
          └── mcp：尚未实现，拒绝所有请求
```

桌面与门卫目前是两个独立入口，桌面不会自动启动或连接门卫。前端直接使用 Wails 注入的 `window.go.service.App`，不依赖生成的前端绑定文件，也不运行 Node 服务。静态资源编译进桌面二进制。

## 模块边界与实际状态

| 模块 | 已有内容 | 下一步及限制 |
|---|---|---|
| `service` | 环境概览、选择目录、创建/列出会话草稿、拒绝启动 | 生命周期协调、持久化、控制面和用户授权；不绑定 vault 的读方法 |
| `session` | 会话/Agent/状态类型、线程安全内存存储 | 持久化与重启对账；草稿不代表任何容器存在 |
| `runtime` | 后端接口；平台与命令路径探测；不可用后端 | 专用 Colima、网络、挂载、就绪检查和安全停机；永不回退宿主执行 |
| `gatekeeper` | 三个独立监听；`PUT /config` 原子下发策略、密钥与额度；自签 CA + MITM；先授权再校验占位符，配了密钥的域名拒绝外来 key；只改请求头；不跟随重定向；上游错误正文脱敏；按沙箱的请求数/token 额度与 80% 告警；SSE 事件流（序号、补发、缺口）；用量统计 | 只支持 443 端口 HTTPS；MCP 入口仍拒绝；配置不落盘，门卫重启后需应用重新下发（额度用基线延续） |
| `policy` | 规则校验、规范化、deny 优先、默认拒绝；精确路径或 `/*` 前缀；deny 规则可用 `*.` 通配 | 只覆盖 HTTP 方法与路径，不是 MCP 工具权限 |
| `vault` | 秘密存取接口 | Keychain 与安全交付；无文件、环境变量或内存假存储兜底 |
| `audit` / `events` | 脱敏事件结构（含主机、方法、路径、状态、字节、token）；门卫侧 SSE 已实现 | 应用侧订阅、去重、JSONL 落盘与缺口提示（S6） |
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

`make gatekeeper` 生成本机二进制；`make gatekeeper-linux` 生成沙箱里用的 Linux/arm64 二进制，并把 `docker/gatekeeper/Dockerfile` 一起放进 `build/gatekeeper/`：

```bash
make gatekeeper-linux
docker --context colima-aiofcage build -t aiofcage/gatekeeper:dev build/gatekeeper
```

管理 token 可以用 `--token-stdin` 从标准输入第一行读取（容器内使用，避免出现在 `docker inspect`），也可以在本机开发时用环境变量 `CAGE_ADMIN_TOKEN`。监听地址必须是具体接口 IP，默认分别为 `127.0.0.1:8899`（管理）、`127.0.0.1:8888`（代理）、`127.0.0.1:8890`（MCP）。

| 入口 | 行为 |
|---|---|
| 管理：无效、缺失或重复 Authorization | 401，不回显 token |
| 管理：带 Origin 或查询串 | 403 / 400 |
| `GET /health` | 未配置 503（`ready=false`）；配置后 200，返回 `run_id`、策略版本、最新事件序号 |
| `PUT /config` | 校验后原子替换策略、密钥、额度；策略版本必须递增（否则 409）；错误信息只含 ID，不含密钥或占位符 |
| `GET /events` | SSE；每条事件 `id: <run_id>:<seq>`；带 `Last-Event-ID` 断线续传；游标过旧或来自其他运行时先发 `audit.gap` |
| `GET /usage` | 请求数、拒绝数、额度计数、按密钥 ID 的请求与 token；不含任何密钥 |
| `GET /ca` | 本次运行的公有 CA 证书（私钥只在内存） |
| 代理 | 未配置时一律 503；配置后按 §6.2 的顺序授权，拒绝时返回 403/429 和 `X-Aiofcage-Reason` |
| MCP | 尚未实现，一律 503 |

`PUT /config` 的结构见 `internal/gatekeeper/config.go`：`policy`（规则）、`secrets`（ID、占位符、注入头 `Authorization` 或 `X-Api-Key`、前缀、真值）、`limits`（请求数/token 上限及基线）。

## 验证与接续开发

`make check` 执行带桌面构建标签的 Go vet/build；`make test` 执行后端竞态检查。当前测试验证：未配置时不能启动、不能通过管理配置启用代理，管理认证/Origin/重复认证检查，代理/CONNECT/MCP 拒绝，以及无效监听地址被拒绝。测试用 HTTP 记录器与内存会话，不读写用户文件、不启动容器、不调用真实外部服务。

前端语法可运行 `node --check frontend/src/main.js`，Node 仅用于可选开发检查，应用本身无需 Node。桌面需 macOS 工具链；独立门卫可用 `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/gatekeeper` 编译到 Linux。没有 `.api` 及其生成类型，本工程目前不涉及 goctl 生成。

建议先实现和验证 runtime 的专用环境/拒绝绕过，再接门卫管理与事件，然后按目标文档处理模型和 MCP。角色建议为：边界负责人处理 runtime/gatekeeper/vault/connector；平台负责人处理 service/session/policy/audit/events/oauth/mcp；界面负责人处理 frontend 和桌面交互。实际成员姓名与完成情况尚未填写，不能把角色建议算成比赛分工证据。
