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
| 2 | **出网拓扑**：`cage-internal`（无默认路由）+ **门卫自己就是双网卡边车** | 一个容器同时具备内网地址和出网能力，就同时是唯一入口与唯一出口；`-p` 在内部网络无效、`host.docker.internal` 不可解析，都是实测结论 |
| 3 | **默认拒绝 + 未知目标弹窗询问** | 学 Docker 的姿态（沙箱即安全），但静默失败对普通用户是坏体验，所以补 Zed/Claude 的询问层 |
| 4 | **凭证链路只做 MITM（方案 A）** | base_url 只覆盖模型调用，MCP 与普通 API 仍要 MITM，等于维护两套；A 是市场主流（Docker / srt / E2B / Daytona 全是宿主侧拦截） |
| 5 | **审计在宿主侧生成并 hash 链化** | 判断逻辑与账本一旦进沙箱，VM 快照里就带着可伪造的审计（nono NEP-0007 的结论） |
| 6 | **`dsh` 走"自定义 Agent 命令"通道，不做专项适配** | 产品本来就该支持任意命令；这样 dsh 是不是 Linux 可执行，不再是阻塞项 |
| 7 | **仓库统一 `shaoxier/AIofCage`，所有 PR 合入** | 否则"多名成员提交记录"这条硬性规则不成立 |
| 8 | **明确不做**：内存快照 / fork、多 Agent 编排、Windows | 见 §2.2，写进文档比含糊其辞安全 |
| 9 | **不做阻断式审批弹窗**，改为面板内联 + "允许并重试" | 用户会批准 93% 的权限提示，弹窗多了等于没有；沙箱本身才是控制手段 |
| 10 | **AI 安全解说必做** | 占 15% 权重，且个人桌面端没人做；AI 只解释，不参与拦截决策 |
| 11 | **用量统计 + 上限 + 80% 告警必做** | 商业故事的落点，也对应"38% 的组织已设上限" |

> 以上 8 条不再作为议题讨论。有新信息（例如 dsh 无法在 Linux 运行、或某个 Agent 强制校验 key 格式）时，按 §14 的决策记录追加一条修订，而不是推翻重来。

### 分工建议

| 角色 | 负责 |
|---|---|
| A · 边界 | `gatekeeper` / `runtime` / `vault` / 容器镜像 |
| B · 平台 | `policy` / `audit` / `session` / `service` |
| C · 界面 | GUI 全部页面 + 拦截交互 + 演示录制 |

**如果只有一个人**：按 A → C → B 串行。B 只保留最小集（策略匹配 + 事件流落盘），审计页与策略编辑器降级为 JSON 文件 + 命令行查看。界面必须做，因为它占 50% 权重。

### 两个硬门槛

- **T+4（今天 20:00）Gate 1**：容器内 Codex 用 xapi 跑通一次真实请求（模型调用也走门卫）。过不了先排查 key 格式校验与客户端 OAuth——这是排查题，不是选型题（见 ADR-005）
- **T+56（10/25 深夜）**：冻结特性，只修 bug

---

## 0. 一句话定位

**Agent 在笼子里干活，钥匙永远不进笼子。**

不是"再包一层 Docker"，而是把三件事同时锁死：

1. **文件**——看不见宿主，除了一个工作区目录
2. **网络**——没有出网的路，只有经过门卫的一条
3. **凭证**——真密钥只在宿主钥匙串，笼子里只有占位符

---

## 1. 判分逻辑与定位

### 1.1 评分权重（决定时间分配）

| 维度 | 权重 | 评委能感知到的 |
|---|---|---|
| 商业价值与产品设计 | **50%** | 界面、演示、商业故事 |
| 架构设计 | 20% | 威胁模型站不站得住、边界讲不讲得清 |
| AI 价值呈现 | 15% | AI 帮用户看懂，而不是当防线 |
| AI 工程效率 | 15% | 怎么用 AI 把东西做出来，证据留在仓库里 |

**推论**：核心链路是前提（没有它全是空壳），但**分数主要在"看得见"**。所以门卫从第一行代码起就输出结构化事件流——实时日志面板、用量上限、模拟攻击、AI 解说都是同一份数据的消费方，一份投入换四个展示点。

### 1.2 定位：不宣称"首创"

截至 2026 年 10 月，"沙箱里只放占位符、由代理按域名换真 key"已经是行业标配（Docker sbx、Claude Code 的 credential mask、nono、Infisical、Matchlock、七牛 LAS 云沙箱都在做）。

- **我们说**：把一套已被验证的机制，做成普通人能用、看得见、管得住的产品；并且是**七牛 LAS 密钥注入能力的桌面端**。
- **我们不说**：首创占位符、唯一沙箱。懂行的评委会一眼识破。

### 1.3 与竞品的能力空白

| | 原生桌面 GUI | 中文 | 实时请求日志 | 按 key 用量上限 | AI 安全解说 |
|---|---|---|---|---|---|
| Docker sbx | ✗ | ✗ | 终端文本 | 未见 | 未见 |
| Claude Code / Codex 内置 | ✗ | 部分 | ✗ | ✗ | 部分 |
| nono / Matchlock / sandcat | ✗ | ✗ | 终端文本 | ✗ | ✗ |
| **AIofCage** | **✓** | **✓** | **✓ 颜色区分** | **✓** | **✓** |

空白集中在最后五列。完整调研见 `docs/market.md`。

### 1.4 评委实际在验的四件事

| 评委的问题 | 我们的答案 | 对应实现 |
|---|---|---|
| 这是真沙箱，还是 Agent 启动器？ | 三层边界（文件 / 网络 / 凭证），每层可现场验证 | §5.1、§5.4 |
| 安全策略有真实效果吗？ | 五个攻击脚本，每个对应一起真实事件 | §9 |
| 普通用户会用吗？ | 点三下启动；拦截在面板里看得见，可一键"允许并重试" | §8 |
| Agent 真能跑通吗？ | Codex 在笼子里完成真实任务，模型调用也走门卫 | §5.3 |

### 1.5 "桌面级"的正确定位

约束的是**产品形态与用户**（面向非技术用户的 GUI 产品），不是"隔离必须跑在桌面"。隔离体放本地或云端都成立；本项目选择本地，因为演示不依赖网络。

---

## 2. 目标与非目标

### 2.1 目标（P0，必须交付）

按评分权重排序，前 6 条是"不做就空壳"，后 3 条是"做了才有分"。

1. 一个 macOS 桌面应用：选项目文件夹 → 选 Agent → 一键启动
2. Agent 运行在隔离环境中，只能看到工作区目录
3. 容器无默认路由，唯一出口是门卫边车
4. 真实密钥只存在于 macOS 钥匙串；容器内只有占位符
5. 门卫按"域名 + HTTP 方法 + 路径"判定放行/拦截/注入，并**只替换请求头**
6. 门卫从第一行代码起输出结构化事件流（放行/拦截/注入/用量）
7. 实时请求面板（中文、颜色区分）+ 一键"模拟攻击"（5 个脚本）
8. 按 key 的用量统计、上限、80% 告警
9. AI 安全解说：拦截时用中文解释"发生了什么、像哪起真实事件、建不建议放行"

**降级为 P1（路线图）**：写操作审批弹窗。理由见 §8.4——Anthropic 披露用户会批准 93% 的权限提示，审批疲劳让弹窗形同虚设；沙箱本身才是控制手段，弹窗只做"拦截后的补救入口"。

### 2.2 P1（有时间就做）

- 第二个 Agent（dsh 或 Claude Code，谁先验证通过就做谁）
- GitHub MCP 免填 token
- 退出沙箱时检查 `.claude/`、`.vscode/`、`.git/hooks` 是否被改动（封 Mini Shai-Hulud 式持久化）
- 用自然语言生成白名单草案

### 2.3 非目标（明确不做，写进文档避免被追问）

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
┌──────────────────── 可信：macOS 宿主 ─────────────────────┐
│  · Keychain：真密钥的唯一来源（service: sbx-builderday-key）│
│  · AIofCage.app：GUI + 策略 + 审计账本                     │
└──────────────────────────┬──────────────────────────────────┘
                           │ 带 token 的管理接口
                           │ （仅在创建/启动时下发密钥）
┌──────────────────────────▼──────────────────────────────────┐
│ colima VM                                                   │
│  ┌─────────── 可信（边车，不在沙箱内）───────────┐          │
│  │ gatekeeper 容器                               │          │
│  │  · 双网卡：cage-internal + 默认 bridge        │          │
│  │  · 唯一出网路径                               │          │
│  │  · 真密钥只存在于本进程内存                   │          │
│  │  · 生成 CA，供 agent 容器信任                 │          │
│  └───────────────────┬───────────────────────────┘          │
│                      │ cage-internal（无默认路由）          │
│  ┌───────────────────▼───────────────────────────┐          │
│  │ agent 容器（不可信）                          │          │
│  │  · Codex / Claude Code / dsh                  │          │
│  │  · 只有占位符，没有真密钥                     │          │
│  └───────────────────────────────────────────────┘          │
└─────────────────────────────────────────────────────────────┘
```

**注意**：密钥的存放地在宿主 Keychain，运行地在 gatekeeper 进程内存。gatekeeper 虽然是个容器，但它只跑我们自己的二进制、不执行任何 Agent 代码，且与 agent 不在同一网络命名空间内。

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
│  │                       └── runtime  容器/网络生命周期          │  │
│  └───────────────────────────────┬──────────────────────────────┘  │
│                                  │ 管理接口（token 鉴权）           │
│  ~/CageWorkspace  ──── colima mounts 白名单（VM 层）                │
└──────────────────────────────────┼─────────────────────────────────┘
                                   │
┌──────────────────── colima VM (vz) ┼────────────────────────────────┐
│                                    ▼                               │
│   gatekeeper 容器  ── eth0: bridge（出网）                          │
│      · MITM + 策略 + 注入          eth1: cage-internal（:8888）      │
│      · 唯一出口，同时是唯一入口                                      │
│           ▲                                                        │
│           │ 无默认路由，只能到达 gatekeeper                          │
│   agent 容器（只连 cage-internal）                                  │
│     HTTP_PROXY=http://gatekeeper:8888                               │
│     Codex / Claude Code / dsh                                       │
│     ~/CageWorkspace/<project> → /work                               │
└────────────────────────────────────────────────────────────────────┘
```

**为什么不需要单独的 relay**：gatekeeper 自己就是双网卡边车——它同时接入 `cage-internal` 和默认 bridge，因此既能在内网上被 agent 访问，又能出网。多一个 socat relay 只是多一跳、多一个攻击面。

### 4.2 一次请求的完整链路

```
Agent 发起 HTTPS 请求
  → 容器无默认路由，只能连 gatekeeper:8888
  → gatekeeper 解 CONNECT，按域名/方法/路径匹配策略
      ├─ 未命中白名单  → 403 + 审计 DENY + GUI 弹窗
      └─ 命中白名单    → MITM 解密 → 覆盖 Authorization 为真密钥
                        → 经 eth0 转发上游 → 响应脱敏 → 审计 ALLOW/INJECT
```

**注意**：gatekeeper 是容器，不是宿主进程。所以"真密钥"从 Keychain 读出后要经管理接口送进 gatekeeper 内存——这是一次本地网络传输，走 docker bridge，不出机器。这一条必须写进架构文档，否则会被问"密钥是不是又进容器了"。

**回答口径**：密钥的**持久化位置**只有 macOS Keychain；gatekeeper 内存是运行期持有，进程退出即消失，且 gatekeeper 内不运行任何 Agent 代码。

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

**最终拓扑：gatekeeper 自己就是那个双网卡边车。**

```bash
# 1) 内部网络：无默认路由
docker network create --internal cage-internal

# 2) gatekeeper：双网卡，唯一入口 + 唯一出口
docker run -d --name cage-gatekeeper \
  --network bridge \
  --cap-drop ALL --security-opt no-new-privileges \
  -e GATEKEEPER_TOKEN=<每次启动随机生成> \
  aiocage/gatekeeper:dev            # 监听 0.0.0.0:8888，同时提供管理接口
docker network connect cage-internal cage-gatekeeper --alias gatekeeper

# 3) agent：只连内部网络
docker run -it --name cage-agent \
  --network cage-internal \
  --user 1000:1000 \
  --read-only --tmpfs /tmp --tmpfs /home/agent \
  --cap-drop ALL --security-opt no-new-privileges \
  -v ~/CageWorkspace/<project>:/work \
  -e HTTP_PROXY=http://gatekeeper:8888 -e HTTPS_PROXY=http://gatekeeper:8888 \
  -e NO_PROXY=localhost,127.0.0.1,gatekeeper \
  -e NODE_EXTRA_CA_CERTS=/etc/aiocage/ca.crt \
  aiocage/agent:dev
```

**验收标准**：
- 容器内 `curl -sS https://xapi.qiniu.io/v1/models` 走通（白名单内）
- `curl --noproxy '*' https://xapi.qiniu.io` 必须失败（无路由）
- `ip route` 无 default
- 容器内 `env | grep -i -E 'key|token'` 只有 `cage-xxxxxxxx`

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
  → 域名白名单？否 → 403 + DENY 事件 + 面板标红
  → 是 → MITM
       → 匹配规则（host → method → path）
       → 决策：deny / allow / inject
       → inject：从 vault 取密钥，**覆盖**（Set，非 Add）Authorization
       → 该域名配了密钥但请求带的是非占位符凭证 → 剥离或拒绝（铁律 2）
       → 剥离入站 Proxy-Authorization / X-Forwarded-*
       → 转发
响应
  → 3xx 且 Location 不在白名单 → 不跟随，记 DENY（铁律 3）
  → 剥离 WWW-Authenticate（防止容器内客户端自行发起 OAuth）
  → 脱敏：响应体中出现真密钥则替换为 [REDACTED]
  → 记录体积、状态码、耗时、usage tokens
```

**五条铁律**（每条都对应一起真实攻击，写进 README 的威胁模型）：

1. **只替换请求头，永不碰请求体。** Agent 可能把占位符写进发往白名单域名的请求体（例如 issue 正文）；替换请求体等于替攻击者把真 key 公开发出去。
2. **配了密钥的域名只认占位符。** 请求里出现任何非占位符的凭证 → 剥离或拒绝。这条堵的是 Rehberger 演示的"用攻击者自己的 key 调 Files API 外传"——纯域名白名单挡不住，因为 `api.anthropic.com` 必须放行。
3. **不自动跟随重定向。** 3xx 指向非白名单域名时不跟，直接记录并拒绝。
4. **策略只能由桌面应用经管理接口下发**，绝不读取项目目录里的配置文件。防止项目内 `.claude/settings.json` 篡改策略。
5. **日志只记占位符的名字，不记 key 的值**；默认不记请求/响应体、不记 header 原文。只记 `method / host / path / decision / rule / bytes / tokens`。

### 5.3 凭证链路（本项目最关键的设计）

**问题**：容器里没有密钥，但 Agent 连模型 API 也要鉴权。

### 5.3.1 已验证的结论（2026-10-22，团队在 Docker sbx 内实测）

- 沙箱环境变量里只有占位符，门卫替换后调用 `https://xapi.qiniu.io/v1` **成功**
- 发往白名单外的域名返回 **403**
- 在沙箱的文件系统与环境变量里搜索真 key，**0 处命中**

这条实测把"能不能做"变成了"怎么做"，是本项目最有力的证据，演示时必须复现。

### 5.3.2 比赛用的模型端点

**不是**官方 `api.anthropic.com` / `api.openai.com`，而是：

```
https://xapi.qiniu.io/v1        （OpenAI 兼容，/chat/completions 与 /responses 都可用）
模型：gpt-5.5 / gpt-5.6-sol / gpt-5.6-terra / gpt-6-astra / gpt-6-sol / gpt-6.1-sol
```

Codex 走自定义 provider（容器内 `~/.codex/config.toml`）：

```toml
model = "gpt-6-astra"
model_provider = "qiniu"

[model_providers.qiniu]
name = "Qiniu Builder Day"
base_url = "https://xapi.qiniu.io/v1"
env_key = "OPENAI_API_KEY"      # 容器内只有占位符，由门卫替换
wire_api = "responses"          # /chat/completions 也可用
```

**Codex 版本必须 ≥ 0.161.0**：0.149.1 不认识 gpt-6 系列，会报 `Model metadata … not found`，且 `/model` 列不出模型——因为模型列表来自程序自带目录，不读中转站的 `/v1/models`。本机已装 0.162.0，满足。

**主演示 Agent 定为 Codex**：它在 xapi 上已跑通。Claude Code 的认证方式（`CLAUDE_CODE_OAUTH_TOKEN` 占位符 + 注入）仍待验证，作为第二顺位。

### 5.3.3 密钥来源

比赛 key 存在 macOS Keychain，service 名固定：

```bash
security find-generic-password -a "$USER" -s sbx-builderday-key -w
```

**红线**：这个值不得写进代码、仓库、日志、截图。提交前扫描 `sk-`、`ghp_`、`cage-`（占位符除外）。

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
      { "host": "xapi.qiniu.io",      "methods": ["POST"], "decision": "inject", "secret": "builderday" },
      { "host": "registry.npmjs.org", "methods": ["GET"],  "decision": "allow" },
      { "host": "api.github.com",     "decision": "inject", "secret": "github" },
      { "host": "*.githubusercontent.com", "decision": "deny" }
    ]
  },
  "commands": {
    "default": "allow",
    "ask": ["git push *", "rm -rf *", "curl *"],
    "deny": ["sudo *", "ssh *"]
  },
  "secrets": {
    "builderday": {
      "placeholder": "cage-a1b2c3d4",
      "inject": ["xapi.qiniu.io"],
      "header": "Authorization",
      "prefix": "Bearer ",
      "source": "keychain:sbx-builderday-key"
    },
    "github": {
      "placeholder": "cage-e5f6g7h8",
      "inject": ["api.github.com"],
      "header": "Authorization",
      "prefix": "Bearer ",
      "source": "keychain:aiocage-github"
    }
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

### 8.4 拦截的呈现：面板内联，不做阻断式弹窗

**决策依据**：Anthropic 披露用户会批准 **93%** 的权限提示——弹窗多了等于没有弹窗。所以控制手段是沙箱本身，弹窗只做"拦截之后的补救入口"。

**默认行为**：

1. 门卫拒绝 → Agent 收到结构化错误（`blocked by policy: host not allowed`），它自己可以换路子或向用户报告
2. 面板里出现一行红色事件（不打断会话）
3. 用户点这一行 → 侧边展开"允许并重试"，三个作用域

```
┌─ 请求被拦截 ─────────────────────────────── 16:04:12 ─┐
│ ✖ api.collector.example.com:443        POST /v1/upload │
│   规则：default-deny  来源：Codex  体积：0 B           │
│   [ 允许并重试 ▾ ]   [ 加入永久拒绝 ]   [ 查看详情 ]   │
└────────────────────────────────────────────────────────┘
```

点"允许并重试"后展开：

```
┌────────────────────────────────────────────────────────┐
│  允许 Codex 访问 api.collector.example.com？           │
│  · 该域名不在允许列表内                                │
│  · 重试的请求会带上原请求体                            │
│                                                        │
│  [ 仅本次 ]   [ 本会话 ]   [ 永久（写入策略） ]        │
│  Esc 取消                          此决定写入审计 →    │
└────────────────────────────────────────────────────────┘
```

**作用域语义**：

| 作用域 | 生效范围 | 存储位置 |
|---|---|---|
| 仅本次 | 单次请求，不落盘 | 内存 |
| 本会话 | 沙箱停止即失效 | 会话元数据 |
| 永久 | 写入项目策略，下次启动仍生效 | 策略文件（经应用下发） |

**红线**：只能把 `ask`/`deny` 提升为"本次/本会话"，**永久放行必须显式写策略**，不能在弹窗里一键完成；`deny` 规则不可被弹窗覆盖。

### 8.5 Audit

表格列（8 列，可横向滚动）：

`时间 | 决定 | 动作 | 资源 | 会话 | 规则 | 原因 | 事件 ID`

- 决定列：`✔ 放行` / `✖ 拒绝` / `● 注入` / `? 待确认`
- 拒绝行：左侧 3px 红色边 + 淡红底；颜色之外必须有图标与文字（色盲友好）
- 展开抽屉：完整命令/URL、命中规则链、密钥名称（值打码 `gho_GCaw…43qy`）、耗时、字节数、hash 链前后指针
- 顶部：实时开关、暂停、筛选（决定/动作/会话/时间）、导出 JSONL/CSV

---

## 9. 演示脚本（8 幕，约 6 分钟）

顺序按"政策 → 新闻 → 点三下 → 五次攻击 → AI 解说 → 边界"。每个攻击按钮对应一起有出处的真实事件，比功能清单更能拿下"商业价值与产品设计"的 50%。

**幕 0 · 政策与威胁（40s）**
CNCERT 2026-03 的三条建议（容器隔离、不在环境变量明文存密钥、操作日志审计）与我们的三层设计逐条对应；紧接着放 10-21 LiteLLM 1.107.0 投毒——窃取器专偷环境变量里的 AI key。

**幕 1 · 点三下启动（60s）**
选文件夹 → 选 Codex → 启动。打开终端，Codex 完成一次真实对话（模型调用也走门卫）。

**幕 2 · 偷 key（40s）**
按钮 1：`env | grep -i key`，只拿到 `cage-xxxxxxxx`。对应 LiteLLM / tensorlake。

**幕 3 · 外传（40s）**
按钮 2：向仿冒域名 POST，403 + 面板标红。对应 LiteLLM 用的 `litellm.dev`。

**幕 4 · 绕过代理（40s）**
按钮 3：`unset HTTPS_PROXY; curl ...`，直连失败（无默认路由），DNS 也失败。对应 Infisical 指出的"agent 可以 unset 代理变量"。

**幕 5 · 借白名单外传（60s）** ← 技术差异点
按钮 4：用攻击者自带的 key 调用模型 API 上传数据。门卫发现"该域名配了密钥，但请求带的是非占位符凭证" → 剥离凭证。对应 Rehberger 的 Files API 外传。

**幕 6 · 删家目录（40s）**
按钮 5：`rm -rf ~/`。宿主机毫发无损。对应 2025-12 的真实事故。

**幕 7 · AI 解说（30s）**
点开任一拦截事件 → 中文解释"发生了什么、像哪起事件、建不建议放行"。解说调用本身也走门卫。

**幕 8 · 边界（30s）**
一页讲清三条边界：容器隔离弱于 microVM；门卫防偷不防用（白名单内挪数据挡不住）；项目目录是一座桥（退出时检查 `.claude/`、`.git/hooks`）。主动讲边界是加分项。

## 10. 任务拆分与排期

**时间基准**：T+0 = 2026-10-23 16:00，截止 2026-10-26 09:00（65 小时，含睡眠）。

### Gate 1（T+4，今天 20:00）：链路必须通

- [ ] colima 起来（`--cpu 4 --memory 6`），`mounts: [~/CageWorkspace]`
- [ ] `cage-internal` + relay + agent 三件套跑通
- [ ] 容器内 `curl https://api.github.com` 走门卫成功
- [ ] `claude --version` 在容器内可执行
- [ ] **方案 A 验证**：占位符 + MITM 注入后，容器内 Codex 能否用 xapi 拿到模型响应（团队 10/22 已在 sbx 中验证过同一路径）
- [ ] **绕过验证**：容器内 `unset HTTPS_PROXY` 后直连失败；DNS 查询也失败（已验证：内部网络外部 DNS 返回 SERVFAIL）
- [ ] **占位符验证**：`env | grep -i -E 'key|token'` 只有 `cage-xxxxxxxx`；全盘搜真 key 0 处命中

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
├── cmd/
│   └── gatekeeper/main.go      # 门卫容器的 entrypoint（独立二进制）
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
│   ├── agent/Dockerfile        # 预置 CA、Node、codex、claude
│   └── gatekeeper/Dockerfile   # 只含门卫二进制，无 shell
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

### ADR-002 门卫本身就是双网卡边车，不额外加 relay

- **决定**：`cage-internal` + 一个同时接入 bridge 与 internal 的 gatekeeper 容器。它既是唯一入口，也是唯一出口。
- **被否**：
  - **独立 relay + 宿主门卫**：需要 host↔VM 网络（`host.docker.internal` / gvproxy），多一跳、多一个进程、多一处密钥暴露面。
  - `-p` 端口发布：**实测在内部网络下无效**，`docker port` 为空。
  - unix socket：colima 通过 virtiofs 共享文件系统，**unix socket 不能跨 VM 边界**。
- **理由**：一个容器同时具备内网地址和出网能力，就能同时完成"被 agent 访问"和"访问外网"。这是最少组件数的解。
- **代价**：gatekeeper 成为单点——它挂了整个沙箱就没网。可接受，因为它的崩溃同时也意味着策略不可用，fail-closed 正是我们要的。

### ADR-003 密钥经管理接口下发到 gatekeeper 内存

- **决定**：桌面应用从 Keychain 读出密钥后，通过带 token 的管理接口发给 gatekeeper，只存在于 gatekeeper 进程内存。
- **被否**：
  - gatekeeper 直接读 Keychain：容器内访问 macOS Keychain 需要挂载宿主 socket / 走 `security` 命令，等于把钥匙串暴露给容器，得不偿失。
  - 写进容器环境变量：会出现在 `docker inspect`、`/proc/1/environ`，且可能被快照留存。
  - 挂载密钥文件：文件系统里出现真值，违反"沙箱内 0 处命中"的验收标准。
- **理由**：真密钥只在两个地方存在——Keychain（持久）和 gatekeeper 内存（运行期）。agent 容器内始终为零。
- **代价**：多一个管理接口需要鉴权（每次启动随机生成 token，仅 bind 在 bridge 侧）。
- **答辩口径**：密钥的**持久化位置**只有 Keychain；gatekeeper 只是运行期持有，且其中不运行任何 Agent 代码。

### ADR-004 默认拒绝 + 询问，而不是静默拒绝

- **决定**：未命中白名单的网络请求挂起，弹窗让用户三选一。
- **被否**：Docker sbx 的"无提示、直接阻断"。
- **理由**：Docker 的立场是"沙箱即安全，不需要提示"，那是给开发者的。我们的用户是普通桌面用户，`curl: (56) Recv failure` 对他们等于软件坏了。
- **代价**：多一套审批队列与作用域状态机。
- **红线**：弹窗只能把 `ask` 提升为 `allow`，**永远不能绕过 `deny`**。

### ADR-005 凭证只做 MITM，不接受 base_url 替代

- **决定**：宿主侧自签 CA + MITM，覆盖 Authorization。
- **被否**：`ANTHROPIC_BASE_URL` / Codex `model_providers.base_url` 指向本地网关。
- **理由**：base_url 只覆盖模型调用；GitHub MCP、`pip`、`curl` 仍然需要 MITM。选 base_url 等于最终还是要写 MITM，只是多写一套。
- **代价**：容器内必须装 CA，且要覆盖 Node / Python / Go / curl 四套信任环境。
- **踩坑记录**：goproxy 自带公开 CA 私钥，必须自签；goproxy 默认读取宿主 `HTTPS_PROXY`，必须清空。

### ADR-006 `dsh` 不做专项适配

- **决定**：产品支持"自定义 Agent 命令"，`claude` / `codex` / `dsh` 都只是配置项。
- **被否**：为每个 Agent 写适配器与参数模板。
- **理由**：dsh 的运行形态（Linux 可执行 / 仅 macOS）在文档写作时未知。把它变成阻塞项，等于用一个未知数换掉整个排期。
- **代价**：对三个 Agent 都不做深度集成（例如不解析它们的 session 格式）。可接受——我们卖的是边界，不是 Agent 集成。
- **风险**：若 dsh 仅 macOS 原生二进制，则它无法进 Linux 容器，届时在 UI 上明确标注"该 Agent 仅支持本机模式（无沙箱）"，而不是假装支持。

### ADR-007 明确不做快照 / 多 Agent / Windows

- **决定**：不在 60 小时内做，且写进文档。
- **理由**：快照需要 userfaultfd + COW（E2B 的量级）；多 Agent 编排只有 Daytona / Runloop / K8s 做了，各自是一个平台；WSL2 的 Landlock 版本差异会毁掉策略一致性。
- **写法**：在"非目标"里给出理由，比被评委问出来强。

---

## 15. 商业化叙事

**一句话**：企业分发的是用量，不是 key。

| 模式 | 怎么收费 | 卖什么 | 定位 |
|---|---|---|---|
| A · 个人免费 + 团队订阅 | 个人免费开源；团队约 30–60 元/人/月（推算） | 集中托管 key、统一下发白名单与 MCP 策略、按人设额度、导出审计 | 获客 |
| B · 七牛「安全 Agent 编程包」 | 按席位或 token 包，从 token 收入分成（比例待谈） | 成员机器上没有真 key，只有占位符；七牛按 key 配额与计费 | **主线** |
| C · 私有化企业版 | 按年授权 | 门卫 + 管理台部署内网，对接 SSO / SIEM | 长期 |

**回答"Claude Code 自带沙箱、sbx 也免费，为什么用你"**：

1. 一个入口统一管多种 agent（Codex / Claude Code / dsh）
2. 非技术用户也能用的原生图形界面
3. 模型 key 与 MCP key 由同一个门卫注入
4. 国内模型开箱即用，日志与用量看得见

**三层用户**：个人开发者与刚开始用 agent 的非程序员 → 小团队负责人（不想把 key 发出去）→ 金融政企（TC260 指引与强制国标压力）。

**不报 TAM**：桌面 agent 沙箱没有可靠的独立市场数据。自下而上算：编程 agent 付费用户数 × 每人安全附加费。

> 定价与分成比例均为推算，没有国内付费意愿调查支撑，答辩时要主动说明。
