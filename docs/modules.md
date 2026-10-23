# 工程骨架与模块说明

当前交付的是桌面入口、内存会话、模块接口，以及可配置、可转发的门卫（S2）。目的是让队友可以在同一工程里分模块开发；隔离、Keychain、模型、MCP 和审计采集仍未实现，G0—G5 也没有因此变成已验证。

## 入口与调用关系

```text
main.go / Wails
    ├── frontend/src：Preact + htm（vendor 目录内固定版本，无 npm 构建）
    │     沙箱 / 供应商 / MCP / 日志 / 设置
    └── internal/service.App（唯一暴露给前端的 API，不返回任何密钥）
          ├── store：供应商与沙箱元数据
          ├── vault.Keychain：密钥只写入、只由运行时读取
          └── runtime.Manager：专用虚拟机与沙箱生命周期

cmd/cagectl ──> runtime.Manager（与桌面应用共用）
cmd/gatekeeper（Linux 版嵌入运行时，在每个沙箱里运行）
    └── internal/gatekeeper：管理 / 代理 / MCP 三个监听
```

## 模块边界与实际状态

| 模块 | 已有内容 | 下一步及限制 |
|---|---|---|
| `service` | 环境概览；供应商增删改、预设、连通性测试（宿主直连，不经沙箱）；沙箱创建（含项目导入）、启停、终端、切换供应商、删除；进度事件；事件订阅、JSONL 审计、用量与资源监视 | `existing_key_ref` 只允许 AIofCage 自己的条目和预设列出的条目，防止界面被用来指向任意钥匙串秘密 |
| `session` | Agent 枚举（codex / claude-code / dsh） | 原“会话草稿”已由 `sandbox` 取代 |
| `runtime` | `Environment`：创建/启动专用 Colima 实例并核对实际共享与 context；`Manager`：每个沙箱独立的内部网络、出网网络、门卫与 Agent 容器，镜像按内容摘要构建并缓存，门卫先配置再启动 Agent，切换供应商热更新，停止时保存累计用量 | 桌面应用尚未接入（S5）；永不回退宿主执行 |
| `provider` | 供应商元数据（接口、协议、模型、认证方式、钥匙串条目）、协议兼容性、按协议生成最小放行规则 | Codex 只支持 Responses API |
| `sandbox` | 沙箱元数据、Agent→供应商绑定、每个沙箱+供应商一个占位符、项目必须位于 `~/CageWorkspace` 且无符号链接 | 改变已装 Agent 需重建 Agent 容器（home 数据卷保留） |
| `store` | 供应商与沙箱的 JSON 持久化（`~/Library/Application Support/AIofCage`，0600，原子写入） | 不存任何密钥 |
| `gatekeeper` | 三个独立监听；`PUT /config` 原子下发策略、密钥与额度；自签 CA + MITM；先授权再校验占位符，配了密钥的域名拒绝外来 key；只改请求头；不跟随重定向；上游错误正文脱敏；按沙箱的请求数/token 额度与 80% 告警；SSE 事件流（序号、补发、缺口）；用量统计 | 只支持 443 端口 HTTPS；MCP 入口仍拒绝；配置不落盘，门卫重启后需应用重新下发（额度用基线延续） |
| `policy` | 规则校验、规范化、deny 优先、默认拒绝；精确路径或 `/*` 前缀；deny 规则可用 `*.` 通配 | 只覆盖 HTTP 方法与路径，不是 MCP 工具权限 |
| `vault` | 接口 + `Keychain` 实现：经 `/usr/bin/security` 读写登录钥匙串，写入时密钥走标准输入（不出现在进程列表），写后回读校验 | 不绑定到前端；读方法只供运行时下发给门卫 |
| `audit` / `events` | 脱敏事件结构（含主机、方法、路径、状态、字节、token）；门卫 SSE；应用侧订阅、续传、JSONL 落盘、缺口事件 | 审计日志不做防篡改（hash 链在路线图） |
| `mcp` | 服务绑定草案与网关接口 | 协议 SDK、工具/参数/资源授权及会话映射；绑定草案不是可执行权限 |
| `oauth` | 宿主授权/续期/撤销接口，只有秘密引用 | PKCE、发现、注册、回调、串行刷新及 Keychain |
| `connector` | 固定服务定义与运行接口 | 镜像摘要审查、私有网络和秘密交付；不运行传入的命令或镜像 |
| `buildinfo` | 桌面与门卫共用的版本与 `skeleton` 阶段 | 发布时注入真实版本 |

## 桌面开发接口

前端通过 Wails 注入的 `window.go.service.App` 调用，所有方法返回 Promise。

| 方法 | 说明 |
|---|---|
| `GetOverview()` | 版本、运行环境检查（会访问专用虚拟机）、可安装的 Agent 及版本、数据目录与工作区 |
| `ListPresets()` | 供应商预设（`internal/provider/presets.json`，模型名可能过时，可随时修改） |
| `ListProviders()` | 供应商列表，只返回“密钥是否已保存”和使用它的沙箱，不返回密钥 |
| `SaveProvider(request)` | 新建或编辑；`api_key` 只写入钥匙串；编辑时留空表示不改密钥；改动会立即同步到运行中的沙箱 |
| `DeleteProvider(id)` | 仍有沙箱在用时拒绝；同时删除 AIofCage 保存的钥匙串条目 |
| `TestProvider(id)` | 由桌面进程直连供应商的模型列表接口，返回是否可达、密钥是否有效与耗时 |
| `ListSandboxes()` | 沙箱、实际容器状态、Agent→供应商、累计用量 |
| `CreateSandbox(request)` | 名称、项目（`use` 工作区内已有文件夹 / `copy` 复制进工作区 / `new` 新建空文件夹）、Agent→供应商、可选的请求数与 token 上限 |
| `StartSandbox(id)` / `StopSandbox(id)` | 启动过程通过 `sandbox:progress` 事件推送进度；首次启动会构建镜像 |
| `BindProvider(id, agent, providerID)` | 切换某个 Agent 的供应商；运行中立即下发，Agent 下次启动生效 |
| `OpenTerminal(id, agent)` | 在“终端”里以 Agent 用户进入项目目录，运行该 Agent（`agent` 为空时开 Shell）；第一次会请求“自动化”权限 |
| `OpenProjectFolder(id)` | 在访达中打开项目文件夹 |
| `DeleteSandbox(id, keepHome)` | 删除容器与网络；项目文件夹永远保留，`keepHome` 决定是否保留 Agent 的 home 数据卷 |
| `SelectProject()` | 系统目录选择框 |
| `RecentEvents(id)` | 最近的门卫事件（`id` 为空表示全部沙箱，最多 300 条），附沙箱名称 |
| `UsageSummary()` | 每个沙箱的累计请求/token、实时拒绝数与 CPU/内存，以及按供应商汇总的累计用量 |
| `RevealAuditLog(id)` | 在访达中显示该沙箱的 JSONL 审计日志 |

后台监视（`StartMonitor`，每 5 秒）：为每个运行中的沙箱订阅门卫 SSE（同一门卫运行内按 `Last-Event-ID` 续传，门卫重启后从头订阅），把事件追加写入 `~/Library/Application Support/AIofCage/audit/<沙箱ID>.jsonl`（0600），推送 `sandbox:event`；轮询门卫 `/usage` 并按“门卫运行 + 供应商”计算增量写回累计用量（应用重启也不会重复计数），推送 `sandbox:usage`；用一次 `docker stats` 采样所有运行中沙箱，推送 `sandbox:stats`。额度到 80% 与超额由门卫产生事件，界面弹出提醒。

复制项目时只复制普通文件、目录和符号链接本身（不跟随链接），超过 2 GB 或 20 万个文件会拒绝；拒绝复制家目录、根目录和工作区本身。

真实环境的端到端测试：`AIOFCAGE_E2E=1 go test -run TestEndToEnd ./internal/service/`（需要专用虚拟机和钥匙串中的 `sbx-builderday-key`），会创建、启动、进入、切换供应商、停止并删除一个临时沙箱。

界面自测：`frontend/src` 是静态文件，可以用任意静态服务器打开，并在加载 `main.js` 前定义假的 `window.go.service.App`，用无头浏览器截图检查各页面；地址里的 `#providers` 等锚点会直接打开对应页面。

## 沙箱运行时与 cagectl

`make cagectl` 先编译 Linux 门卫并把它嵌入程序，再生成 `bin/cagectl`。它和桌面应用共用同一套运行时与数据目录，用于开发、自动检查和演示兜底：

```bash
make cagectl
./bin/cagectl env up                                   # 创建/启动专用虚拟机并核对共享边界
./bin/cagectl provider add --id xapi --name "七牛 xapi" --agent codex --protocol openai-responses \
    --base-url https://xapi.qiniu.io/v1 --model gpt-6.1-sol --keychain-service sbx-builderday-key
./bin/cagectl sandbox create --name demo --project ~/CageWorkspace/demo --bind codex=xapi --max-requests 200
./bin/cagectl sandbox start demo
./bin/cagectl sandbox exec demo -t -- codex            # 交互；非交互运行请关闭标准输入（codex exec 会等待管道输入）
./bin/cagectl sandbox events demo                      # 实时事件
./bin/cagectl sandbox stop demo
```

一个沙箱对应的 Docker 对象（都带标签 `io.aiofcage.sandbox=<ID>`）：

| 对象 | 名称 | 要点 |
|---|---|---|
| 内部网络 | `aiofcage-<ID>-in` | `--internal`，无默认路由 |
| 出网网络 | `aiofcage-<ID>-out` | 只有门卫接入 |
| 门卫容器 | `aiofcage-<ID>-gate` | 只读根、无特权；管理端口只发布到宿主 `127.0.0.1` 的随机端口；管理 token 由门卫自己生成，写在只有门卫能读的 tmpfs，宿主用 `docker exec` 读取，不进 `docker inspect` |
| Agent 容器 | `aiofcage-<ID>-agent` | UID 1000、只读根、无特权、`--dns 127.0.0.1`；只挂载项目目录（与宿主同一路径）；`/etc/ssl/certs` 与 `/tmp` 为 tmpfs，每次启动写入门卫 CA |
| home 数据卷 | `aiofcage-<ID>-home` | Agent 的配置与会话记录，跨重启保留 |

启动顺序：校验虚拟机 → 构建/复用镜像 → 网络与容器 → 启动门卫 → 下发策略与密钥（从钥匙串读取）→ 启动 Agent → 写入 CA 与 Agent 配置（`~/.aiofcage/env` 里只有占位符）。任何一步失败都会停掉两个容器，不留下“Agent 在跑、门卫没配置”的状态。

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
