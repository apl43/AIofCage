# AIofCage 桌面工作区交付验收记录

日期：2026-10-24；环境：macOS Apple Silicon，专用 Docker context `colima-aiofcage`。

12 步改造已实现，按任务提交堆叠 PR，供团队统一验收。构建、自动检查、真实沙箱生命周期和两款智能体文件任务已通过；Codex 当前供应商请求超时，真实模型任务仍待补验。本报告不将跳过或证据不足的检查记为通过。

## 交付入口与 PR

本机应用：`/Users/dumpling/AIofCage/bin/AIofCage.app`。产品规则见[桌面工作区规格](../../docs/product-workspace.md)，使用方法见 [README](../../README.md)。

| 步骤 | 任务 | PR |
| --- | --- | --- |
| 01 | 产品目标、对象、流程与验收矩阵 | [#22](https://github.com/shaoxier/AIofCage/pull/22) |
| 02 | 导航、布局和桌面交互基础 | [#23](https://github.com/shaoxier/AIofCage/pull/23) |
| 03 | 默认空白沙箱、可选安装 | [#24](https://github.com/shaoxier/AIofCage/pull/24) |
| 04 | 创建后增装智能体与失败恢复 | [#25](https://github.com/shaoxier/AIofCage/pull/25) |
| 05 | 多供应商、沙箱独立绑定、待重启配置 | [#26](https://github.com/shaoxier/AIofCage/pull/26) |
| 06 | 内置终端与沙箱工作台 | [#27](https://github.com/shaoxier/AIofCage/pull/27) |
| 07 | 持久化模型请求与用量明细 | [#28](https://github.com/shaoxier/AIofCage/pull/28) |
| 08 | 供应商与模型费用估算 | [#29](https://github.com/shaoxier/AIofCage/pull/29) |
| 09 | 全组件运行状态及资源采集 | [#30](https://github.com/shaoxier/AIofCage/pull/30) |
| 10 | 健康面板、资源趋势与异常状态 | [#31](https://github.com/shaoxier/AIofCage/pull/31) |
| 11 | 按实际证据展示隔离检查 | [#32](https://github.com/shaoxier/AIofCage/pull/32) |
| 12 | 用户路径验收、交付修复与证据 | [#33](https://github.com/shaoxier/AIofCage/pull/33) |

依赖顺序为 01 → 12，每个 PR 的 base 是前一步分支。PR 保持打开，由团队按顺序使用 merge commit 合并，未自行合并或推送 main。

## 实际验证

| 路径 | 实际结果 | 证据 |
| --- | --- | --- |
| 默认空白创建、启动与 Shell | 无智能体可运行，Shell 为 agent 用户，UID 1000 | 真实 E2E、[Shell 截图](screenshots/terminal-shell.png) |
| 后续增装 | 从空白增装 Codex、Claude Code，再增装 DeepSeek Harness；项目文件保留 | 真实 E2E、图形操作、[文件证明](fixture-proof.json) |
| 增装失败恢复 | 注入无效待生效 KeyRef，增装失败后恢复原运行快照和两智能体组合 | [E2E 日志](workspace-e2e.log) |
| 多供应商和独立选择 | Codex 可保存两份配置；两个真实沙箱分别选择，applied 配置及占位符独立 | E2E、[供应商截图](screenshots/providers.png) |
| 配置生效时机 | 图形界面保存后显示待重启；确认重启后应用新选择 | [待生效截图](screenshots/agents-pending.png) |
| API Key 更新 | 使用中的同一 Keychain 条目禁止覆盖；停止沙箱后允许更新 | `TestUpdatingKeyCannotOverwriteAppliedCredential` |
| 终端输入与输出 | Shell 直接输入、可见任务输入、中文、行列调整、退出码与跨页会话保留 | 真实 PTY 测试、图形操作 |
| 终端关闭与退出 | Shell 子进程关闭后标记进程数为 0；并发启动与应用退出验证通过 | [终端 E2E 日志](terminal-e2e.log)、文件证明 |
| Claude Code 真实任务 | 在测试项目生成预期文件，读取文件核对内容 | E2E 日志 |
| DeepSeek Harness 真实任务 | 自动测试及 GUI 均生成预期文件；GUI 任务有流式输出 | E2E 日志、[任务截图](screenshots/dsh-task.png)、文件证明 |
| 用量持久化 | 输入、输出、缓存、供应商及模型明细存在；重新创建 App 后请求数一致 | E2E 日志、用量单测 |
| 费用估算 | 人工价格保存读回成功；同页展示有价格、无价格与用量未知三类请求 | [用量截图](screenshots/usage-costs.png) |
| 全组件健康 | 实采运行、停止、含 PostgreSQL MCP 的沙箱；组件数分别符合实际 | [健康采集日志](health.log) |
| 资源趋势 | GUI 观察到 120 样本上限，应用重开后重新记录；停止时显示缺失值 | [趋势截图](screenshots/health-trends.png)、[末轮状态截图](screenshots/health-running.png) |
| 组件与 VM | Agent、门卫及 MCP 分别展示状态和限制；VM 4 核 / 6 GiB / 40 GiB 单独展示 | 健康日志、GUI、[组件截图](screenshots/health-components.png) |
| 清理后状态 | GUI 只剩原两个沙箱，各智能体供应商标签均为 1；后端核对一致 | [清理后截图](screenshots/clean-sandboxes.png)、[清理记录](cleanup.json) |

GUI 使用一次性沙箱 `uat-workspace-1024`。创建、增装、绑定、重启、启停和任务输入均经过界面；最终删除使用 cagectl 和精确路径清理，不能据此宣称“GUI 删除流程已通过”。

## 真实任务、用量与估算

完整 E2E 最后一轮耗时 39.20 秒：Claude Code、DeepSeek Harness 文件任务通过；Codex 子任务因供应商无网络响应而跳过。上层测试的 PASS 包含这一项 SKIP，不代表三款智能体真实调用全部通过。

| 智能体 | 模型 | 总输入 token | 输出 token | 缓存读取 token | 估算 CNY |
| --- | --- | --- | --- | --- | --- |
| Claude Code | deepseek-flash | 22,022 | 100 | 10,880 | 0.0254600 |
| DeepSeek Harness | deepseek-flash | 11,792 | 208 | 11,136 | 0.0056192 |

共 5 次模型请求，用量未知 0 次，未计价 0 次，合计估算 CNY 0.0310792。缓存读取包含在总输入中，计价时不会重复计算。

以上使用**合成验收单价**：每百万 token 输入 2、输出 10、缓存读取 0.2、缓存写入 2 CNY，价格版本 `uat-synthetic-rate`。它们只验证计价逻辑，**不是官方报价或供应商真实账单**。

GUI 另一次独立运行显示 11 次模型请求、约 24.1k 已知 token、估算 CNY 0.0063、8 次未计价，其中 5 次用量未知。这些数值与自动 E2E 属于不同沙箱，不合并。沙箱概览另有 12 次代理请求，包含非模型请求；用量页只统计模型调用。

改价后不重算既有请求；请求保存价格快照。缺少价格或完整用量时，费用保持未知。旧累计、重连缺口、不同币种和缓存归一化另有自动回归测试。

## 隔离检查

安装三智能体后的 GUI 结果为 **5 通过、0 发现问题、1 无法验证、1 跳过**，见[隔离检查截图](screenshots/isolation-checks.png)。

| 检查 | 结果 | 实际证据或限制 |
| --- | --- | --- |
| 智能体凭据 | 通过 | 检查到占位符；不输出凭据值 |
| 未授权 HTTP 域 | 通过 | 明确的 403 与门卫 `default_deny` 原因 |
| 直连外网 | 通过 | 两个 IPv4 socket 直连均明确被阻断 |
| 外来凭据 | 通过 | CONNECT 200 后目标请求 403，原因 `credential_foreign` |
| 宿主边界 | 通过 | 自建临时宿主 marker 和 Docker socket 不可见；不扫描用户敏感文件 |
| DNS 旁路 | 无法验证 | DNS 为 127.0.0.11；无 IPv4/IPv6 默认路由，公网 UDP 53 被阻断，但 Docker DNS 转发路径未证实 |
| 数据库旁路 | 跳过 | 测试沙箱未配置数据库 MCP；没有登录数据库或执行 SQL |

执行失败、DNS 超时和证据不足均不能判为隔离通过。检查只说明已探测的路径，不构成完整安全认证。

## 验收期间修复

- WKWebView 后台动画帧暂停导致终端缓冲已收到输出、屏幕却不刷新：增加可取消、只触发一次的 100ms 帧补偿；保留原生帧调度，未修改 vendored xterm 或使用私有 API。
- 终端延迟到实际显示的容器中打开，并在启动后重新同步尺寸；关闭标签清理容器会话及子进程。启动期间关闭也会补发关闭，退出后拒绝新终端。
- 用量必须同时取得输入和输出字段才标记完整，部分字段不会冒充零用量；全未知分组显示“用量未知”。
- 旧累计提示按实际旧记录或缺失明细判断，避免新账本被误报为旧数据。
- 增装失败按原 applied 快照恢复运行，保留待生效选择；保存配置失败同样尝试恢复。
- 正在被 applied 快照引用的同一钥匙串条目禁止覆盖，防止策略刷新提前使用新 Key。
- 移除按钮背景色过渡，避免 WKWebView 后台状态下切换启动/停止时残留旧背景色；末轮截图已核对停止按钮样式。
- 真实终端竞态测试首次暴露测试订阅移除早于退出事件的竞争，已改为等待退出事件后移除订阅，复跑通过。

## 自动与真实环境检查

| 检查 | 结果 | 日志 |
| --- | --- | --- |
| `make app` | 通过，生成并签名本机 app bundle | [build.log](build.log) |
| `make check` | production Go vet / build 通过 | [check.log](check.log) |
| `go test -race ./internal/...` | 通过；默认不触发真实环境与模型测试 | [race.log](race.log) |
| `node --experimental-default-type=module --test frontend/tests/frames.test.js` | 3 项通过：正常帧、暂停补偿不重复、取消及下一帧 | [frontend.log](frontend.log) |
| JS / Python 语法与 `git diff --check` | 13 个 JS、2 个 Python 文件通过，差异无空白错误 | frontend.log |
| 提交前凭据扫描 | 凭据格式及现有凭据本地无输出比对均无命中 | [secret-scan.log](secret-scan.log) |
| `AIOFCAGE_WORKSPACE_E2E=1 go test ./internal/service -run TestWorkspaceEndToEnd -v -timeout 20m` | 两个模型任务通过，Codex 网络受限跳过；其余生命周期与账本验证通过 | [workspace-e2e.log](workspace-e2e.log) |
| `AIOFCAGE_TERMINAL_E2E=1 go test -race ./internal/service -run '^TestTerminalLifecycleEndToEnd$' -v -timeout 5m` | 通过，未读取用户凭据、未调用模型 | [terminal-e2e.log](terminal-e2e.log) |
| `AIOFCAGE_HEALTH_IDS='sbx-j4mgqw3b sbx-vlbextkx sbx-aokm6c7d' go test ./internal/runtime -run TestDedicatedHealthSnapshot -v` | 通过，三沙箱批采 1,165ms，运行组件和资源上限符合实际 | [health.log](health.log) |

真实测试应只在专用实例运行。完整工作区测试读取现有 `xapi`、`deepseek-cc`、`deepseek-dsh` 凭据引用并产生实际模型用量；临时 store 不改原供应商配置。测试自行删除创建的沙箱、home 和项目。健康命令中的 GUI fixture 已清理，重跑时应替换为当前存在的测试沙箱 ID。

## 跳过、限制与后续补验

1. **Codex 真实任务待补验**：既有七牛 `xapi.qiniu.io` 请求超时。此前沙箱请求由门卫返回 `502 / upstream_failed`，宿主 `/v1/models` 同样无 HTTP 响应；两端 DNS 解析一致。没有把网络超时解释为协议兼容通过，也未修改用户供应商来掩盖问题。待上游恢复后，使用相同测试重跑 Codex 文件任务。
2. DNS 转发旁路未取得充分证据，保留“无法验证”；数据库隔离路径本轮 GUI 跳过。
3. GUI 永久删除流程本轮未补验，最终使用后端清理；完整 OAuth 登录和第三方 MCP 兼容矩阵未在本轮逐项验证。
4. 供应商首期手动切换，新配置重启生效；自动故障切换按已确认范围留到以后。
5. 费用为按人工单价计算的估算，不同步余额、账单和套餐折扣；不同币种不自动换算。
6. 健康趋势只在应用内存中保留最近 120 个样本，重开后重新采集；VM 面板显示配置容量，不宣称是 VM 实时使用量。
7. 容器运行与服务健康分开；未配置健康检查如实展示，不定时发送计费模型请求。授权服务仍可能被智能体滥用，隔离不等于逐命令审批。

## 测试清理

图形测试沙箱已先通过 UI 停止，再以 `cagectl sandbox rm` 删除容器、网络、home 和元数据；测试项目及该沙箱 audit 文件按精确路径移除，保留本报告中的截图与脱敏摘要。`uat-ui-codex`、`uat-ui-dsh` 仅为复用原 KeyRef 的配置副本，已删除，未改动或删除原钥匙串条目。

自动测试资源由 defer 清理。末轮核对：无 `uat-` 项目、无本轮测试沙箱容器/网络/home；默认应用只剩原沙箱 `s3-demo`（停止）和 `s9-db`（运行，Agent / 门卫 / PostgreSQL MCP）。原三个供应商 `xapi`、`deepseek-cc`、`deepseek-dsh` 保留。其他已有数据和 Docker 资源未作批量清理。
