# Ai of Cage

Ai of Cage，寓意“笼中 AI”，是结合独立沙箱的桌面智能体工作区：为 Codex、Claude Code 和 DeepSeek Harness 配置供应商，在沙箱中工作，并查看用量、费用估算和运行状态。

Qiniu Builder Day 2026 · 赛道一参赛项目，当前面向 macOS Apple Silicon。

## 如何使用

1. 在“模型供应商”中为智能体添加配置。一个智能体可以保存多个供应商，密钥保存在 macOS 钥匙串。
2. 点击“创建沙箱”，选择或新建项目。默认创建空白沙箱，也可以勾选需要的智能体。
3. 启动沙箱，在详情中打开 Shell 或智能体。创建后仍可添加三款智能体。
4. 在“智能体”页为每个智能体独立选择供应商。多个沙箱可以共用配置，也可以分别选择。运行中的更改在重启沙箱后生效。
5. 在“用量”“运行状态”和“日志”中查看实际请求、资源和诊断结果。

更新正在使用的 API Key 前，需要先停止引用它的沙箱；接口、模型和价格可保存为待生效配置。

最新范围与验收标准见[桌面工作区产品规格](docs/product-workspace.md)，本次改造的实测、限制和 PR 对应关系见[交付验收记录](outputs/workspace-acceptance/report.md)。

## 功能清单

- [x] 按三款智能体分类管理多个供应商，支持预设、自定义接口、复制和连接测试。
- [x] 每个沙箱独立绑定供应商；保存的选择与当前生效配置分别记录，重启后应用。
- [x] 默认空白沙箱、可选安装、创建后增装；保留项目和持久化主目录，增装失败尝试恢复原配置。
- [x] 可视化创建、搜索、筛选、启停及重启；内置终端、任务输入和系统终端入口。
- [x] 页面切换保留终端会话；沙箱停止、重建或应用退出时结束会话。
- [x] 智能体结构化会话、单一输入框、模型与思考强度选择；Shell 保留原生终端，统一新建菜单。
- [x] 检查三款官方 CLI 的更新，选择新沙箱默认版本；已有沙箱独立更新，保留项目和主目录，失败回滚。
- [x] 智能体终端支持图片粘贴、选图、拖入及附件预览；草稿按终端保存。[图片使用说明](docs/terminal-images.md)
- [x] 统一 SVG 图标、系统字体与较大控件；窄窗口布局、固定表单操作区和键盘可访问表格。[界面验收](outputs/ui-polish-acceptance/report.md)
- [x] 持久化请求明细、供应商/模型/时间筛选、输入/输出/缓存 token、未知用量与日志缺口提示。
- [x] 人工填写模型单价，按请求保存历史价格快照；CNY/USD 分开汇总，缺价格或用量时显示未计价。
- [x] Agent、门卫和 MCP 连接器的状态、CPU、内存、进程数、限制、OOM、退出与重启信息。
- [x] 有界的 CPU/内存趋势、累计网络和磁盘 I/O、采样时间及失败提示；单独展示专用 VM 的配置。
- [x] 隔离检查区分“通过、发现问题、无法验证、跳过”，保留具体证据。
- [x] 请求额度、token 额度及接近上限提醒；脱敏日志写入本地 JSONL。
- [x] HTTP Token、OAuth、filesystem 和 PostgreSQL MCP 接入路径；上游秘密由宿主、门卫或可信连接器持有。
- [x] 基于脱敏请求元数据的 AI 解说，独立于授权和拦截。
- [ ] 供应商自动故障切换，当前使用手动选择。
- [ ] 供应商账单、余额和真实结算同步，当前费用为按所填单价计算的估算。
- [x] 从公开价格目录查找同名模型、填入参考单价，支持自定义中转价及来源日期；历史费用保留当时快照。
- [ ] 完整 OAuth 账号授权与各种第三方 MCP 的兼容性矩阵；已有连接路径不代表所有服务已验证。

## 构建与检查

需要 Go 1.26.4 或更高版本、Xcode Command Line Tools、Docker CLI 与 Colima（可通过 `brew install colima docker` 安装）。Wails 固定为 v2.17.0。前端使用固定版本的 Preact、htm、xterm.js，直接嵌入应用，无 npm 构建步骤。

```bash
make app                 # "bin/Ai of Cage.app"
open "bin/Ai of Cage.app"     # 或在访达里双击
make check               # Go 静态检查与编译
make test                # 后端测试与竞态检查
make cagectl             # 共用相同运行时的命令行工具
make dev                 # Wails 热开发
```

应用 Logo 以笼体围住 AI 核心，表达“笼中 AI”。矢量源文件为 `frontend/src/assets/logo.svg`，界面与 macOS 图标使用同一设计。修改后运行 `make icons`，通过系统 WebKit 和 `iconutil` 重新生成 `packaging/macos/AppIcon.png`（1024 像素）与 `AppIcon.icns`（16—1024 像素），再运行 `make app` 打包。

前端调度回归测试使用 Node 内置测试器：

```bash
node --experimental-default-type=module --test frontend/tests/frames.test.js
```

真实环境测试默认跳过，启用方法、测试供应商和清理情况见[验收记录](outputs/workspace-acceptance/report.md)。真实模型任务会调用已配置供应商并产生用量。

## 运行与安全边界

应用使用专用 Colima profile `aiofcage` 和 Docker context `colima-aiofcage`，默认 VM 配置为 4 核、6 GiB 内存、40 GiB 磁盘，仅共享 `~/CageWorkspace:w`。Docker 操作始终显式指定 context，不修改用户默认实例。

Agent 容器仅连接内部网络，通过每个沙箱独立的门卫访问已授权服务。门卫校验域名、端口、方法、路径和占位凭据，再注入上游秘密；管理接口仅向宿主回环开放并单独鉴权。数据库等需要秘密的 MCP 运行在可信连接器中。

秘密持久化在钥匙串，宿主传输、门卫及连接器在运行期按需持有。Agent 获得模型占位符及受限 MCP 内部凭据。此设计限制密钥泄漏和未授权出网，已授权模型或工具仍可能被滥用。项目目录中的文件可被智能体修改或删除，应自行保留版本与备份。

运行状态与服务健康分别展示：容器运行不代表模型可用，未配置容器健康检查时明确显示。面板不会为探活定时发送计费模型请求。隔离探针只能说明所测路径，DNS 超时和执行失败均不视为隔离通过。

请求审计不记录正文、上游凭据、MCP 参数与结果；终端内容不作为完整操作审计。应用不提供逐命令审批，也不声称记录全部文件访问或防御容器逃逸。断线超出补发范围时显示日志缺口。

## 相关文档

- [本次产品规格与 12 步验收矩阵](docs/product-workspace.md)
- [交付验收记录](outputs/workspace-acceptance/report.md)
- [原始产品目标与安全验收](docs/requirements-and-acceptance.md)
- [架构设计](docs/architecture.md)、[模块与开发说明](docs/modules.md)
- [早期验证记录](docs/validation/README.md)、[实验验证包](experiments/validation/README.md)
- [比赛规则](docs/competition-rules.md)

## 致谢

界面流程参考 [CC Switch](https://github.com/farion1231/cc-switch) 和 Docker Desktop；代理注入设计参考 [mattolson/agent-sandbox](https://github.com/mattolson/agent-sandbox) 与 [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)。第三方前端库来源和许可证见 [vendor 说明](frontend/src/vendor/README.md)。
