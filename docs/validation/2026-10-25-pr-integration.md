# PR #50—#59 整合与验收（2026-10-25）

本轮覆盖 moercat 提交的十个 PR。逐项 PR 文档保留提交时的说明，本记录补充本轮独立复验；历史记录不代表本轮全部重新通过。

## 合并范围

| PR | 实现范围 |
| --- | --- |
| [#50](https://github.com/shaoxier/AIofCage/pull/50) | 固定 MCP 连接器的沙箱工具权限，显式空权限拒绝所有工具 |
| [#51](https://github.com/shaoxier/AIofCage/pull/51) | 停止时清空门卫授权、删除连接器并清理运行令牌 |
| [#52](https://github.com/shaoxier/AIofCage/pull/52) | 安全诊断与最近隔离检查 |
| [#53](https://github.com/shaoxier/AIofCage/pull/53) | OAuth 本机断开授权 |
| [#54](https://github.com/shaoxier/AIofCage/pull/54) | 会话链路诊断和失败分类 |
| [#55](https://github.com/shaoxier/AIofCage/pull/55) | 最近脱敏审计时间线 JSON 导出 |
| [#56](https://github.com/shaoxier/AIofCage/pull/56) | 保留项目和配置的运行环境重置 |
| [#57](https://github.com/shaoxier/AIofCage/pull/57) | 持久化原生会话恢复元数据 |
| [#58](https://github.com/shaoxier/AIofCage/pull/58) | 本机 MCP 的任务比较 |
| [#59](https://github.com/shaoxier/AIofCage/pull/59) | 本机 MCP 的显式备用供应商 |

按依赖顺序使用 merge commit 合并，再合并整合修复；不直接推送 main，不 squash 历史。

## 整合修复

- 接通终端恢复列表，隐藏已打开会话的重复恢复入口，恢复原先模型和思考强度。
- 保留空数组的 JSON 语义，避免显式拒绝工具在界面显示为默认全选；安全诊断无 MCP 或空授权时仍正常显示。
- 安全边界改为说明，避免在没有探针证据时显示“已执行”。
- 成功控制任务转为 `done` 并释放并发占用；多次尝试的请求/token 用量累计。
- 从 stderr 与结构化错误提取失败分类，向控制接口只返回固定中文建议；中文摘要按完整字符截断。
- 重置不保留 Agent home 时清理失效恢复记录；已经移除的网络与卷使用 Docker 的幂等删除选项，允许重置后再次重置或删除沙箱。

## 文档核对

README、产品规格、当前架构、模块接口、本机调用说明和验证索引已核对。本次补齐 #56—#59 及遗漏的历史验证索引与新增接口，删除当前说明中“没有自动故障切换”的过时说法，并明确桌面与控制接口的不同范围。

检查 40 份 Markdown 的 92 个本地链接，未发现断链；验证目录内每份记录均已纳入索引。

恢复记录不保存消息正文；原生历史依赖保留的 Agent home。隔离检查摘要只保存在当前应用进程。时间线 JSON 是最近最多 300 条事件的快照，每次覆盖同名文件；完整历史使用 JSONL。OAuth 断开只删除本机凭据，不调用上游撤销端点。

## 独立复验

环境：macOS 26.6.1、Apple Silicon、Go 1.26.4、Node 26.3.1，Docker context 为 `colima-aiofcage`。代码基于 #59 的 `f326ab09879a5b93dd37a95ebfacba620fd5ba39` 加上本文所列整合修复，在独立工作树验收后提交。

程序端到端测试使用独立数据目录；桌面验收使用原应用数据中的临时沙箱 `uat-merge-ui`。本轮不改写原有供应商、MCP 服务、沙箱绑定或 VM 资源配置。测试的 DeepSeek Harness 为 0.2.1-rc.2，模型为 `deepseek-flash`。

| 检查 | 实际结果 |
| --- | --- |
| `make check` | 通过，Go vet 与生产构建成功 |
| `go test -race -json ./internal/...`（与 `make test` 相同范围） | 13 个包通过，167 个测试通过、0 失败、19 跳过；未启用的真实环境测试不算通过 |
| 前端状态、帧解析、草稿测试 | 10 项通过 |
| PostgreSQL 策略测试 | 4 项通过 |
| 项目前端 JavaScript 语法 | 23 个文件通过 |
| 新增真实重置/恢复测试 | 通过：重建 App、重置容器并保留项目文件与 Agent home 后，续聊记住原暗号且恢复高思考强度；删除 home 后恢复列表清空；重复清理成功 |
| 新增真实供应商故障切换测试 | 通过：合成无效凭据触发真实认证失败，切到现有供应商成功；状态转为 done、用量累计、对比只返回摘要，随后可停止沙箱 |
| 现有真实控制接口测试 | 通过：两个任务并行、取消、进程清理、控制会话退出后的自建沙箱停止、用量留存 |
| macOS 应用与 cagectl | 构建通过，应用签名验证通过 |

常规 Go 测试中的 19 项跳过包含钥匙串、独立进程辅助入口、下载夹具和需要显式启用的真实环境测试。随后单独启用并通过了新增重置/恢复、备用切换和现有控制接口这 3 个真实测试；没有把其余跳过项补算为通过。

## 桌面验收

| 操作 | 实际结果与截图 |
| --- | --- |
| 原数据基线 | 7 个沙箱、6 个运行中。[开始列表](assets/2026-10-25-pr-integration/01-existing-sandboxes.jpg) |
| MCP 空授权 | 创建测试沙箱，只启用合成演示服务，取消其全部 3 个工具；保存、启动并重新打开设置后仍为 0/3。[配置](assets/2026-10-25-pr-integration/02-empty-tool-grant.jpg)、[安全诊断](assets/2026-10-25-pr-integration/03-security-zero-tools.jpg) |
| 无 MCP 诊断 | 移除测试沙箱的 MCP 后显示“当前沙箱未启用 MCP 服务”，页面正常。[截图](assets/2026-10-25-pr-integration/04-security-no-mcp.jpg) |
| 应用重开后恢复 | 第一轮记住测试暗号；正常退出并重开应用后显示恢复入口。恢复后模型与高思考强度保留，第二轮正确回复 `cage-ui-merge-42`，已打开的会话无重复恢复入口。[首轮](assets/2026-10-25-pr-integration/05-first-turn.jpg)、[恢复入口](assets/2026-10-25-pr-integration/06-recovery-entry.jpg)、[续聊](assets/2026-10-25-pr-integration/07-recovered-context.jpg) |
| 重置确认和执行 | 默认保留 Agent 配置及会话并恢复运行；Esc 取消后仍运行，随后确认重置并成功恢复运行，原恢复入口仍在。[确认弹窗](assets/2026-10-25-pr-integration/08-reset-confirmation.jpg) |
| 时间线导出 | 界面提示成功，实际 JSON 有 8 条事件、权限 0600，无测试提示词或请求正文/参数/认证字段。[导出通知](assets/2026-10-25-pr-integration/09-audit-export.jpg) |
| 隔离检查 | 5 项通过、0 项发现问题、1 项无法验证、1 项跳过；安全中心显示同次检查时间与 5/7 摘要。[检查结果](assets/2026-10-25-pr-integration/10-isolation-summary.jpg)、[安全中心](assets/2026-10-25-pr-integration/11-health-isolation-result.jpg) |
| 清理 | 删除测试沙箱及 Agent home 后，列表恢复为原 7 个沙箱、6 个运行中；测试项目文件夹为空，单独移除。[清理后列表](assets/2026-10-25-pr-integration/12-cleanup.jpg) |

隔离检查的 5 项通过分别为占位凭据配置、未授权域名拒绝、两个固定 IPv4 地址直连阻断、外来凭据拒绝、宿主临时文件与 Docker socket 不可见。DNS 检查只得到 UDP 53 阻断及无默认路由，界面按“无法验证”报告；数据库检查因本测试沙箱未启用数据库 MCP 而跳过。本轮不能据此宣称完整隔离审计通过。

清理过程中观察到一次短暂的“审计采集中断，正在从已保存的位置重试”通知，随后删除成功且列表恢复；本轮未修正该通知出现的时机。它不影响已验证的清理结果，也不能作为日志完整性的证明。

## 未覆盖与工具限制

- OAuth 第三方账号登录、真实令牌到期和上游撤销：跳过；保留用户现有 Linear 授权，不为验收断开既有账号。相关本机断开与脱敏逻辑由常规 Go 测试覆盖。
- 本轮真实恢复与备用切换使用 DeepSeek Harness；Codex、Claude Code 的三客户端全量回归、真实限流与超时备用分支未重新执行，现有测试或历史记录不能替代这次真实验收。
- 原生滚动工具两次返回 `noWindowsAvailable`；重新绑定后应用窗口和检查结果仍可读取。隔离检查下半部分保留完整无障碍文本，截图保留可见摘要。
- Node 26 已移除 `--experimental-default-type=module`，按实际版本改用 `node --test`；Python 脚本测试通过 Go 包装测试执行，未把错误的独立 unittest 调用记为产品失败或通过。

完整命令日志和检查元数据保存在本机忽略目录 `.validation-runs/pr-integration-20261025/`；本文件及 12 张截图为可提交的脱敏证据。

复现入口：

```bash
make gatekeeper-linux connector-linux
make check
make test
node --test frontend/tests/*.test.js frontend/tests/*.test.mjs
node --test internal/runtime/assets/connector/postgres/policy.test.mjs
AIOFCAGE_STACK_E2E=1 go test -race -v -run '^TestStack.*EndToEnd$' -count=1 -timeout=20m ./internal/service
AIOFCAGE_CONTROL_E2E=1 go test -race -v -run '^TestAgentControlEndToEnd$' -count=1 -timeout=15m ./internal/service
make app cagectl
codesign --verify --deep --strict "bin/Ai of Cage.app"
git diff --check
```

PostgreSQL 策略测试首次运行前，在 `internal/runtime/assets/connector/postgres` 执行 `npm ci --ignore-scripts`。真实测试从已配置的 DeepSeek Harness 供应商取得凭据，只由程序内部交付给门卫，不打印凭据；会产生少量真实模型用量。其余需要浏览器账号、其他供应商或专用环境的项目，按本轮实际执行情况分别记录。
