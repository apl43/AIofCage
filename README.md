# AIofCage

> 桌面级 AI Agent 安全沙箱：让 Claude Code、Codex、dsh 放手干活，钥匙永远不进笼子。

Qiniu Builder Day 2026 · 赛道一（产品研发组）参赛项目 · 🚧 开发中

**产品形态：CC Switch + Docker Desktop。** 按 agent 管理模型供应商，key 只存钥匙串；像 Docker Desktop 一样创建、启停沙箱，每个沙箱自选要装的 agent 并绑定供应商，多个沙箱可共用或各用各的供应商，并能看到每个沙箱的用量。详见[产品形态与开发计划](docs/development-plan.md)。

当前仓库已有可构建的桌面与门卫骨架：可登记内存会话草稿、查看环境依赖和模块页面；隔离运行、真实认证、代理转发与 MCP 尚未实现。[产品目标与验收](docs/requirements-and-acceptance.md)和[架构设计](docs/architecture.md)仍是待验证的目标，[比赛规则](docs/competition-rules.md)区分组织方要求与团队选择。当前骨架不能作为已完成的安全沙箱使用。

## 开发启动

桌面目标为 macOS Apple Silicon，需要 Go 1.26.4 或更高版本和 Xcode Command Line Tools。前端采用 Wails 的纯静态资源方式，不依赖 Node、npm 或前端打包步骤。Wails 固定为 v2.17.0。

```bash
make desktop
./bin/AIofCage
```

界面可以选择文件夹并创建会话草稿；选择动作不会复制或挂载文件夹，草稿在退出应用后清除。启动按钮保持禁用，后端启动方法也返回未就绪。服务连接、策略、审计页标明待实现，不接受真实密钥。

```bash
make gatekeeper   # 构建独立门卫二进制
make check        # Go 静态检查与编译（桌面需要 macOS 工具链）
make test         # 后端边界测试，包含竞态检查
make dev          # 可选：使用固定版本 Wails CLI 热开发
```

门卫的本机开发方式及接口状态见[模块与开发说明](docs/modules.md)。门卫当前始终拒绝代理和 MCP 请求，不能配置成可转发状态；应用不会创建 VM、容器或修改用户的 Docker context。

独立的[第一批验证包](experiments/validation/README.md)提供环境检查、最小门卫测试、容器隔离探针和可选 Codex 真实任务；[验证记录](docs/validation/README.md)区分实测结果与阻塞项。实验通过不等于产品功能已完成或设计已冻结。

## 要解决的问题

Claude Code、Codex 这类编程 Agent 能直接在你的电脑上执行命令。它们会犯错，也可能被网页或 README 里藏的恶意指令"下套"（提示词注入），结果可能是删错文件，或者把 `~/.ssh`、环境变量里的 API Key 发到外面。

现有的沙箱方案要么只隔离命令、不管密钥，要么全靠命令行和配置文件，普通用户用不起来。

## 思路：隔离 + 门卫换钥匙

```
┌─── AIofCage 桌面应用 ─────────────────────────────────────┐
│  钥匙串：API Key / MCP 密钥的唯一持久化位置               │
│  会话管理：钥匙串、策略下发、事件订阅、JSONL 审计           │
│  操作界面：选文件夹 → 选 Agent → 一键启动                   │
└───────────────────────────┬───────────────────────────────┘
                            │ 管理 API + SSE，随机 token 鉴权
                            ▼
        专用 Colima VM：门卫容器执行网络策略、模型注入与 MCP 网关
                            │ 可信连接器按服务隔离，代持数据库等凭据
                            ▲
                            │ 内部网络代理
        Agent：选定项目、模型占位符及受限 MCP 内部凭据，无直接外网
```

1. **隔离**：Agent 跑在容器里；宿主目录仅暴露选定项目，项目内文件仍可能被修改或删除。
2. **出网管控**：容器没有直接出网的路，所有请求必须经过门卫代理，不在白名单里的一律拦截。
3. **Agent 不持有上游真实密钥**：秘密持久化在钥匙串，宿主传输时短暂持有，可信门卫与按服务隔离的连接器按需在运行期持有。模型走授权后注入；MCP 由网关代理工具调用，OAuth 登录在宿主完成，沙箱只有受限的内部调用能力。
4. **对普通用户友好**：图形界面操作，添加密钥就像填表，不用写一行配置。

## 功能规划

- [x] 可构建的 Wails 桌面入口、模块页面和内存会话草稿
- [x] 独立门卫入口、管理鉴权及未配置时始终拒绝的数据入口
- [ ] 容器隔离 + 强制所有出网走门卫代理
- [ ] 钥匙串存储密钥，门卫按域名注入
- [ ] 供应商管理（参考 CC Switch）：按 agent 分组，预设一键添加，key 存钥匙串，连通性测试
- [ ] 沙箱管理（参考 Docker Desktop）：创建时勾选 agent、绑定供应商；启停、删除、打开终端、切换供应商
- [ ] 每个沙箱的用量与资源占用，按供应商汇总
- [ ] 实时请求面板：放行 / 拦截 / 已注入密钥
- [ ] 支持 Codex、Claude Code、dsh
- [ ] MCP 免填上游秘密：HTTP Token、OAuth、无密钥 stdio、数据库密码四类基线
- [ ] MCP 服务、工具与资源权限；连接、撤销、过期处理及跨会话隔离
- [ ] 用量上限与异常告警，防止被下套的 Agent 刷光额度
- [ ] 一键"模拟攻击"演示

权限边界：文件范围由 VM 共享和容器挂载执行；网络与模型 API 权限由门卫执行，MCP 工具/资源权限由协议网关执行。第一版不提供逐命令审批或完整文件访问审计。被拦请求结束后，用户只能授权后续访问，由客户端重新请求；代理不重放请求体。

审计边界：门卫生成脱敏请求与用量事件，应用生成授权和会话事件并追加写 JSONL。断线补发超出缓冲区时显示日志缺口；终端文本和文件 diff 不当作完整操作审计。

兼容性目标：分别验证 Codex、Claude Code、dsh 的真实任务及 MCP 调用；MCP 基线覆盖 GitHub HTTP、OAuth 服务、filesystem stdio 和密码型数据库连接器。候选组件、版本和证据见[验收文档](docs/requirements-and-acceptance.md)。一个 Agent 和一个 HTTP MCP 是首个里程碑；OAuth 或密码连接器缺失仍是议题覆盖缺口，不能自动降为可选项。有限样本不能证明“所有主流”均可用。

目前先建立工程骨架，关键路线仍需通过需求、隔离、Agent 认证、MCP/OAuth、秘密与权限、接口与工作量六项关口。入口可编译不等于上述验证通过，也不能保证比赛前全部可达成。

## 技术栈

- Go
- [Wails](https://github.com/wailsapp/wails)：桌面界面
- [goproxy](https://github.com/elazarl/goproxy)：计划用于 MITM 门卫，尚未接入
- Docker：容器隔离
- Colima：专用 profile `aiofcage`，Docker context `colima-aiofcage`，仅共享 `~/CageWorkspace:w`
- macOS 钥匙串：密钥存储

## 致谢

设计上参考了 [mattolson/agent-sandbox](https://github.com/mattolson/agent-sandbox)（MIT）和 [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) 的"代理注入凭证"思路。
