# AIofCage 早期架构设计草案

> 本文保留早期 v3 设计过程，包含已被后续实现替换的方案与完成状态。当前部署、模块职责、配置一致性与故障恢复请阅读[当前架构](architecture-current.md)，不要使用本文的旧清单判断当前产品完成度。

> 桌面级 AI Agent 安全沙箱：让 Claude Code、Codex、dsh 放手干活，密钥永远不进笼子。
> Qiniu Builder Day 2026 · 赛道一（产品研发组）
>
> 状态：设计修订 v3 草案 · 2026-10-23；以下能力均为设计目标，未完成的实现不得标为已验证。
> 前置文档：[产品目标、设计冻结与验收](requirements-and-acceptance.md)。按最新开发安排先搭可构建入口与接口骨架，当前实现范围见[模块说明](modules.md)，不表示 G0—G5 已通过。真实安全能力仍须先验证对应关口并记录设计决策。MCP 扩展属于待验证架构候选。

---

## 0.1 给队友的一页速览

### 当前执行的决定

| # | 决定 | 一句话理由 |
|---|---|---|
| 1 | **桌面应用 + 独立门卫容器**，不增加宿主 daemon | 门卫负责请求判定、注入、额度和事件；应用负责会话、钥匙串和审计落盘 |
| 2 | **专用 Colima 实例**：profile `aiofcage`，context `colima-aiofcage` | vz + virtiofs，4 核 / 6 GiB / 40 GiB，仅共享 `~/CageWorkspace:w`；不修改用户默认实例 |
| 3 | **双网卡门卫**：Agent 仅连内部网络，门卫连接内部网络和出网 bridge | 代理端口与管理端口分离；管理端口只向宿主回环发布 |
| 4 | **模型走 MITM，MCP 走协议网关** | 门卫同一容器内分开监听；MCP 网关代持上游认证并执行工具权限，HTTP 路径授权不能代替它 |
| 5 | **宿主追加写 JSONL 审计** | 门卫生成网络事件，应用订阅并落盘；hash 链降为路线图 |
| 6 | **多 Agent 使用启动配置** | Codex、Claude Code、dsh 分别验证启动、认证和真实任务；自定义命令入口不代表已经兼容 |
| 7 | **统一向 `shaoxier/AIofCage` 提交 PR** | 集中保存团队开发与交付记录；队友原创初始化代码属于团队开发成果 |
| 8 | **明确范围**：暂不做内存快照、多 Agent 编排、Windows | 非目标见 §2.3，不能把未覆盖能力写成已实现 |
| 9 | **未知请求先拒绝，再由用户决定是否授权后续访问** | 不挂起请求、不缓存请求体、不自动重放；客户端重新发起请求 |
| 10 | **AI 安全解说为增强项** | 核心目标通过后再做；不参与授权、不自动更改策略 |
| 11 | **门卫执行用量上限和 80% 告警** | 团队增强目标，基础议题能力优先；启用后应用崩溃不解除限制，额度状态不完整时不恢复转发 |
| 12 | **MCP 四类基线列入核心目标** | HTTP Token、OAuth、无密钥 stdio、数据库密码分别验证；有凭据连接器在 Agent 外运行 |

> 当前正文按上述决定执行；§14 中被取代的 ADR 只保留历史背景。新增结论必须附验证环境与证据，不能把别的沙箱上的实验当作本项目验收结果。

### 分工建议

| 角色 | 负责 |
|---|---|
| A · 边界 | `gatekeeper` / `runtime` / `vault` / 容器镜像，接入策略与额度执行 |
| B · 平台 | `policy` 模型 / `events` / `audit` / `session` / `service`，与 A 对齐判定接口 |
| C · 界面 | GUI 全部页面 + 拦截交互 + 演示录制 |

**开发前置**：先完成目标文档的范围、技术关口与工作量判断，记录设计冻结。之后 A 与 B 约定的策略、管理 API、事件字段作为共同接口，C 基于同一接口开发。核心安全链路和 MCP 通过后再扩展展示能力。商业价值与产品设计的 50% 权重不等于界面单项占 50%。

### 两个硬门槛

- **正式编码前**：G0—G5 的关键路线有可复现证据，目标、接口、风险和实际分工完成冻结；未经验证的旧排期不能替代这个条件。
- **2026-10-26 00:00，T+56**：冻结特性；07:00 前提交，09:00 为官方截止。时间均为上海时区。

---

## 0. 一句话定位

**Agent 在笼子里干活，钥匙永远不进笼子。**

不是"再包一层 Docker"，而是把三件事同时锁死：

1. **文件**——看不见宿主，除了一个工作区目录
2. **网络**——没有出网的路，只有经过门卫的一条
3. **凭证**——上游秘密持久化在宿主钥匙串，宿主传输时短暂持有，门卫与可信连接器按服务在运行期持有；Agent 只有模型占位符和受限的内部 MCP 调用凭据

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

### 1.3 产品设计目标

| | 原生桌面 GUI | 中文 | 实时请求日志 | 按 key 用量上限 | AI 安全解说 |
|---|---|---|---|---|---|
| Docker sbx | ✗ | ✗ | 终端文本 | 未见 | 未见 |
| Claude Code / Codex 内置 | ✗ | 部分 | ✗ | ✗ | 部分 |
| nono / Matchlock / sandcat | ✗ | ✗ | 终端文本 | ✗ | ✗ |
| **AIofCage（设计目标，尚未实现）** | 计划支持 | 计划支持 | 计划支持 | 计划支持 | 计划支持 |

表格用于整理调研与设计方向，不证明其他产品没有相关能力。完整调研见 `docs/market.md`，功能验收以可重复执行的测试和演示为准。

### 1.4 评委实际在验的四件事

| 评委的问题 | 我们的答案 | 对应实现 |
|---|---|---|
| 这是真沙箱，还是 Agent 启动器？ | 三层边界（文件 / 网络 / 凭证），每层可现场验证 | §5.1、§5.4 |
| 安全策略有真实效果吗？ | 五个攻击脚本，每个对应一起真实事件 | §9 |
| 普通用户会用吗？ | 安装、导入、认证、启动、拒绝、停止清理完整流程，无手改 Docker/CA/JSON | 目标文档 §5、本文 §8 |
| Agent 真能跑通吗？ | Codex、Claude Code、dsh 分别有真实任务和 MCP 调用证据 | 目标文档 §3、本文 §5.3 |

### 1.5 "桌面级"的正确定位

本项目选择 macOS 图形界面和本地隔离环境，降低用户配置成本。模型 API 和远程 MCP 仍依赖网络；本地部署不等于离线运行。云端隔离是否满足比赛要求，需另向组织方确认。

---

## 2. 目标与非目标

### 2.1 议题基线（P0，必须交付）

先完成题目要求对应的能力，再补展示功能；未通过兼容性验收的客户端和 MCP 必须明确标记。

1. 一个 macOS 桌面应用：选项目文件夹 → 选 Agent → 一键启动
2. Agent 运行在隔离环境中；宿主目录仅暴露选定工作区，容器系统文件仍正常可见
3. 容器无默认路由，唯一出口是门卫边车
4. 秘密持久化在 Keychain，可信门卫/连接器按需运行期持有；Agent 不含上游秘密，仅有模型占位符和受限 MCP 内部凭据
5. 门卫按"域名 + HTTP 方法 + 路径"判定放行/拦截/注入，并**只替换请求头**
6. 门卫从第一行代码起输出结构化事件流（放行/拦截/注入/用量）
7. 中文活动面板，展示权限、拒绝原因、执行状态与日志缺口；安全反向测试有可复现步骤
8. 首次环境准备至停止清理完整 GUI 流程，用户不用手配 Docker、证书或 JSON
9. MCP 服务、工具、类型化参数与资源权限；注销、过期、重启关闭与跨会话隔离
10. HTTP Token、OAuth、无密钥 stdio、数据库密码四类 MCP 基线均有真实调用证据
11. 对 Codex、Claude Code、dsh 分别登记镜像、版本、认证、真实任务和 MCP 调用验收结果，不能以启动入口替代兼容性验证

**题目覆盖边界**：一个 Agent 和一个 HTTP MCP 跑通只是首个里程碑。四类基线是团队的最低目标，也不能证明覆盖无限的“所有主流”；范围疑义需组织方确认。OAuth、密码连接器或指定 Agent 缺失是核心覆盖缺口，不能通过改列 P1 消除。写操作的交互式审批可延后，未经授权的写工具默认拒绝。

### 2.2 P1（有时间就做）

- 按 key 的用量统计、上限和 80% 告警（团队增强项，执行语义见 §7；不能把估算金额当精确账单上限）
- AI 安全解说与一键攻击演示；验收用的安全反向验证仍属 P0
- hash 链与外部锚定的审计校验
- 受控命令执行代理和逐进程审计，明确不能仅靠 shell 包装或字符串黑名单
- 退出沙箱时检查 `.claude/`、`.vscode/`、`.git/hooks` 是否被改动（提示风险，不保证识别全部持久化）
- MCP 写操作的交互式审批；工具/参数默认拒绝和 OAuth 基线属于 P0
- 用自然语言生成白名单草案

### 2.3 非目标（明确不做，写进文档避免被追问）

| 不做 | 理由 |
|---|---|
| 自研 hypervisor / microVM | Docker、E2B、microsandbox、Blaxel 全都站在别人的 VMM 上；这是 6 个月的项目 |
| 通用 TLS MITM 的完整兼容（证书固定、gRPC、QUIC） | 只覆盖 HTTP(S) 白名单域名；QUIC/UDP 一律阻断 |
| 无边界兼容任意 MCP 和任意凭据协议 | 先按目标文档验证四类基线；OAuth 必须有可行路线，注册方式按服务选择，不承诺所有 DCR/客户端元数据变体 |
| 内存级快照 / fork | 需要 userfaultfd + COW；只做"停止/启动保留磁盘" |
| 多 Agent 编排、跨沙箱通信 | 一个沙箱一个 Agent 已经是完整故事 |
| Windows / WSL2 | 只支持 macOS + Apple Silicon |
| 第一版高级策略编辑器 | 使用预设和会话授权；自然语言规则草案列为 P1，用户确认后才能下发 |
| 防内核 0day | 威胁模型不含内核提权；见 §3.3 |

---

## 3. 威胁模型

### 3.1 攻击面

| 攻击者 | 手法 | 后果 |
|---|---|---|
| 被投毒的仓库 / README / 网页 | 提示词注入，诱导 Agent 执行命令 | 删文件、外泄密钥、反弹 shell |
| Agent 自身的错误 | 误删、误改、误 push | 数据损坏 |
| 恶意 MCP server | 工具返回值投毒 / 自行外联 | 数据外泄 |
| 供应链 | `npx -y` 拉取任意包 | 沙箱内代码可破坏已授权项目；可信连接器若被投毒还可窃取自身凭据，不能按普通 Agent 代码处理 |

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

**注意**：上图是最小模型链路。扩展 MCP 后，Keychain 仍是上游秘密的唯一持久化位置，门卫/MCP 网关与可信连接器按服务在运行期持有，宿主传输时短暂持有。连接器是新增可信代码，不能运行项目传入的任意命令；完整边界见目标文档 §4。Agent 与各可信容器不共享进程/网络命名空间，连接器网络不接入 Agent。

**安全承诺的条件**：

- 假设攻击者可在 Agent 容器内任意执行代码，但尚未突破容器隔离。此时不得读到门卫内存、管理凭证、CA 私钥或未挂载宿主文件，也不得绕过网络出口。
- Colima profile `aiofcage` 的共享目录只包含 `~/CageWorkspace`，用于减少暴露的宿主文件；这不构成对任意逃逸攻击的安全证明。
- Agent 和门卫位于同一 VM。若攻击者逃逸并取得 VM 的高权限，可能读取门卫内存和改写网络，不能继续承诺密钥与出口安全。
- 工作区为可写挂载时，Agent 能修改或删除其中的数据；容器只读根文件系统不保护可写工作区。

### 3.4 不做防护的场景

- 门卫或可信连接器进程被攻破，或容器逃逸后取得 Colima VM 高权限
- macOS 内核 / Virtualization.framework 的 0day
- 用户主动授予危险访问或把敏感文件放进工作区
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
│  │   internal/service  ──┬── policy   策略编辑与校验             │  │
│  │                       ├── vault    Keychain 适配              │  │
│  │                       ├── audit    事件订阅与 JSONL 落盘      │  │
│  │                       └── runtime  容器/网络生命周期          │  │
│  └───────────────────────────────┬──────────────────────────────┘  │
│                                  │ 管理 API + GET /events（鉴权）   │
│  ~/CageWorkspace  ──── colima mounts 白名单（VM 层）                │
└──────────────────────────────────┼─────────────────────────────────┘
                                   │
┌──────────────────── colima VM (vz) ┼────────────────────────────────┐
│                                    ▼                               │
│   gatekeeper 容器  ── bridge（出网；管理端口 8899）                  │
│      · MITM + 策略执行 + 注入      cage-internal（代理端口 8888）   │
│      · 额度执行 + 结构化事件；不执行 Agent 代码                     │
│           ▲                                                        │
│           │ 无默认路由；代理只开放转发所需端口                       │
│   agent 容器（只连 cage-internal）                                  │
│     HTTP_PROXY=http://gatekeeper:8888                               │
│     Codex / Claude Code / dsh                                       │
│     ~/CageWorkspace/<project> → 同一绝对路径                         │
└────────────────────────────────────────────────────────────────────┘
```

**模型链路无需单独 relay**：gatekeeper 同时接入 Agent 内网和出网 bridge。MCP 扩展在同一容器增加内部 `8890` 监听，按需另接私有连接器网络；连接器不与 Agent 共网，完整拓扑见目标文档 §4.2。多会话必须有各自网络和门卫，本文 `cage-internal` 是单会话示意名。

### 4.2 一次请求的完整链路

```
Agent 发起 HTTPS 请求
  → Agent 无直接出网路径，经 gatekeeper:8888 请求
  → CONNECT 阶段检查目标域名与端口，拒绝未授权目标
  → MITM 后检查实际目标、HTTP 方法和路径
      ├─ 显式拒绝 / 未授权 → 403 + 拒绝事件，客户端自行结束请求
      └─ 获准 → 校验占位符 → 检查额度并预留 → 注入密钥 → 转发
  → 门卫输出脱敏事件 → 应用订阅 /events → JSONL + 活动面板
```

**注意**：gatekeeper 是容器，不是宿主进程。所以"真密钥"从 Keychain 读出后要经管理接口送进 gatekeeper 内存——这是一次本地网络传输，走 docker bridge，不出机器。这一条必须写进架构文档，否则会被问"密钥是不是又进容器了"。

**回答口径**：上游秘密的**持久化位置**只有 macOS Keychain；模型秘密在门卫内存中运行期持有。MCP 还可能由可信连接器持有本服务凭据，不能继续声称所有秘密只存在于门卫。门卫和连接器都不运行 Agent 提供的任意代码。

### 4.3 管理接口和事件回传

背景：桌面应用必须拿到门卫的真实判定结果，才能展示拦截、额度和 AI 解说。管理通道与 Agent 代理通道分离，不以终端输出代替事件。

| 接口 | 作用 | 安全约束 |
|---|---|---|
| `GET /health` | 确认门卫版本和就绪状态 | 与管理接口相同鉴权，不返回密钥 |
| `PUT /config` | 原子下发策略、凭证和额度状态 | 先校验版本与字段，成功后整体生效；请求体不得写日志 |
| `GET /events` | SSE 事件流，应用主动连接 | `Authorization: Bearer <管理 token>`，token 不放 URL |
| `GET /usage` | 查询当前额度和在途预留 | 门卫为执行方，应用不能用本地显示值代替实际计数 |

MCP 扩展沿用同一管理鉴权与配置版本：`PUT /config` 增加固定服务绑定、工具/资源策略及内部凭据有效期，真实凭据与普通元数据分开处理且不回读；`GET /health` 增加服务就绪摘要。停止会话/断开服务通过原子配置更新撤销绑定，执行方确认后才显示已断开。OAuth 开始、回调与刷新属于宿主专用服务，不接受 Agent 的任意上游 URL。完整字段、错误码和原子更新样例在 G5 冻结前补齐，不把本段视为已实现接口。

- 门卫代理监听内部网络的 `8888`；管理监听单独的 `8899`，仅绑定 bridge 一侧地址，经端口转发发布到宿主 `127.0.0.1`。绑定地址由运行时探测，不依赖 `eth0/eth1` 顺序。
- MCP 数据入口 `8890` 只绑定本会话 Agent 内网，使用会话范围的内部认证，拒绝浏览器的非授权 Origin；管理 token 与上游 token 均不能用作此入口凭据。代理拒绝转发到 `8890`、管理面、连接器网络和其他会话。
- 管理 token 每次启动随机生成，至少 32 字节，仅宿主应用和门卫持有；Agent 不继承它。管理端口不能从 Agent 内网或代理转发访问。
- 门卫每次启动生成 `gatekeeper_run_id`，每条事件带单调递增 `seq`。内存保留最近 1000 条事件；应用带 `Last-Event-ID` 重连，按 `(run_id, seq)` 去重。
- 心跳每 15 秒一次。断线重连从 1 秒退避到最多 30 秒；请求体和密钥不进入重放缓冲区，缓冲区只存脱敏元数据。
- 游标过旧或门卫重启时返回明确的缺口标识，应用记录 `audit.gap`，不能声称审计完整。第一版不保证覆盖应用长期离线期间的全部事件。
- 配置未下发、策略无效、密钥缺失或额度状态未恢复时，不开放转发。应用断线不取消门卫已有策略和额度限制。

例如，应用已落盘 `run-a:120` 后重连，门卫保留 `run-a:100` 至 `run-a:1099`，从 121 补发；若最早只剩 500，则记录 121 至 499 的缺口，再同步当前状态。

### 4.4 分层与职责

| 层 | 职责 | 代码位置 |
|---|---|---|
| L1 交互 | GUI 页面、会话授权、审计表格，不接收明文密钥回读 | `frontend/` |
| L2 服务 | 用例编排、Wails 绑定、事件推送 | `internal/service` |
| L3 领域 | 策略模型、凭证适配、OAuth 状态、MCP 服务绑定、审计与会话 | `internal/{policy,vault,oauth,mcp,audit,session}`（规划） |
| L4 边界 | 门卫 HTTP/MCP 权限执行与事件；宿主编排网络和可信连接器 | `internal/{gatekeeper,runtime,connector,events}`（规划） |
| L5 适配 | Docker CLI、`security` CLI、colima | `internal/{dockerx,keychain}` |

**原则**：密钥输入允许经专用写入接口到 vault，但不得回读到前端、事件或日志。下发服务不可避免地短暂接触密钥字节，应缩短持有时间；门卫内存是可信边界的一部分，不能声称除 Keychain 外任何进程都不持有密钥。

---

## 5. 关键机制设计

### 5.1 专用环境与出网限制

**团队已有的五项实验记录**：仅适用于当时的 Colima / 网络配置，尚缺版本与复现脚本，不视为所有 Docker 环境的普遍结论。

| 结论 | 实测结果 |
|---|---|
| `--internal` 网络无默认路由 | 路由表只有子网路由，无 default |
| `--internal` 仍可访问桥网关 | 可 ping 通 172.x.0.1（那是 VM，不是 macOS） |
| 内部网络下 `-p` 端口发布 | **无效**，`docker port` 为空 |
| 内部网络下 `host.docker.internal` | **DNS SERVFAIL**；直连宿主 IP 被丢弃 |
| 普通 bridge 访问宿主 `127.0.0.1:8899` | **可用**（colima gvproxy 转发） |

**固定环境**：profile `aiofcage`、context `colima-aiofcage`，vz + virtiofs，4 核 / 6 GiB / 40 GiB，唯一共享为 `~/CageWorkspace:w`。启动脚本检查专用实例配置，发现额外共享则停止；不修改 default profile，不全局切换 Docker context。

下面是编排示例，不是已实现的安装脚本。`CAGE_PROJECT` 由应用解析为 `~/CageWorkspace` 内的真实绝对路径，拒绝指向范围外的符号链接；外部目录需先复制或显式导入。`CAGE_CA_CERT` 仅含公有 CA 证书，私钥不得挂载进 Agent。

```bash
# 1) 内部网络：无默认路由
docker --context colima-aiofcage network create --internal cage-internal

# 2) gatekeeper：双网卡，唯一入口 + 唯一出口
docker --context colima-aiofcage run -d --name cage-gatekeeper \
  --network bridge \
  -p 127.0.0.1:8899:8899 \
  --cap-drop ALL --security-opt no-new-privileges \
  -e GATEKEEPER_TOKEN="$CAGE_ADMIN_TOKEN" \
  aiocage/gatekeeper:dev
docker --context colima-aiofcage network connect --alias gatekeeper cage-internal cage-gatekeeper

# 3) agent：只连内部网络
docker --context colima-aiofcage run -it --name cage-agent \
  --network cage-internal \
  --user 1000:1000 \
  --read-only --tmpfs /tmp --tmpfs /home/agent:uid=1000,gid=1000 \
  --cap-drop ALL --security-opt no-new-privileges \
  --memory 4g --cpus 2 --pids-limit 512 \
  --mount "type=bind,src=$CAGE_PROJECT,dst=$CAGE_PROJECT" \
  --mount "type=bind,src=$CAGE_CA_CERT,dst=/etc/aiocage/ca.crt,readonly" \
  --workdir "$CAGE_PROJECT" \
  -e HTTP_PROXY=http://gatekeeper:8888 -e HTTPS_PROXY=http://gatekeeper:8888 \
  -e NO_PROXY=localhost,127.0.0.1,gatekeeper \
  -e NODE_EXTRA_CA_CERTS=/etc/aiocage/ca.crt \
  aiocage/agent:dev
```

门卫启动时等待两个网络接口就绪，代理绑定内部地址、管理绑定 bridge 地址；两个监听器和路由必须隔离。示例中的 token 只进入可信门卫，运行时禁止输出到日志。管理端口经 Colima 转发后的真实宿主监听范围需要检查，不能只依赖 `-p` 字面值。准备 CA、下发配置和事件订阅成功后才启动 Agent。

**验收标准**：
- 容器内经门卫完成一个真实模型请求；`GET /v1/models` 只有显式授权后才能通过
- `curl --noproxy '*' https://xapi.qiniu.io` 必须失败（无路由）
- `ip route` 无 default
- 同时清空大小写代理变量，覆盖 IPv4、IPv6、DNS 外传和自定义 socket 的绕过测试
- Agent 中的密钥相关变量只有占位符，不出现管理 token；配置文件和镜像层也不得有真值
- 从宿主回环可访问管理 API；从 Agent、代理转发和局域网访问均失败
- VM 中仅能看到共享工作区，项目在宿主和 Agent 内为同一绝对路径

### 5.2 门卫代理

**选型**：`github.com/elazarl/goproxy v1.9.2`。实现代理时在 `go.mod` / `go.sum` 固定版本，本次文档修订不代表依赖已经接入。

**必须避开的坑**：

1. **goproxy 自带公开的 CA 私钥**（`GoproxyCa`），不能作为信任根使用。必须自签 CA 并自己构造 `ConnectAction`，禁止直接使用默认 `goproxy.AlwaysMitm`。
2. **goproxy 默认读取宿主 `HTTPS_PROXY` 环境变量**（`Tr.Proxy` 与 `ConnectDial`），会莫名链式转发。必须显式清空。
3. **上游 TLS 必须校验证书**。上游修复见 [goproxy #774](https://github.com/elazarl/goproxy/pull/774)，v1.9.2 已包含修复；禁止降级或设置 `InsecureSkipVerify: true`，用不受信任的上游证书验收。

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

这段代码只示意 CONNECT 阶段的 CA 与初筛，不是完整门卫实现。解密后的每个请求仍需执行 §6.2 的完整授权和目标一致性检查，不能用 `AllowsHost` 代替方法、路径与凭证校验。

**请求处理顺序**：

```
CONNECT
  → 域名白名单？否 → 403 + DENY 事件 + 面板标红
  → 是 → MITM
       → 校验 CONNECT 目标、实际请求目标一致性；阻断私网、回环和管理地址的代理访问
       → 按 host / port / method / path 完整授权，显式 deny 优先
       → 拒绝未授权请求；仅允许的请求进入凭证处理
       → 该域名配了密钥但凭证不是对应占位符 → 直接拒绝，不先注入
       → 额度检查与并发预留 → 从内存凭证表取值 → Set 覆盖认证头
       → 剥离入站 Proxy-Authorization / X-Forwarded-*
       → 转发
响应
  → 不自动跟随 3xx；Location 目标未授权时返回结构化拒绝
  → 日志与事件只取脱敏元数据；认证异常不得回显凭证
  → 记录体积、状态码、耗时、usage tokens，完成额度结算
```

**五条铁律**（每条都对应一起真实攻击，写进 README 的威胁模型）：

1. **只替换请求头，永不碰请求体。** Agent 可能把占位符写进发往白名单域名的请求体（例如 issue 正文）；替换请求体等于替攻击者把真 key 公开发出去。
2. **配了密钥的目标只认对应占位符。** 缺失、重复、错误占位符或外来认证值一律拒绝。显式允许的公共无认证路径须单独定义，不能继承该目标的注入权限；只处理已适配的认证头，不支持的认证形式失败关闭。
3. **不自动跟随重定向。** 3xx 指向非白名单域名时不跟，直接记录并拒绝。
4. **策略只能由桌面应用经管理接口下发**，绝不读取项目目录里的配置文件。防止项目内 `.claude/settings.json` 篡改策略。
5. **日志只记占位符的名字，不记 key 的值**；默认不记请求/响应体、不记 header 原文。只记 `method / host / path / decision / rule / bytes / tokens`。

### 5.3 凭证链路（本项目最关键的设计）

**问题**：容器里没有密钥，但 Agent 连模型 API 也要鉴权。

### 5.3.1 前期实验（2026-10-22，团队在 Docker sbx 内实测）

- 沙箱环境变量里只有占位符，门卫替换后调用 `https://xapi.qiniu.io/v1` **成功**
- 发往白名单外的域名返回 **403**
- 在沙箱的文件系统与环境变量里搜索真 key，**0 处命中**

以上是其他运行环境中的前期实验，本项目需要在 profile `aiofcage` 中独立复现并保存版本、脚本和脱敏结果，才可标为通过。

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

**团队实验中的版本问题**：记录曾出现 0.149.1 不识别所选模型、在较新版本可用的情况；这不是经本项目验证的通用最低版本。上例是历史实验配置，模型、客户端版本和请求协议在 G2 固定并复测，不以本机安装状态算兼容通过。

**首个链路实验优先 Codex**：团队曾在其他环境用 xapi 跑通。Claude Code 的认证方案仍待验证；三个客户端在最终基线中均需验证，不因首个演示选择而降为可选。

### 5.3.3 密钥来源

比赛 key 存在 macOS Keychain，service 名固定：

```bash
security find-generic-password -a "$USER" -s sbx-builderday-key -w
```

**红线**：这个值不得写进代码、仓库、日志、截图。提交前扫描 `sk-`、`ghp_`、`cage-`（占位符除外）。

**排障降级：模型 base_url 指向门卫**

- 若 MITM 兼容性暂时阻塞，可在隔离内网让模型 base_url 指向门卫的专用适配入口；不能暴露为通用任意目标转发接口
- 门卫以明文 HTTP 接收，无需 CA
- 缺点：需要为每个 Agent 写适配；MCP 仍须通过协议网关验收

**模型正常链路使用 MITM。** 排障时先检查客户端认证、证书信任和请求目标。采用 base_url 降级必须登记实际覆盖范围，不能把模型请求通过当成 MCP 网关也通过。

**占位符规则**：

```
格式：cage-<随机标识>，示例 cage-a1b2c3d4
作用域：会话 + provider；占位符不是授权凭据
先完成 host / port / method / path 授权
规则引用 secret 且认证头等于对应占位符（包含固定前缀）→ 检查额度后注入
缺失、重复、外来认证值或错误占位符 → 拒绝，不原样转发
未配置凭证的公共目标 → 仅按显式公共规则转发，不附带任何真密钥
日志只记录 secret 名称，不记录占位符原文、认证头或真实密钥
```

### 5.3.4 MCP 兼容与权限范围

v3 将 MCP 从通用 HTTP 注入扩展为可信协议网关，细节及待验证组件以[目标文档 §4](requirements-and-acceptance.md#4-mcp-覆盖目标与凭据方案)为准：

| 类型 | 目标路径 | 必须验证 |
|---|---|---|
| 远程 HTTP MCP，静态 Token | Agent → 内部 MCP 网关 → 固定上游；网关持有 Token | 初始化、列工具、只读调用、拒绝写入与越界资源 |
| HTTP OAuth MCP | 系统浏览器 + 宿主授权/刷新 + 网关上游会话 | 注册和回调、PKCE/state、正确资源、到期与撤销，不透传内部 token |
| 无秘密本地 stdio | 固定版本在 Agent 内运行，仅项目范围 | 路径与符号链接隔离；无上游秘密 |
| 带密码的 stdio / 数据库 | 宿主创建固定可信连接器；网关访问其私有 MCP 入口 | 密码交付不落盘、网络分离、只读账户与工具/参数限制 |

网关使用服务/会话范围的短期内部凭据，Agent 只配内部地址或无秘密 stdio 桥接器；真实上游秘密不进入沙箱。工具/资源权限在 JSON-RPC 层判定，`POST /mcp`、工具注解和只读 Token 均不能单独代替它。远程带凭据 MCP 不同时提供可绕过网关的 HTTP 注入入口。

兼容性基线为四类，不是任意 MCP 或任意私钥协议的通用保证。OAuth 或密码连接器未通过仍是核心缺口；先跑通一个 HTTP 服务仅完成最小闭环。网关、SDK、连接器和 OAuth 注册条件须在 G3/G4 验证后才可冻结设计。

### 5.4 容器硬化清单

| 项 | 值 | 目的 |
|---|---|---|
| 用户 | `--user 1000:1000` | 非 root |
| 根文件系统 | `--read-only` + `--tmpfs /tmp` + `--tmpfs /home/agent` | 防持久化 |
| capabilities | `--cap-drop ALL` | 无 NET_ADMIN / SYS_ADMIN |
| 提权 | `--security-opt no-new-privileges` | 阻断 setuid |
| Docker socket | **绝不挂载** | 防容器逃逸到宿主 Docker |
| 网络 | 仅 `cage-internal` | 无默认路由 |
| 挂载 | 仅项目目录，宿主与容器使用同一真实绝对路径 | 最小暴露，路径一致；只读预设使用只读挂载 |
| 资源 | `--memory 4g --cpus 2 --pids-limit 512` | 防跑飞 demo 机器 |

### 5.5 隔离的三层边界

| 层 | 机制 | 现场验证方式 |
|---|---|---|
| 文件 | profile `aiofcage` 仅共享 `~/CageWorkspace:w`；Agent 只挂载其中一个项目 | 放置宿主测试哨兵文件，Agent 无法读取；已授权工作区路径与宿主一致 |
| 网络 | `--internal` 无外网路由 + 双网卡门卫代理 | 直连、IPv6、DNS 和管理端口绕过测试失败，代理放行请求成功 |
| 凭证 | Keychain + 模型占位符 + MCP 可信网关 | 扫描 Agent 环境、配置、参数和镜像无上游秘密；仅有受限内部能力；跨会话和撤销测试失败 |

### 5.6 审计落盘与可信程度

- 追加写 JSONL：`~/Library/Application Support/AIofCage/audit/<session>.jsonl`
- 门卫生成网络、凭证注入、MCP 判定与额度事件；应用生成用户授权、策略变更、OAuth 连接/撤销和会话事件，记录明确来源。连接器状态须经可信控制通道确认，不能从工具输出猜测
- 应用消费 §4.3 的事件流后追加落盘，Agent 容器不得挂载审计目录；去重、恢复游标和缺口提示都必须实现
- 写盘失败时停止受影响会话并提示用户；应用崩溃期间由门卫维持策略与额度，事件缓冲可能溢出，重连后必须提示缺口
- 导出：JSONL / CSV
- 第一版不含 hash 链、`verify` 命令和不可抵赖承诺；宿主属于信任边界，宿主用户仍能修改日志

| 数据 | 能说明什么 | 不能说明什么 |
|---|---|---|
| 门卫事件 | 代理实际收到的目标、判定、凭证名称和用量 | 看不到被网络层阻断且没到达代理的连接，也不是所有系统调用记录 |
| 应用控制事件 | 用户何时授权、下发了哪版策略、何时启动或停止 | 不能以 UI 点击记录替代门卫执行结果 |
| 工作区前后 diff | 会话前后的文件差异 | 不能证明谁读过文件，不能还原中间修改后又恢复的操作 |
| 终端文本 | 帮助排错，来源不可信且可能包含敏感信息 | 不能当作命令确实执行、权限实际拦截的安全证据 |

文件访问与逐进程审计若升级为目标，需要独立的系统级采集方案；不得从 shell history 或终端文本生成伪装成可信事件的 `exec`、`fs.write`。

### 5.7 会话生命周期

```
create → starting → running → stopped → deleted
                 ↘ failed
```

| 阶段 | 动作 | 状态存放 |
|---|---|---|
| create | 建内部网络、门卫与 Agent 容器配置，生成策略快照 | JSON metadata，不存密钥 |
| starting | 管理 API ready、下发配置、订阅事件、自检后才启动 Agent | 门卫内存 + 会话元数据 |
| running | 终端 attach、事件回传、额度执行和会话授权 | 门卫执行状态 + 宿主审计 |
| stopped | 停止 Agent 与门卫；保留项目、策略、累计额度与审计 | 宿主工作区与元数据；tmpfs 不保留 |
| deleted | 删除容器、网络、临时卷（审计保留） | — |

**生命周期归属**：应用持有会话元数据，门卫为独立容器。GUI 崩溃不会解除门卫策略和额度；应用重启时按容器标签和会话 ID 对账，恢复订阅或停止孤儿会话。门卫重启丢失凭证时先关闭转发，确认累计额度后再恢复，不能重启清零。

### 5.8 用量、并发与限额

- 限额由门卫在转发前执行，已达上限时返回 `429` 并输出 `quota.blocked`。应用只展示与配置，不能在 GUI 内实现唯一的额度判断。
- 第一版至少支持请求次数硬上限。已适配模型支持 token 用量统计；要承诺 token 硬上限，必须先限制输入大小与最大输出，并在同一计数器上原子预留、响应后结算。
- 例如预算还剩 10000 tokens，两次并发各预留 6000 时只准第一笔；第一笔实际用 2000，退回 4000。无法取得 usage 时保守保留预留值，并标记估算，不能按零消耗结算。
- 未适配上游若不能给出安全的预留上界，只展示估算 token / 金额，并使用请求次数上限保护；界面不能标成精确费用封顶。
- 80% 告警每个预算周期触发一次；门卫重启后从可信累计记录恢复，缺失或有在途未结算请求时保守计费。不得让停止 / 重开会话绕过同一预算周期。

---

## 6. 策略模型

### 6.1 策略文件

```json
{
  "version": 1,
  "name": "workspace-default",
  "workspace": {
    "access": "rw"
  },
  "network": {
    "default": "deny",
    "rules": [
      { "host": "xapi.qiniu.io", "port": 443, "methods": ["POST"], "paths": ["/v1/responses", "/v1/chat/completions"], "decision": "allow", "secret": "builderday" },
      { "host": "registry.npmjs.org", "port": 443, "methods": ["GET"], "pathPrefix": "/", "decision": "allow" },
      { "host": "api.github.com", "port": 443, "methods": ["GET"], "paths": ["/user"], "decision": "allow", "secret": "github" },
      { "host": "*.githubusercontent.com", "decision": "deny" }
    ]
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
      "placeholder": "cage-e5f60718",
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
1. 规范化目标并检查私网、回环、管理端口和目标不一致 → DENY
2. host / port / method / path 命中显式 deny          → DENY
3. 全部字段命中基础 allow 或有效会话授权              → 进入凭证与额度检查
4. 未命中授权                                        → DENY（reason: default-deny，可申请后续授权）
5. 若规则引用 secret，严格校验对应认证占位符           → 不符合则 DENY
6. 额度原子检查、预留                                → 超额返回 429
7. 获准请求注入凭证（如需），再转发                    → ALLOW / INJECT 事件
```

**不可协商**：注入不能跳过授权。显式 `deny` 不可被会话授权覆盖；`default-deny` 只是没有授权，允许用户添加新规则。路径使用统一规范化结果匹配；认证值、路径解析或多规则凭证引用有歧义时拒绝。

上述 GitHub `/user` 只是 HTTP API 规则例子，不等于 GitHub MCP 接入完成。实际 MCP 需要单独定义其固定服务地址、认证方式与只读凭证，按 §5.3.4 验收。

### 6.3 权限的执行方

| 权限 | 第一版执行方与方式 | 审计边界 |
|---|---|---|
| 文件范围 | Colima 共享白名单 + 容器只挂载指定项目；只读预设用只读挂载 | 记录挂载配置和启动自检，不伪造每次文件访问事件 |
| 进程与资源 | 非 root、丢弃 capabilities、no-new-privileges、资源限制 | 记录实际生效配置；不声称完整监控每个子进程 |
| 网络 | 内部网络强制经门卫，门卫匹配目标 / 方法 / 路径 | 记录到达门卫的请求；网络层直接拒绝的连接需要额外采集器 |
| 凭证 | 宿主 vault + 门卫/可信连接器按服务运行期持有；模型授权后才注入 | 只记录凭证名称，不保存真值、内部 token 或认证头 |
| MCP 工具 | 网关执行服务、工具、类型化参数与资源策略，上游只读权限作为补充 | 记录可信网关的判定，不从 `POST /mcp` 或工具返回文本推断授权结果 |
| 命令审批 | 第一版不提供逐命令审批；容器内代码按已授予的范围执行 | shell 包装、别名或命令字符串黑名单都不是安全边界 |

用户可选择工作区只读 / 可写、模型连接、MCP 连接和受限联网预设。模板必须转换为可执行的挂载和网络规则，不能只修改 UI 开关。项目内配置不允许提升宿主下发的权限。

例如，只读工作区会直接导致写文件失败，但不会自动产生文件拒绝日志；GitHub Token 只读时，上游拒绝写操作，应记录上游状态码而不是误记为门卫成功拦截。

### 6.4 预设

| 预设 | 文件 | 网络 | 适用 |
|---|---|---|---|
| 离线只读 | 只读 | 全拒 | 手工检查或本地工具；远程模型不可用 |
| 工作区（默认） | 工作区 rw | 模型 API + 包仓库 | 日常开发 |
| 依赖安装 | 工作区 rw | 已知包仓库 + 用户补充会话授权 | 需要装依赖，未知目标仍先拒绝 |

---

## 7. 审计事件模型

### 7.1 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string | 门卫事件为 `gatekeeper_run_id:seq`；应用事件使用独立 ID |
| `ts` | RFC3339 | 来源进程生成时间 |
| `received_at` | RFC3339 | 应用接收时间；保留来源时间以便排查偏差 |
| `source` | enum | `gatekeeper` / `app` |
| `gatekeeper_run_id` / `seq` | string / uint64 | 门卫运行实例与递增序号；用于补发、去重和缺口检测 |
| `session_id` | string | 沙箱会话 |
| `actor` | enum | `agent` / `user` / `system` |
| `action` | enum | `net.request` / `net.inject` / `quota.blocked` / `quota.warning` / `authorization.change` / `policy.change` / `session` / `audit.gap` |
| MCP 扩展 action | enum | `mcp.call` / `mcp.connection` / `oauth.connection` / `connector.lifecycle`；门卫记实际调用判定，应用记已确认的连接状态 |
| `decision` | enum | `allow` / `deny` / `inject` / `error` / `not-applicable` |
| `resource` | string | 脱敏后的域名和路径，不含 query、userinfo、fragment；路径中的敏感标识也应隐藏 |
| `host` / `method` / `path` | string | 网络类事件专用 |
| `rule` / `policy_version` | string | 命中规则 ID 和策略版本，不记录可能带敏感信息的规则原文 |
| `secret` | string | 密钥**名称**（绝不记录值） |
| `service_id` / `tool_id` / `request_id` | string | 已登记的 MCP 标识与本地生成的关联 ID，不回显任意上游 ID、工具参数和结果；补发沿用同一 run/seq |
| `reason` | string | 人类可读原因 |
| `bytes_in` / `bytes_out` | int | 流量 |
| `status` | int | 上游 HTTP 状态码 |
| `duration_ms` | int | 耗时 |
| `tokens` / `usage_kind` | number / enum | 用量和 `reported` / `estimated` / `reserved`，未知不写成零 |

### 7.2 不可记录的内容

- 任何 header 原文（尤其是 `Authorization` / `x-api-key`）
- 请求/响应体；第一版不开放原文抓包调试模式
- 密钥值本身
- URL 查询参数、用户信息、管理 token；错误字符串同样必须经过脱敏
- MCP 内部 token、OAuth code/state/verifier、访问/刷新令牌、工具参数与结果。MCP 事件只记已登记的服务/工具标识、脱敏资源 ID、判定/原因、策略版本、耗时和来源序号

### 7.3 权限与审计的参考实现

| 项目 | 权限如何执行 | 可观测范围与局限 | AIofCage 借鉴 |
|---|---|---|---|
| Docker Sandboxes | VM / OS 隔离；策略代理检查目标、协议和 HTTP 规则 | `sbx policy log` 展示网络目标、决定、规则与原因；其文档注明目前不支持文件系统日志 | 网络事件与实际授权关联；不把网络日志宣传为全量文件审计 |
| Anthropic sandbox-runtime（srt） | macOS 使用 Seatbelt；Linux 使用 bubblewrap / namespace 等机制，网络经过代理 | macOS 可读取系统沙箱违规日志，Linux 需按平台排查；不是统一的全量命令账本 | 把权限落实到 OS / 网络执行层，区分平台能力 |
| mattolson/agent-sandbox | Agent 容器与可信代理分离，iptables 强制出口；代理执行端点策略并注入凭证 | 代理日志说明请求与匹配结果；端点文档定义域名、协议、方法、路径与查询参数 | 可信门卫、细粒度网络规则、严格凭证注入 |

参考一手来源：[Docker 策略](https://docs.docker.com/ai/sandboxes/security/policy)、[Docker 请求日志](https://docs.docker.com/reference/cli/sbx/policy/log/)、[srt 仓库](https://github.com/anthropic-experimental/sandbox-runtime)、[agent-sandbox 仓库](https://github.com/mattolson/agent-sandbox)。这些是架构参考，不代表本项目已实现相同能力。

---

## 8. UI 骨架

本节是页面草图。完整用户旅程及失败处理以[目标文档 §5](requirements-and-acceptance.md#5-普通用户的完整操作流程)为准；只有终端和几个按钮不足以完成用户友好性验收。

### 8.1 信息架构

```
Sessions（默认）
  └─ Session Detail
       ├─ Console（终端）
       ├─ Activity（事件流）
       ├─ Changes（文件 diff）
       ├─ Requests（被拦请求与后续授权）
       └─ Policy（本次会话生效策略）
Policies（预设 + 编辑）
Secrets（钥匙串条目）
Audit（全局审计）
Settings（Agent 路径、默认预设、审计保留）
```

**原则**：后续访问授权只在 Requests 操作；Policy 展示门卫已确认生效的版本；Audit 标明事件来源和缺口，不以终端文本填补。

### 8.2 Sessions

```
┌──────────────────────────────────────────────────────────────────────┐
│ ● AIofCage                          [⌘K]            [+ New Session]  │
├──────────────┬───────────────────────────────────────────────────────┤
│ SESSIONS  3  │  Needs you (1)                                        │
│  All         │  ┌─────────────────────────────────────────────────┐  │
│  Running  2  │  │ ● acme-api    Claude Code   blocked · 2 events  │  │
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
│ [All][Net][Quota]│                          │ ┌────────────────────┐ │
│                  │ $ npm test               │ │ ⚠ Connect          │ │
│ 14:02 ✔ allow    │ ...                      │ │ api.github.com     │ │
│  GET /user       │                          │ │ [Keep blocked][…]  │ │
│ 14:04 ✖ deny     │                          │ └────────────────────┘ │
│  unknown.example │                          │ POLICY                 │
│ 14:06 ● inject   │                          │ workspace · 4 hosts    │
│  api.anthropic   │                          │ secrets: 2             │
├──────────────────┴──────────────────────────┴────────────────────────┤
│ sbx_7f2a · policy v3 · proxy on · 2.1 GB · 14m         [Stop] [•••]  │
└──────────────────────────────────────────────────────────────────────┘
```

### 8.4 拦截的呈现：授权后续访问，不重放请求

**原因**：请求已返回拒绝后，代理重放不能把结果送回已结束的客户端调用，还可能代用户发送危险请求。第一版立即拒绝，用户确认后仅改变后续请求的授权，不实现挂起审批。

**默认行为**：

1. 门卫拒绝 → Agent 收到结构化错误（`blocked by policy: host not allowed`），它自己可以换路子或向用户报告
2. 面板里出现一行红色事件（不打断会话）
3. 用户点这一行 → 查看域名、方法、路径、用途和风险，选择是否为后续访问添加会话规则

```
┌─ 请求被拦截 ─────────────────────────────── 16:04:12 ─┐
│ ✖ registry.example.com:443             GET /pkg.tgz   │
│   规则：default-deny  来源：Codex  体积：0 B           │
│   [ 授权后续访问 ]   [ 保持拦截 ]       [ 查看详情 ]   │
└────────────────────────────────────────────────────────┘
```

点"授权后续访问"后展开：

```
┌────────────────────────────────────────────────────────┐
│  本会话允许 Codex 访问 registry.example.com？          │
│  · 该域名不在允许列表内                                │
│  · 仅限 GET /pkg.tgz，不自动授权该域名全部接口           │
│  · 原请求已经结束，请让 Agent 重新请求；不会自动发送数据 │
│                                                        │
│  [ 允许本会话 ]   [ 保持拦截 ]   [ 打开策略设置 ]       │
│  Esc 取消                          此决定写入审计 →    │
└────────────────────────────────────────────────────────┘
```

**作用域语义**：

| 作用域 | 生效范围 | 存储位置 |
|---|---|---|
| 本会话 | 限定域名 / 端口 / 方法 / 路径，停止即失效 | 门卫执行状态 + 宿主变更记录，不恢复为永久规则 |
| 永久 | 必须进入策略设置重新确认，生成新版本 | 宿主策略存储，经管理接口下发 |

**红线**：显式 deny、管理地址、非法凭证和额度限制不能从此处绕过。只对 `default-deny` 的合法目标添加会话规则；不提供“单次请求”承诺，避免重试和并发下的授权歧义。界面收到门卫确认的新版本后才显示生效。

### 8.5 Audit

表格列（8 列，可横向滚动）：

`时间 | 来源 | 决定 | 动作 | 资源 | 会话 | 规则 | 原因 | 事件 ID`

- 决定列：`✔ 放行` / `✖ 拒绝` / `● 注入` / `错误`；缺口单独提示
- 拒绝行：左侧 3px 红色边 + 淡红底；颜色之外必须有图标与文字（色盲友好）
- 展开抽屉：脱敏目标、规则 ID 与版本、凭证名称、耗时、字节数、来源序号；不回读密钥，也不展示其首尾片段
- 顶部：实时开关、暂停、筛选（决定/动作/会话/时间）、导出 JSONL/CSV

---

## 9. 演示脚本（幕 0 至幕 8）

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

**幕 5 · 借白名单外传（60s）**
按钮 4：用攻击者自带的 key 调用模型 API 上传数据。门卫发现"该目标配置了注入密钥，但请求带的是非占位符凭证" → 直接拒绝并记录，不原样转发。门卫仍不能阻止所有合法白名单请求内的内容外传。

**幕 6 · 文件边界（40s）**
按钮 5：尝试读取工作区外的宿主哨兵文件，确认因未挂载而不可见；工作区内文件仍可在可写预设中被删除，演示前请用测试副本。不能声称记录了每次文件读取，也不在真实用户家目录执行删除命令。

**幕 7 · AI 解说（30s）**
点开任一拦截事件 → 中文解释"发生了什么、像哪起事件、建不建议放行"。解说调用本身也走门卫。

**幕 8 · 边界（30s）**
一页讲清三条边界：同一 Colima VM 内取得高权限会威胁门卫；门卫不能识别所有白名单内的数据外传；项目目录是可写共享边界，diff 不是完整访问审计。

## 10. 任务拆分与排期

**时间基准**：原估算 T+0 = 2026-10-23 16:00，官方截止 2026-10-26 09:00（65 小时，含睡眠）。以下按日拆分仅保留作工作量参考，v3 新增 MCP 目标后尚未经重新估算，不能视为可按时完成的承诺。先完成 G0—G5、记录冻结与实际分工，再重排实施；保留合并和上传缓冲，不得在截止后修改仓库。

### 首个链路实验（原拟 2026-10-23 20:00，非设计冻结证明）

- [ ] 专用 Colima profile `aiofcage`（vz + virtiofs，4 核 / 6 GiB / 40 GiB）就绪，只共享 `~/CageWorkspace:w`
- [ ] `cage-internal` + 双网卡门卫 + Agent 可重复创建 / 删除；管理与代理端口隔离
- [ ] 在测试配置中完成一次已授权 API 请求，拒绝未授权 API 请求
- [ ] **方案 A 验证**：占位符 + MITM 注入后，容器内 Codex 能否用 xapi 拿到模型响应（团队 10/22 已在 sbx 中验证过同一路径）
- [ ] **绕过验证**：清除全部代理变量后，IPv4 / IPv6 直连与 DNS 外传失败；重新验证内部网络 DNS，不沿用旧环境结论
- [ ] **凭据验证**：Agent 中无上游真实秘密；模型为占位符，MCP 只有会话范围内部能力，另验证撤销/过期
- [ ] 门卫生成事件，应用从宿主回环订阅并落盘；Agent 无法连接管理端口

> 实验失败：查认证、CA、网络与管理通道。模型 base_url 适配只作局部排障，不能替代 MCP 网关验收。若关键路线不可行，修订架构并重测；不以“架构已定”为由跳过阻塞。

### Day 1（T+0 → T+12）：链路 + 门卫

| 模块 | 产出 | 验收 |
|---|---|---|
| `runtime` | 专用 profile 检查、内部网络、双网卡门卫与 Agent 启停 | 三件套可重复创建销毁、拒绝额外共享 |
| `gatekeeper` | 自签 CA、MITM、覆盖 header、403 | 单测：占位符→真值、非白名单 403 |
| `policy` | JSON 解析 + 决策 | 单测覆盖 6 种决策路径 |
| `events` / `audit` | SSE 补发、断线缺口、JSONL 追加写 | 去重、重连与缺口提示通过 |
| `vault` | Keychain 读写 | 增删查可用 |
| 镜像 | `aiocage/agent:dev` | CA + Node/Python 环境变量正确 |
| MCP 最小闭环 | 首个 HTTP MCP 经可信网关访问 | 初始化 + 列工具 + 只读调用；这不代表四类基线通过 |

### Day 2（T+12 → T+36）：产品闭环

| 模块 | 产出 |
|---|---|
| `session` | 状态机 + 元数据持久化 |
| `service` | Wails 绑定、事件推送 |
| GUI | Sessions / Detail / Console / Requests（被拦事件） |
| 会话授权 | 默认拒绝后的内联授权，仅更新后续请求规则，写审计 |
| Secrets 页 | 增删改、打码展示 |
| Audit 页 | 表格 + 筛选 + 导出 |

### Day 3（T+36 → T+60）：演示 + 文档

| 时间 | 事项 |
|---|---|
| T+36 ~ T+44 | 红队脚本 + 差分对比 + 一键模拟攻击按钮 |
| T+44 ~ T+50 | 按 §9 的幕 0 至幕 8 录屏、截图 |
| T+50 ~ T+56 | 架构文档、模块规格、分工说明、README 运行说明 |
| T+56 ~ T+63 | 冻结特性，只修 bug；确保 PR 合入并在 10 月 26 日 07:00 前完成交付 |
| T+65 | 官方 09:00 截止，仓库停止修改 |

### 分工建议（3 人）

| 角色 | 负责 |
|---|---|
| A · 边界 | `gatekeeper` / `runtime` / `vault` / 镜像 |
| B · 平台 | 策略模型 / `events` / `audit` / `session` / `service`，与 A 协同定义执行接口 |
| C · 界面 | GUI 全部页面 + 审批交互 + 演示录制 |

---

## 11. 目录结构（Go 骨架）

以下是目标结构，当前已有入口、内存会话及模块接口，实际文件与待实现内容见[模块说明](modules.md)。这里的代理、Keychain、事件落盘等具体文件仍未实现；MCP/OAuth/连接器只有接口边界，尚无协议依赖或真实接入。

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
│   │   ├── authorizations.go
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
│   │   ├── quota.go            # 转发前额度检查与并发预留
│   │   ├── management.go       # 管理 API；只绑定 bridge 侧
│   │   └── redact.go           # 日志与事件脱敏
│   ├── events/                 # 门卫流的订阅、去重、补发与缺口检测
│   ├── mcp/                    # 内部入口、协议会话映射、上游客户端与工具/资源权限
│   ├── oauth/                  # 宿主授权、回调、串行刷新；秘密交给 vault
│   ├── connector/              # 固定连接器目录与私有 MCP 适配；宿主编排容器
│   ├── vault/                  # Keychain
│   │   ├── store.go            # 接口
│   │   └── keychain.go         # security CLI 实现
│   ├── audit/                  # 审计账本
│   │   └── jsonl.go            # 宿主侧 JSONL 追加写
│   ├── runtime/                # 容器与网络
│   │   ├── docker.go
│   │   ├── network.go
│   │   └── profile.go          # aiofcage 专用实例自检
│   ├── authorization/          # 后续请求的会话授权
│   └── dockerx/                # docker CLI 薄封装
├── frontend/                   # Wails 前端
│   └── src/
│       ├── pages/{Sessions,SessionDetail,Policies,Secrets,Audit,Settings}
│       └── components/{AuthorizationSheet,AuditTable,ActivityFeed,PolicyEditor}
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
    ├── demo.md                 # 演示脚本（待填）
    └── competition-rules.md    # 比赛规则摘要及原始材料来源
```

---

## 12. 降级路径

| 失效点 | 降级方案 | 代价 |
|---|---|---|
| MITM 注入不通（Agent 强制校验 key 格式或走 OAuth） | 模型评估固定 base_url 适配；MCP 保留协议网关 | 需另验证认证与策略，不能视为全链路已通 |
| colima 不可用 | 评估另一隔离后端，重新验证共享、出网和管理面 | 未通过同一安全验收前不得替换并称已达标 |
| Wails 构建失败 | CLI 可供技术排错，桌面交付仍记阻塞 | 用户友好性不能靠命令行演示替代 |
| 宿主回环无法连接门卫管理端口 | 检查 Colima 端口转发、监听地址与防火墙；保持门卫 fail-closed | 管理与事件通道未验证前不能启动 Agent |
| 时间不足 | 砍 Policies 页与 Changes 页，只保留 Sessions/Detail/Audit | 功能完整度下降 |
| OAuth、密码连接器或题目指定 Agent 不可用 | 先解决可行性或确认范围；否则只交阶段演示并公开缺项 | 不把核心覆盖缺口改成可选项 |

---

## 13. 提交物清单（对照规则）

- [ ] 全部源代码（PR 合入 `shaoxier/AIofCage`）
- [ ] 运行说明（README：一条命令跑起来）
- [ ] 架构设计文档（本文档）
- [ ] 产品目标、兼容矩阵、G0—G5 验证记录与设计冻结记录（见目标文档，当前尚未完成）
- [ ] 模块规格与分工说明（`docs/modules.md`）
- [ ] Demo 视频（§9 的幕 0 至幕 8；时长依实际录制为准）
- [ ] 开发过程：多人 PR 记录，非最后一天一次性提交

比赛原文摘要与来源见 [`docs/competition-rules.md`](competition-rules.md)。使用比赛前已有代码作为基础时适用开发前 `v0` tag 条款；队友在本届比赛中新写的初始化代码属于团队开发记录，不应被写成必须补打 `v0`。

---

## 14. 决策记录（ADR）

只记录**有争议、且被否掉了其他选项**的决定。修改时追加新条目，不删旧条目。

### ADR-001 单进程，不做 daemon

**状态：已被 ADR-009 取代，以下为旧方案记录，不用于实现。**

- **决定**：门卫、策略、审计、容器编排全部跑在 Wails 进程内。
- **被否**：`sandboxd` 独立守护进程 + UDS（Docker sbx 的做法）。
- **理由**：daemon 需要处理进程守护、socket 鉴权、版本协商、崩溃恢复四件事，在 60 小时内是纯负债。microsandbox 重写时把 server 和 supervisor 都删掉了，是最强的信号。
- **代价**：GUI 崩溃会留下孤儿容器 → 启动时做一次 reconcile（按 metadata 里的容器 ID 清理）。
- **何时应推翻**：如果要做多客户端（CLI + GUI 同时操作同一沙箱）。

### ADR-002 门卫本身就是双网卡边车，不额外加 relay

**状态：保留，管理与事件通道补充见 §4.3 和 ADR-009。**

- **决定**：`cage-internal` + 一个同时接入 bridge 与 internal 的 gatekeeper 容器。它既是唯一入口，也是唯一出口。
- **被否**：
  - **独立 relay + 宿主门卫**：需要 host↔VM 网络（`host.docker.internal` / gvproxy），多一跳、多一个进程、多一处密钥暴露面。
  - `-p` 端口发布：**实测在内部网络下无效**，`docker port` 为空。
  - unix socket：colima 通过 virtiofs 共享文件系统，**unix socket 不能跨 VM 边界**。
- **理由**：一个容器同时具备内网地址和出网能力，就能同时完成"被 agent 访问"和"访问外网"。这是最少组件数的解。
- **代价**：gatekeeper 成为单点——它挂了整个沙箱就没网。可接受，因为它的崩溃同时也意味着策略不可用，fail-closed 正是我们要的。

### ADR-003 密钥经管理接口下发到 gatekeeper 内存

**状态：模型 HTTP 链路仍采用；MCP 运行期持有者由 ADR-013 扩展。下列“只有两个位置”是旧描述，不包含宿主传输和可信连接器。**

**状态：保留，接口绑定、鉴权和就绪条件以 §4.3 为准。**

- **决定**：桌面应用从 Keychain 读出密钥后，通过带 token 的管理接口发给 gatekeeper，只存在于 gatekeeper 进程内存。
- **被否**：
  - gatekeeper 直接读 Keychain：容器内访问 macOS Keychain 需要挂载宿主 socket / 走 `security` 命令，等于把钥匙串暴露给容器，得不偿失。
  - 写进容器环境变量：会出现在 `docker inspect`、`/proc/1/environ`，且可能被快照留存。
  - 挂载密钥文件：文件系统里出现真值，违反"沙箱内 0 处命中"的验收标准。
- **理由**：真密钥只在两个地方存在——Keychain（持久）和 gatekeeper 内存（运行期）。agent 容器内始终为零。
- **代价**：多一个管理接口需要鉴权（每次启动随机生成 token，仅 bind 在 bridge 侧）。
- **答辩口径**：密钥的**持久化位置**只有 Keychain；gatekeeper 只是运行期持有，且其中不运行任何 Agent 代码。

### ADR-004 默认拒绝 + 询问，而不是静默拒绝

**状态：已被 ADR-008 取代，以下为旧方案记录。对 Docker sbx “无提示”的概括不准确，其 Balanced 预设会生成审批请求，不能据此宣传差异化。**

- **决定**：未命中白名单的网络请求挂起，弹窗让用户三选一。
- **被否**：Docker sbx 的"无提示、直接阻断"。
- **理由**：Docker 的立场是"沙箱即安全，不需要提示"，那是给开发者的。我们的用户是普通桌面用户，`curl: (56) Recv failure` 对他们等于软件坏了。
- **代价**：多一套审批队列与作用域状态机。
- **红线**：弹窗只能把 `ask` 提升为 `allow`，**永远不能绕过 `deny`**。

### ADR-005 凭证只做 MITM，不接受 base_url 替代

**状态：模型正常路径仍使用 MITM；MCP 接入改按 ADR-013，以下关于 GitHub MCP 必须 MITM 的理由已被修订。运行位置按 ADR-009，排障降级以 §5.3 为准。**

- **决定**：宿主侧自签 CA + MITM，覆盖 Authorization。
- **被否**：`ANTHROPIC_BASE_URL` / Codex `model_providers.base_url` 指向本地网关。
- **理由**：base_url 只覆盖模型调用；GitHub MCP、`pip`、`curl` 仍然需要 MITM。选 base_url 等于最终还是要写 MITM，只是多写一套。
- **代价**：容器内必须装 CA，且要覆盖 Node / Python / Go / curl 四套信任环境。
- **踩坑记录**：goproxy 自带公开 CA 私钥，必须自签；goproxy 默认读取宿主 `HTTPS_PROXY`，必须清空。

### ADR-006 使用启动配置接入 `dsh`

- **决定**：产品支持"自定义 Agent 命令"，`claude` / `codex` / `dsh` 都只是配置项。
- **被否**：为每个 Agent 写适配器与参数模板。
- **理由**：共享启动机制可减少重复实现；但 Linux 可执行文件、平台、认证方式和证书信任必须逐项验证。
- **代价**：对三个 Agent 都不做深度集成（例如不解析它们的 session 格式）。可接受——我们卖的是边界，不是 Agent 集成。
- **风险**：若 dsh 无可用 Linux 版本，则标记“暂不支持”，记录兼容性缺口。不得静默退到无沙箱的宿主执行，也不得算作议题要求已完成。

### ADR-007 明确不做快照 / 多 Agent / Windows

- **决定**：不在 60 小时内做，且写进文档。
- **理由**：快照需要 userfaultfd + COW（E2B 的量级）；多 Agent 编排只有 Daytona / Runloop / K8s 做了，各自是一个平台；WSL2 的 Landlock 版本差异会毁掉策略一致性。
- **写法**：在"非目标"里给出理由，比被评委问出来强。

### ADR-008 拒绝后授权后续请求，取代 ADR-004

- **决定**：未知目标立即拒绝，活动面板提供本会话后续访问授权；客户端自行重试。
- **被否**：挂起等待弹窗、代理保存请求体后自动重放、在拒绝面板一键永久放行。
- **理由**：客户端已收到拒绝后，重放不能恢复原调用，且可能在用户不知情时发送数据。
- **边界**：显式 deny 不可绕过；永久规则需进入策略设置；不实现含糊的单次请求授权。

### ADR-009 桌面控制与容器门卫分离，取代 ADR-001

- **决定**：门卫容器执行 MITM、策略、注入、额度和事件生成；应用负责会话、Keychain、配置下发、SSE 订阅和 JSONL 落盘，不另建宿主 daemon。
- **通道**：代理 `8888` 与管理 `8899` 分离，管理仅从宿主回环可达，使用随机 token；事件补发与缺口协议见 §4.3。
- **代价**：必须处理两个进程的生命周期与断线；GUI 崩溃不解除策略，门卫重启先关闭转发。

### ADR-010 首版审计使用 JSONL，hash 链进入路线图

- **决定**：只记录可验证来源的网络、用量、授权、策略和会话事件；宿主追加落盘。
- **被否**：首版 canonical JSON、hash 链和 verify 命令，以及从终端推断完整命令审计。
- **理由**：优先保证事件真实、来源明确和缺口可见；宿主在信任边界内，hash 链本身也不能证明日志完整或不可伪造。

### ADR-011 先授权，再处理凭证

- **决定**：完整目标与操作授权成功后才校验占位符、预留额度和注入真实凭证。
- **被否**：按域名命中 inject 就直接放行；配有密钥的目标接受外来 key 原样转发。
- **验收**：同一域名的未授权方法 / 路径、外来凭证、管理地址和不合法上游证书均必须拒绝。

### ADR-012 MCP 兼容列为核心交付

- **决定**：第一个真实 HTTP MCP 的免密钥只读调用列入 P0，同时维护主流 Agent 与 MCP 的实际兼容矩阵。
- **边界**：HTTP 路径权限不是 MCP 工具权限；OAuth、数据库等未实现形式必须明确标注，不以单个示例宣称全覆盖。
- **状态**：v3 由 ADR-013 扩展最终覆盖目标；单个 HTTP 示例只保留为第一里程碑。

### ADR-013 MCP 使用可信协议网关与连接器（v3 候选）

- **决定**：模型仍走 HTTP MITM；带凭据 MCP 统一走门卫内独立 `8890` 协议入口，宿主负责 OAuth 授权/刷新与连接器编排。有密码的 stdio/数据库连接器在独立可信容器运行，无密钥本地工具可留在 Agent 内。
- **修订**：ADR-003/005 的密钥注入描述仅适用于模型 HTTP 链路；上游 MCP 令牌不直接下发给 Agent，MCP 覆盖不再限于静态头。ADR-009 的管理与审计通道继续使用，新增 MCP 判定与认证生命周期事件。
- **代价**：新增协议会话映射、OAuth 注册与续期、工具/参数策略、连接器供应链和私有网络。每个连接器都是接触凭据的可信依赖，不能运行用户任意配置。
- **通过条件**：目标文档 G3/G4 验证四类真实服务及反向测试；SDK、连接器版本和秘密交付通道未核实前保持候选状态。

### ADR-014 先冻结目标与可行性，再实现功能

- **决定**：以目标文档 R1—R7、G0—G5 作为设计和发布依据；需求、秘密生命周期、执行边界、接口、实际分工和实验结果明确后记录冻结，再进入正式功能开发。
- **修订**：旧按日排期是估算，AI 解说、丰富攻击展示和用量增强不能挤占指定 Agent、四类 MCP 与用户流程；关键路线失败时允许调整架构。
- **边界**：设计补齐不等于可行性验证通过。不能凭文档勾选“已满足议题”，也不能在核心阻塞未解决时承诺截止前全部可达成。

---

## 15. 商业化叙事

**一句话**：企业分发的是用量，不是 key。

| 模式 | 怎么收费 | 卖什么 | 定位 |
|---|---|---|---|
| A · 个人免费 + 团队订阅 | 个人免费开源；团队约 30–60 元/人/月（推算） | 集中托管 key、统一下发白名单与 MCP 策略、按人设额度、导出审计 | 获客 |
| B · 七牛「安全 Agent 编程包」 | 按席位或 token 包，从 token 收入分成（比例待谈） | 成员的 Agent 不持有上游 key；本地可信层代持，七牛按 key 配额与计费 | **主线** |
| C · 私有化企业版 | 按年授权 | 门卫 + 管理台部署内网，对接 SSO / SIEM | 长期 |

**回答"Claude Code 自带沙箱、sbx 也免费，为什么用你"**：

1. 一个入口统一管多种 agent（Codex / Claude Code / dsh）
2. 非技术用户也能用的原生图形界面
3. 模型秘密由门卫注入，MCP 秘密由可信网关和连接器代持；用户统一管理账号和权限
4. 国内模型开箱即用，日志与用量看得见

**三层用户**：个人开发者与刚开始用 agent 的非程序员 → 小团队负责人（不想把 key 发出去）→ 金融政企（TC260 指引与强制国标压力）。

**不报 TAM**：桌面 agent 沙箱没有可靠的独立市场数据。自下而上算：编程 agent 付费用户数 × 每人安全附加费。

> 定价与分成比例均为推算，没有国内付费意愿调查支撑，答辩时要主动说明。
