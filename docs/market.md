# AIofCage 市场与竞品调研

本报告整理截至 2026 年 10 月的竞品、公开安全事件、国内生态与商业化假设。凭证占位符和代理注入已有多个实现，AIofCage 的设计方向是面向普通用户的桌面操作、跨 Agent 配置和可解释的请求日志，而非宣称首创隔离或密钥注入。表中 AIofCage 均为设计目标，尚未实现；竞品结论仅反映所引用资料，未检索到不等于没有该能力。研发排期、权限与审计设计统一见 [架构文档](architecture.md)，比赛要求见 [规则摘要](competition-rules.md)。

## 至少十家已在做占位符注入，Docker sbx 几乎复刻了 AIofCage 的核心链路

### Docker sbx 是必须正面回应的对手

Docker 在 2026 年 4 月上旬发布了 Docker Sandboxes。每个沙箱是一台有独立内核的 microVM，macOS 上用的是基于 Apple Hypervisor.framework 的自研 VMM（[Docker Blog](https://www.docker.com/blog/why-microvms-the-architecture-behind-docker-sandboxes/)）。它的出网和密钥设计与 AIofCage 高度重合。出网方面，所有出站 TCP 都经过宿主机代理；默认的 Balanced 预设拒绝未知域名，只放行 AI 厂商 API、包管理器等基线白名单，未匹配的请求会生成一条审批请求（[Docker Docs: Policies](https://docs.docker.com/ai/sandboxes/security/policy)）。密钥方面，密钥存在 macOS 钥匙串里，agent 只能看到 `proxy-managed` 这类哨兵值；自定义密钥在沙箱里是 `sbx-cs-<rand>` 形式的占位符，并且“替换只作用于 HTTP 请求头，不作用于请求体”（[Docker Docs: Credentials](https://docs.docker.com/ai/sandboxes/configuration/credentials/)）。MCP 方面，HTTP MCP 的密钥以 `mcp:` 前缀存在宿主机，每个沙箱的 MCP 网关负责替换注册信息里的 `${api-key}` 占位符（[Docker Docs: MCP gateway](https://docs.docker.com/ai/sandboxes/mcp-gateway/)）。日志方面，`sbx policy log` 能列出被拦请求的主机、命中的规则和原因（[sbx policy log](https://docs.docker.com/reference/cli/sbx/policy/log/)）。**本地 CLI 和算力都免费，商用也免费**，只有组织级集中治理单独收费（[Docker Docs](https://docs.docker.com/ai/sandboxes/)）。Anthropic 官方文档在“虚拟机”方案里直接推荐了它，原话是“a free, standalone product from Docker”（[Claude Code Docs](https://code.claude.com/docs/en/sandbox-environments)）。sbx 的短板都在体验上：只有 CLI 和 TUI，要求登录 Docker，闭源（[Adam Warski](https://warski.org/blog/docker-sandboxes-sbx-vs-sandcat/)）；自定义密钥还是实验功能；内置服务列表里没有国内模型厂商。本次检索也没有找到它的用量统计、费用上限或产品内 AI 功能。

### agent 厂商的内置沙箱只关子命令，管不到 agent 自己的 key 和 MCP

2026 年主流 agent 都补上了操作系统级沙箱，10 月尤其密集：GitHub Copilot 的本地沙箱在 **10 月 7 日 GA，不另收费**（[GitHub Changelog](https://github.blog/changelog/2026-10-07-local-sandboxing-for-github-copilot-now-generally-available)），AWS Kiro 在 10 月 14 日跟进（[Kiro Blog](https://kiro.dev/blog/local-sandboxing/)）。但这些沙箱的结构相同：harness 在沙箱外，只把它派生的子命令关进去。Claude Code 的沙箱默认关闭，只限制 Bash 类命令；Read/Edit 等内置工具、hooks、本地 MCP 服务器都“run with your full access”，默认还能读 `~/.ssh` 和 `~/.aws/credentials`。它在 2026 年加入了 credential mask，原理确实是“哨兵值加代理替换”，但前提是开启实验性的 `tlsTerminate`（[Claude Code Docs: Sandboxing](https://code.claude.com/docs/en/sandboxing)）。Codex 的 `network_proxy` 明确不覆盖 MCP 连接，也不覆盖客户端自己的模型请求（[Codex Docs](https://learn.chatgpt.com/docs/agent-approvals-security.md)）。Cursor 文档只提到用 `.cursorignore` 隐藏文件，没有凭证注入（[Cursor Docs](https://cursor.com/docs/agent/security/run-modes)）。Kiro 的远程 HTTP/SSE MCP 不受沙箱规则约束（[Kiro Docs](https://kiro.dev/docs/sandbox/)）。Gemini CLI 最典型：启用代理时，它的容器模式用的正是“`--internal` 网络 + 独立代理容器”，和 AIofCage 是同一种拓扑，却又把 `GEMINI_API_KEY` 直接用 `--env` 传进了容器（[gemini-cli sandbox.ts](https://github.com/google-gemini/gemini-cli/blob/main/packages/cli/src/utils/sandbox.ts)）。结论是：**在 agent 厂商自带的方案里，“模型 key 和 MCP key 由同一个门卫统一注入”是空白**，只有 sbx 做到了。AIofCage 把整个 agent 进程，包括它自己的模型 key，都放到占位符后面，防护范围比“只关子命令”更完整。

### 开源项目和密钥厂商停在 CLI，企业网关不面向个人

开源社区在 2026 年把“代理注入”做成了热门方向。mattolson/agent-sandbox 用 mitmproxy 边车加 iptables 强制所有出网走代理（[GitHub](https://github.com/mattolson/agent-sandbox)）；VirtusLab/sandcat 用 WireGuard 透明代理替换 Anthropic、OpenAI 和 GitHub 的凭证（[GitHub](https://github.com/VirtusLab/sandcat)）；Matchlock 和 Gondolin 在 microVM 里只给 agent 占位符（[Matchlock](https://github.com/jingkaihe/matchlock)；[Gondolin](https://github.com/earendil-works/gondolin)）；nono 给每个会话发一个 phantom token，并支持对 MCP Streamable HTTP 注入（[nono Docs](https://nono.sh/docs/cli/features/credential-injection)）。密钥厂商方面，Infisical 在 4 月以研究预览形式发布了开源的 Agent Vault。它先剥掉 agent 自带的凭证，再注入正确的凭证；但官方自己承认配置“有点笨重”，还提醒只设 `HTTPS_PROXY` 不够，因为 agent 可以把它 unset（[Infisical Blog](https://infisical.com/blog/agent-vault-the-open-source-credential-proxy-and-vault-for-agents)）。1Password 与 NVIDIA OpenShell 的占位符集成还处于 Beta（[1Password Developer](https://www.1password.dev/get-started/secure-ai-access)）。这些项目**没有一个提供原生桌面 GUI**。唯一带 Web 控制台的 strongdm/leash，反而会把 `ANTHROPIC_API_KEY` 自动转发进容器（[GitHub](https://github.com/strongdm/leash)）。Aembit、Pomerium 这类“由网关代持 token”的产品面向企业和 SaaS 开发者（[Pomerium Docs](https://www.pomerium.com/docs/capabilities/mcp/mcp-upstream-oauth)）。LiteLLM 这类 LLM 网关支持按 key 设预算、发 80% 软告警（[LiteLLM Docs](https://docs.litellm.ai/docs/proxy/users)），但只管模型 key，不负责隔离 agent。

| 方案 | 隔离方式 | 真 key 不进沙箱 | 出网白名单 | MCP 密钥 | 图形界面 | 日志 / 用量 | 产品内 AI |
|---|---|---|---|---|---|---|---|
| Docker sbx | microVM | 是（只换请求头） | 是，默认拒绝 | 宿主机网关代持 | 否（CLI/TUI） | 网络日志；未见用量统计 | 未见 |
| Claude Code 内置沙箱 | Seatbelt/bwrap，只包 Bash 类命令 | 部分（mask 需开实验开关） | 是 | 否，MCP 在沙箱外 | 否 | 未见 | 分类器审批主机，但不解释原因 |
| Codex / Cursor / Copilot / Kiro | 操作系统级，只包子命令 | 未见注入 | 是 | Codex、Kiro 明确不管 | IDE 设置或配置文件 | 未见 | 未见 |
| Gemini CLI | Seatbelt 或容器 | 否，key 用 `--env` 传入 | 可选 | 未说明 | 否 | 否 | 否 |
| nono / Matchlock / mattolson / sandcat | 内核沙箱、microVM 或容器 | 是 | 是 | 只有 nono 支持 HTTP MCP | 否 | 终端文本日志 | 否 |
| Infisical Agent Vault | 只有代理，本身不隔离 | 是（剥掉后重新注入） | 需自己封网 | — | 否 | 有日志 | 否 |
| strongdm/leash | 容器 + 系统调用监控 | 否，会转发 key | Cedar 策略 | MCP observer | Web UI | 有 | 否 |
| **AIofCage（设计目标，尚未实现）** | Docker 容器 + `--internal` 网络 | 是 | 是，默认拒绝 | 门卫注入 HTTP MCP 密钥 | **原生桌面应用，中文** | **实时面板 + 用量上限** | **AI 安全解说（计划中）** |

按列看这张表，空白集中在最后三列。**面向非技术用户的原生 GUI、普通人看得懂的实时请求日志、在本地代理层按 key 统计用量并设上限、用自然语言解释“为什么拦截”**，这四项在检索到的产品里没有同时出现过。AI 能力只出现在两个地方：Claude Code 的 auto 模式用分类器审批每条命令要访问的主机，但只给出放行或拒绝，不解释原因（[Claude Code Docs](https://code.claude.com/docs/en/sandboxing)）；Nightfall 的 Nyx 能用自然语言调查威胁、调整策略，但它是企业产品（[Nightfall Blog](https://www.nightfall.ai/blog/mcp-security-platforms-ai-agent-policy-enforcement)）。同时也要正视劣势：AIofCage 用的普通容器隔离弱于 sbx 的 microVM，Claude Code 也已经有官方的 mask 机制，答辩时宣称“首创占位符”会被一眼识破。更稳妥的说法是“把一套已被验证的机制做成普通人能用、看得见、管得住的产品”。隔离强度的升级可以写进路线图，例如换成 apple/container 后端：它在 Mac 上把每个 Linux 容器跑成一台轻量 VM，需要 Apple Silicon 和 macOS 26（[GitHub apple/container](https://github.com/apple/container)）。

## 两天前的 LiteLLM 投毒证明威胁正在发生，但 AIofCage 只挡得住“偷”，挡不住“借”

### 攻击者已经在利用开发者本机的 AI 工具偷密钥

2025 年 8 月以来，供应链攻击从“偷开发者的密钥”升级到“利用并瞄准开发者的 AI 工具”。Nx s1ngularity（2025-08-26）的恶意 postinstall 脚本会调用受害者本机的 Claude、Gemini、Q 三个 CLI，并主动加上 `--dangerously-skip-permissions`、`--yolo`、`--trust-all-tools` 参数，让 AI 帮它在文件系统里找密钥。**Wiz 称这一步“在数百个案例中成功”**，第一阶段就泄露了 1000 多个有效的 GitHub token（[Wiz](https://wiz.io/blog/s1ngularity-supply-chain-attack)），事后仍有 90% 以上的 GitHub token 有效（[Apiiro](https://apiiro.com/blog/nx-supply-chain-breach-shows-why-malicious-package-detection-matters)）。2026 年 5 月的 Mini Shai-Hulud 会修改 AI 编程 agent 的配置文件，制造 hook 来持久化；一旦发现自己的 token 被撤销，就清空受害者的家目录（[MITRE ATT&CK S9043](https://attack.mitre.org/software/S9043/)）。10 月 8 日被投毒的 tensorlake 0.5.144 把 `.claude`、`.cursor`、`.kiro` 目录和 MCP 配置列为窃取目标（[Socket](https://socket.dev/blog/tensorlake-compromise)）。**10 月 21 日的 LiteLLM 1.107.0 在被隔离前下载了约 4.9 万次**，它的窃取器专门收集环境变量和配置文件里的 OpenAI、Anthropic 等 API key，外传到一个仿冒域名；约 4.5 小时后，攻击者就开始用偷到的 AWS 凭证探测 Bedrock（[Wiz](https://www.wiz.io/blog/litellm-compromised-steals-credentials)）。这些攻击有一个共同前提：恶意代码以开发者本人的身份运行，能读整个家目录，能随意出网。AIofCage 的文件隔离、密钥占位符和出网白名单正好对应“读不到、拿不到、发不出”。

### key 泄露越来越多，人工确认已经失效

GitGuardian 的统计显示，**2025 年公开 GitHub 上泄露的 AI 服务密钥有 1,275,105 个，同比增长 81%**；在 MCP 相关配置文件里找到了 24,008 个不同的密钥；Claude Code 辅助的提交泄露率是 3.2%，是基线 1.5% 的两倍多（[GitGuardian](https://dev.to/gitguardian/the-state-of-secrets-sprawl-2026-ai-service-leaks-surge-81-and-29m-secrets-hit-public-github-2bgj)）。Astrix 分析了 5,200 多个开源 MCP 服务器：88% 需要凭证，53% 依赖长期有效的静态 API key 或 PAT，只有 8.5% 用 OAuth（[Astrix](https://astrix.security/learn/blog/state-of-mcp-server-security-2025/)）。这就是题目要求“MCP 配置时无需在沙箱内填写任何敏感信息”的现实背景。10 月 OpenCode Console 被攻破，存在它工作区里的 OpenAI、Anthropic、Google key 被盗刷，有用户损失超过 1000 美元（[TechCrunch](https://techcrunch.com/2026/10/19/hackers-hit-ai-coding-tool-opencode-running-up-charges-on-users-ai-accounts/)），这从反面说明 key 只该放在用户自己的钥匙串里。破坏性操作同样真实：一位 Mac 用户的 Claude Code 执行了 `rm -rf tests/ patches/ plan/ ~/`，删掉了整个家目录（[Simon Willison](https://simonwillison.net/2025/Dec/9/claude)）；Anthropic 的内部事故日志里也有 agent 删除远程分支、把工程师的 GitHub token 上传到内部集群的记录（[Anthropic Engineering](https://www.anthropic.com/engineering/claude-code-auto-mode)）。

人的注意力也有上限。Anthropic 披露，**用户会批准 93% 的权限提示**，审批疲劳让人们“不再仔细看自己批准的是什么”（[Anthropic Engineering](https://www.anthropic.com/engineering/claude-code-auto-mode)）。它的沙箱把权限提示减少了 84%，并明确指出有效的沙箱必须同时做到文件系统隔离和网络隔离（[Anthropic Engineering](https://www.anthropic.com/engineering/claude-code-sandboxing)）。开发者的态度是矛盾的：Stack Overflow 2025 年调查中，约 81% 的人担心使用 agent 时的数据安全和隐私（[Stack Overflow](https://survey.stackoverflow.co/2025/ai)）；2026 年 4 月的快速调查中，63% 的人很少或从不让 agent 完全自动运行，44% 的人强烈同意“安全是采用 agent 的障碍”（[Stack Overflow Blog](https://stackoverflow.blog/2026/05/27/agents-on-a-leash-agentic-ai-remains-mostly-monitored-at-work)）。用户想开全自动又不敢开，这就是 AIofCage 最直白的价值主张：**放心开 YOLO，因为最坏的情况也被关在笼子里。**

### 诚实的边界：门卫防偷不防用，项目目录是一座桥

占位符方案有两个业界公认的弱点。一是“防得住偷 key，防不住用 key”（[dvergr](https://cljdoc.org/d/org.replikativ/dvergr/0.1.8/doc/boundary-secret-injection-credential-handling-in-the-sandbox)）；二是白名单域名本身就能被用来外传（[Formal](https://www.formal.ai/blog/using-proxies-claude-code/)）。s1ngularity 走的就是 api.github.com。如果门卫给 GitHub 注入了有写权限的 token，恶意代码虽然拿不到 token 的值，仍然可以借门卫的手建公开仓库、上传数据。Invariant Labs 演示的 GitHub MCP 外泄也完全发生在白名单内：agent 读到公开 issue 里注入的指令，用权限过宽的 PAT 读取私有仓库，再向公开仓库提 PR 把数据带出去（[Invariant Labs](https://invariantlabs.ai/blog/mcp-github-vulnerability)）；Supabase MCP 的案例同理（[Simon Willison](https://simonwillison.net/2025/Jul/06/supabase-mcp-lethal-trifecta/)）。对这类“在 SaaS 内部挪数据”的攻击，AIofCage 基本无能为力，只能靠审计日志、按路径限制和最小权限来缓解。

但有一类攻击，门卫恰好有独特的解法。Rehberger 演示过让 Claude 用**攻击者自己的 API key** 调用 Anthropic Files API，把数据上传到攻击者的账号，单次最多 30MB。纯域名白名单挡不住，因为 api.anthropic.com 必须放行（[SecurityWeek](https://www.securityweek.com/claude-ai-apis-can-be-abused-for-data-exfiltration/amp/)）。门卫本来就在解密 HTTPS、改写请求头，只要再加一条规则：“配置了密钥的域名只接受占位符，带着其他真 key 的请求一律剥掉凭证或拒绝”，几十行代码就能堵上这条路。Infisical Agent Vault 和 NVIDIA OpenShell 都有类似的“剥掉再注入”（[Infisical](https://infisical.com/blog/agent-vault-the-open-source-credential-proxy-and-vault-for-agents)；[NVIDIA](https://perspectives.nvidia.com/nvidia-openshell/agent-sandbox-injects-credentials-gateway-no-api-keys/)）。相对于 devcontainer 和纯域名白名单方案，这是少有的真实技术差异点。

另一个边界是项目目录。它以读写方式挂载，沙箱里写进 `.claude/settings.json`、`.vscode/tasks.json` 或 `.git/hooks` 的恶意配置会原样留在宿主机上，Mini Shai-Hulud 就是这样持久化的。项目里的 `.env` 也会被挂进沙箱。Copilot CLI 的“加密上下文注入”就是先读 `.env.prod`、再把内容外传，全程约 28 秒，GitHub 认为这不算漏洞（[Adversa AI](https://adversa.ai/blog/cryptographic-context-injection-github-copilot/)）。

| 事件（时间） | 文件隔离 | 出网白名单 | 密钥占位符 | 审计日志 | 对路演的用处 |
|---|---|---|---|---|---|
| LiteLLM 1.107.0 投毒（2026-10-21） | ✓ | ✓ 仿冒域名被拦 | ✓ | ✓ | 最新、最贴题 |
| tensorlake 投毒（2026-10-08） | ✓ | ✓ | ✓ MCP 配置里只有占位符 | ✓ | 支撑“MCP 免密钥” |
| Nx s1ngularity（2025-08） | ✓ | △ GitHub 在白名单内 | ✓ | ✓ | “恶意代码让你的 AI 去偷密钥” |
| Mini Shai-Hulud（2026-05） | △ 会经项目目录持久化 | ✓ | ✓ | ✓ | 需要退出时检查配置文件 |
| Claude Code `rm -rf ~/`（2025-12） | ✓ | — | — | — | 最直观的现场演示 |
| Claude Code DNS 外传（CVE-2025-55284） | — | △ 待实测 internal 网络的 DNS | ✓ | — | 验证项 |
| 借 Files API 白名单外传（2025-10） | — | 只靠域名 ✗；加“只认占位符”后 ✓ | ✓ | ✓ | 差异化卖点 |
| GitHub MCP 私有仓库外泄（2025-05） | ✗ | ✗ | △ token 不泄露，但会被滥用 | ✓ | 主动讲清边界 |
| OpenCode Console 泄露（2026-10） | ✗ | ✗ | ✗ 服务端被攻破 | ✗ | 印证“key 只放自己的钥匙串” |

上表按 AIofCage 的设计目标判断，前提是依赖安装也在沙箱终端里执行。如果用户在宿主机上跑 `npm install`，AIofCage 帮不上忙，所以产品的默认流程应该引导用户在沙箱里装依赖。

## 国内桌面生态与七牛云端能力

### 云沙箱默认放开出网，编程 CLI 只管文件

2025 年下半年到 2026 年，国内大厂和 AI Infra 公司集中推出了云端 agent 沙箱。它们几乎都以 API/SDK 的形式卖给 agent 开发者，并以“兼容 E2B”为卖点：腾讯在 4 月开源了冷启动低于 60ms 的 Cube Sandbox（[新闻稿](https://www.aap.com.au/aapreleases/cision20260423ae41855/)），PPIO 自称是国内首款兼容 E2B 的 agent 沙箱（[PPIO](https://ppio.com/ai-computing/sandbox)），七牛 LAS 沙箱也“完全兼容 E2B SDK”（[七牛开发者中心](https://developer.qiniu.com/las/13281/sandbox-overview)）。这些产品的出网通常默认放开。阿里 AgentBay 的安全白皮书写明“规则未设置时，默认允许访问所有域名”，而且域名管控只对 Windows 沙箱有效；白皮书也没有提到密钥注入，只建议客户使用环境变量或 KMS（[AgentBay 安全白皮书](https://help.aliyun.com/zh/agentbay/agentbay-security-white-paper)）。国产编程 CLI 的沙箱只管“文件能写到哪里”。Qwen Code 官方文档说 `--yolo` 本身不会启用沙箱（[Qwen Code Docs](https://qwenlm.github.io/qwen-code-docs/en/users/features/headless/)），它的 bwrap 沙箱也“不会隐藏宿主机机密”（[Qwen Code Docs](https://qwenlm.github.io/qwen-code-docs/en/users/features/sandbox/)）；TRAE 文档的原话是“当前安全策略不涉及网络访问”（[TRAE 文档](https://docs.trae.cn/work_sandbox)）；dsh 的三档沙箱据社区资料主要管控文件写入，不限制网络（[菜鸟教程](https://www.runoob.com/deepseek-harness/deepseek-harness-sandbox-approval.html)）。这些工具的 key 都放在 agent 自己就能读到的环境变量或配置文件里。

### 本地桌面只有“安全龙虾”，国内用户还多一层风险

国内本地桌面方向的产品几乎都围绕 OpenClaw（“龙虾”）。360 龙虾卫士在 3 月 14 日发布，用 WSL 沙箱做隔离，用 AI 引擎识别恶意技能，客户端免费，入门算力套餐 169 元；报道没有提到密钥保护和出网控制（[澎湃新闻](https://www.thepaper.cn/newsDetail_forward_32769260)）。同一个月，360 安全龙虾的公开安装包里还被发现带着泛域名证书的 SSL 私钥（[IT 之家](https://www.ithome.com/0/929/718.htm)）。七牛自家的桌面龙虾 Linclaw，公开资料里也没有沙箱或密钥隔离的描述（[七牛新闻站](https://news.qiniu.com/archives/1773626321194)）。国内用户还多面对一层风险：Anthropic 从 2025 年 9 月起限制中资控股超过 50% 的实体使用 Claude（[InfoQ](https://www.infoq.cn/article/4IpOKeq7lJmFMkGjjbUW)），很多人转向淘宝、闲鱼上的中转站。2026 年 4 月的一篇 arXiv 论文实测了 28 个付费路由和 400 个免费路由，结果是 **1 个付费路由和 8 个免费路由主动注入了恶意代码，17 个路由动用了研究者布置的 AWS 诱饵凭证**（[arXiv 2604.08407](https://arxiv.org/abs/2604.08407)）。GitGuardian 的报告提到，2025 年泄露的 DeepSeek API key 有 11.3 万个（[GitGuardian](https://dev.to/gitguardian/the-state-of-secrets-sprawl-2026-ai-service-leaks-surge-81-and-29m-secrets-hit-public-github-2bgj)）；dsh 社区也有用户报告关机后 key 在凌晨被调用，但维护者没有确认（[GitHub Discussion #2756](https://github.com/deepseek-ai/deepseek-harness/discussions/2756)）。

### 政策文件已经列出了 AIofCage 要解决的问题

国家互联网应急中心 2026 年 3 月 10 日针对 OpenClaw 发布风险提示，建议“使用容器等技术严格隔离运行环境，限制过高权限”，并“避免在环境变量中明文存储密钥，并建立完整的操作日志审计机制”（[新华网转载 CNCERT](https://www.news.cn/tech/20260310/d5e1d772bed046239ea3774903c08970/c.html)）。这几乎逐条对应 AIofCage 的文件隔离、密钥占位符和请求日志，是最适合讲给国内评委的官方依据。标准层面，TC260 在 7 月 1 日发布了《智能体部署使用安全指引》（[TC260](https://www.tc260.org.cn/tc260/tzgg/202607/98b6f259f8a3428b8fe1cfea3c0fb19e.shtml)）；强制性国标《智能体应用安全基本要求》在 6 月 27 日下达计划，研制期 18 个月（[全国标准信息公共服务平台](https://std.samr.gov.cn/gb/search/gbDetailedCNF?id=4C5277928DA2411EE06397BE0A0AE436)）。这次调研没有读到指引的正文，答辩时只宜说“方向一致”，不要声称逐条对应了具体条款。

### 七牛：云端已有同样的能力，缺的是端侧入口

七牛的 AI 业务正在找增长点。2025 年 AI 相关收入 4.37 亿元，占营收 24.7%（[格隆汇](https://m.gelonghui.com/news/5190653)）；2026 年上半年“AI 云”收入 1.58 亿元，占 17.57%，两期口径可能不同，不能直接比较（[同花顺](https://stock.10jqka.com.cn/20260929/c680379515.shtml)）。七牛把自己定位为“中国版 OpenRouter”，MaaS 相关用户超过 18 万，平台支持多个 key 相互隔离、按项目设预算上限（[爱范儿](https://www.ifanr.com/1652283)），API 也已支持给每个 key 设置日、月、总额度和告警阈值（[七牛开发者文档](https://developer.qiniu.com/aitokenapi/13405/set-api-key-limit-api)）。更关键的是，**七牛 LAS 云沙箱已经上线“密钥注入”，规则类型包括 openai、anthropic、qiniu、github 和通用 http，目的正是“避免把 API Key 明文写入沙箱环境、代码或镜像中”**（[七牛：密钥注入](https://developer.qiniu.com/las/13446/sandbox-qshell-injection)）。它的出网规则支持按域名设置 allowOut/denyOut，但“默认启用互联网访问”（[七牛：沙箱网络访问](https://developer.qiniu.com/las/13291/sandbox-internet-access)）。七牛的 MCP 托管也做到了“API Key、密钥等敏感信息云端托管，终端无需暴露”（[七牛 MCP 使用说明](https://developer.qiniu.com/aitokenapi/12984/mcp-user-manual)）。

缺口在端侧。七牛的 AI 编程工具配置文档覆盖了 Cline、Cursor、OpenCode 等，却没有 Claude Code 和 Codex（[七牛 AI 编程工具配置大全](https://developer.qiniu.com/aitokenapi/13417/tools-AI-Coding-api)）；而七牛新闻站在 8 到 10 月密集发布了 dsh 和 Codex 的教程（[七牛新闻站](https://news.qiniu.com/)），说明编程 agent 用户正是七牛想争取的增量。所以面对七牛评委，最有力的叙事是：**AIofCage 不是另起炉灶，而是七牛 LAS 密钥注入能力的桌面版，也是本地开发的入口**。门卫规则的语义可以对齐 `qshell sandbox injection-rule` 的五种类型，用户在本地验证好的规则可以迁移到云沙箱；云端托管的 MCP 用七牛的服务，本地和私有的 HTTP MCP 由门卫注入，两者互补。

## 企业要分发的是用量而不是 key：三类用户与三种收费方式

### 需求和资本都已到位，但不要报 TAM

在沙箱里运行的“工作负载”规模已经很大。Stack Overflow 2026 年调查显示，在使用 AI 的开发者中，66% 用编程 agent，用得最多的是 Claude Code（66%）；38% 的组织设置了 AI 用量上限（[Stack Overflow Blog](https://stackoverflow.blog/2026/10/06/the-results-of-the-2026-developer-survey-are-here/)）。Claude Code 在 2026 年 2 月的年化收入超过 25 亿美元，一半以上来自企业（[Anthropic](https://www.anthropic.com/news/anthropic-raises-30-billion-series-g-funding-380-billion-post-money-valuation)）。Codex 6 月的周活超过 500 万，其中约 20% 是不写代码的知识工作者（[Constellation Research](https://constellationr.com/insights/news/openai-touts-broadening-codex-usage-5-million-weekly-active-users)）。资本也在用真金白银确认这个需求：2025 年 8 到 9 月，四家独立的 AI 护栏公司相继被大型安全厂商收购（[Decryption Digest](https://www.decryptiondigest.com/blog/ai-guardrail-vendors-after-acquisition-lakera-prompt-security-aim-calypsoai)）；据报道，Cisco 在 2026 年 5 月以约 4 亿美元收购了做非人类身份凭证治理的 Astrix（[Security Boulevard](https://securityboulevard.com/2026/05/cisco-fortifies-ai-defenses-with-acquisition-of-astrix-security/)）；agent 沙箱基础设施公司 Daytona 在 10 月以 10 亿美元估值完成 B 轮融资（[Dealroom](https://dealroom.co/news/164304-daytona-raises-60m-series-b-at-1b-valuation-to-build-the-agentic-cloud/)）；Gartner 预测，到 2030 年守护型 agent 将占 agentic AI 市场的 10% 到 15%（[Gartner](https://www.gartner.com/en/newsroom/press-releases/2025-06-11-gartner-predicts-that-guardian-agents-will-capture-10-15-percent-of-the-agentic-ai-market-by-2030)）。但“桌面 agent 沙箱”还没有可靠的独立市场规模数据，相邻市场的估算口径能差出一倍以上。答辩时不建议报 TAM，而是自下而上地讲：编程 agent 的付费用户数，乘以每人的安全附加费。

### 三层目标用户

第一层是**在 Mac 上用 Claude Code、Codex、dsh 的个人开发者，以及刚开始用 agent 的非程序员**。他们想开全自动又怕出事，也不会自己配 Docker 网络和 HTTPS 代理，正是“点三下就能用”和中文界面要服务的人。在国内，这一层还包括依赖中转站使用 Claude Code 的用户：AIofCage 能限制恶意中转站注入的代码所能造成的破坏，同时把这些用户引向合规的七牛接口。第二层是**小团队和大量使用外包的团队的负责人**。他们需要让成员用上模型，又不想把 key 发出去。阿里百炼 Token Plan 团队版已经在按席位每月 198 到 1,398 元售卖这类方案：成员不能自己生成 key，由管理员分配专属 key（[阿里云开发者社区](https://developer.aliyun.com/article/1743199)），说明有人愿意为这种需求付费。第三层是**金融科技、政企等合规要求高的客户**。金融科技每天使用 agent 的比例达到 55%，是各行业中最高的（[Stack Overflow Blog](https://stackoverflow.blog/2026/05/27/agents-on-a-leash-agentic-ai-remains-mostly-monitored-at-work)），同时又直接面对 TC260 指引和即将出台的强制国标。

### 三种收费方式，以七牛捆绑为主线

| 模式 | 怎么收费 | 卖什么 | 参照 | 定位 |
|---|---|---|---|---|
| A. 个人免费 + 团队订阅 | 个人版免费开源；团队版每人每月约 30 到 60 元（推算） | 集中托管 key，统一下发白名单和 MCP 策略，按人设额度和告警，导出审计日志 | sbx 本地免费、组织治理付费（[Docker Docs](https://docs.docker.com/ai/sandboxes/security/governance/)）；1Password Business 每人每月 7.99 美元（[PricingSaaS](https://pricingsaas.com/companies/1password/diffs/2026Q1)）；Docker Business 每人每月 24 美元（[UsagePricing](https://www.usagepricing.com/blueprint/activity/docker-2026-10-15-build-cloud-minutes-dropped)） | 获客、建立口碑 |
| B. 七牛“安全 Agent 编程包” | 按席位或 token 包售卖，AIofCage 从 token 收入中分成（比例待与七牛确认） | 成员电脑上连专属 key 都没有，只有占位符；七牛负责按 key 的配额和计费 | 百炼 Token Plan 团队版（[阿里云开发者社区](https://developer.aliyun.com/article/1743199)）；百炼 Coding Plan Pro 每月 200 元（[阿里云帮助中心](https://help.aliyun.com/zh/model-studio/coding-plan)）；OpenRouter 抽成约 5%（[Sacra](https://sacra.com/c/openrouter/)） | **主线，最贴合主办方** |
| C. 私有化企业版 | 按年授权 | 门卫和管理台部署在企业内网，对接 SSO 和 SIEM | Portkey 按部署方式（含完全隔离部署）定价（[Portkey Docs](https://portkey.ai/docs/enterprise/pricing)）；E2B Enterprise 每月 3000 美元起（[E2B](https://e2b.dev/pricing)） | 长期增长空间 |

答辩应该以模式 B 为主线。同行的 Coding Plan 拼的是价格和额度，智谱 GLM Coding Plan 甚至一度因为需求太旺而限售（[界面新闻](https://www.jiemian.com/article/14011644.html)）。七牛很难在价格上取胜，但可以在“安全、可管”上拉开差距：企业买的是团队 token 包，成员拿到的是一个桌面应用，真 key 由门卫持有，用量同时受七牛的按 key 配额和门卫面板约束。一句话定位是“**企业分发的是用量，不是 key**”。必须提前准备一个质疑：“Claude Code 自带沙箱，sbx 也免费，为什么要用你？”回答落在四点：一个入口统一管多种 agent；非技术用户也能用的图形界面；模型 key 和 MCP key 由同一个门卫注入；国内模型开箱即用，日志和用量看得见。需要说明的是，模式 A 的人民币定价和模式 B 的分成比例都是推算，没有国内付费意愿的调查数据支撑。

## 结论

这轮调研最重要的修正是：AIofCage 的护城河不在门卫机制本身，而在“事件流 + 可信的体验 + 七牛渠道”这个组合。Docker、Anthropic、开源社区，连同七牛自己的云沙箱，已经把机制做成了标配；Copilot 和 Kiro 在 10 月免费上线沙箱，sbx 本地免费。这说明单卖“隔离”做不成生意，能收钱的是让企业分发用量而不是 key，以及让普通人看懂 agent 到底做了什么。对七牛来说，AIofCage 补上的正是 LAS 密钥注入和 MCP 托管在端侧缺失的那一环，也是把 token 卖给 Claude Code、Codex、dsh 用户的入口。

仍需验证的商业假设包括七牛合作与分成政策、国内用户的付费意愿，以及相关竞品在国内网络环境下的可用性。技术上需区分容器内代码执行与突破 VM 信任边界，不能将有限的本机实验推广为普遍安全保证；具体实现与验收以架构文档为准。
