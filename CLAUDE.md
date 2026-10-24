# AIofCage

Qiniu Builder Day 2026 赛道一参赛作品：桌面级 AI Agent 安全沙箱。

题目原文要求：
- 核心是安全，其次是最终用户的友好性，不能是太技术化的配置
- 支持在沙箱内跑主流 agent 应用，如 claude code、codex、dsh 等
- 所有主流 MCP 配置时均无需在沙箱内填写任何敏感信息（api key、password 等）

## 设计目标与编码前置条件

需求与验收以 `docs/requirements-and-acceptance.md` 为准，架构候选见 `docs/architecture.md`（v3 草案），比赛要求见 `docs/competition-rules.md`。按最新开发安排先搭可构建工程骨架，当前实现范围和模块接口见 `docs/modules.md`；这不表示 G0—G5 已通过或设计已冻结。真实隔离、凭据与 MCP 接入仍须完成对应验证，不得用模拟成功替代。旧 ADR 标记为被取代时不得照旧实现。

- 构建：`make desktop`、`make cagectl`（都会先编译并嵌入 Linux 门卫）、`make gatekeeper`；检查：`make check`、`make test`。桌面编译使用 `production` tag；前端为 `frontend/src` 静态资源。
- 门卫（`internal/gatekeeper`）在经 `PUT /config` 下发策略与密钥后才转发，未配置时一律 503；MCP 入口仍拒绝。沙箱运行时（`internal/runtime`）与 `cagectl` 已能创建并启动沙箱；桌面应用已接入供应商管理、沙箱管理、实时请求日志（JSONL 审计）与用量监视（`internal/service`）；远程 HTTP MCP（Bearer 令牌注入）与内置 filesystem MCP 已接入；AI 解说与模拟攻击待接入。前端为 Preact + htm（`frontend/src/vendor`），无 npm 构建。Codex 只支持 Responses API。

- Go 单仓库，桌面界面用 Wails。
- 专用 Colima profile `aiofcage`、Docker context `colima-aiofcage`，vz + virtiofs，4 核 / 6 GiB / 40 GiB；唯一共享目录 `~/CageWorkspace:w`。所有 Docker 命令显式指定 context，不修改用户默认实例。
- **隔离**：Docker 容器，项目目录按相同路径挂载进去，其他什么都不挂。
- **强制出网走门卫**：agent 容器只接入 `--internal` 网络；门卫代理是一个边车容器，同时接入 internal 网络和外网；agent 容器的 `HTTP(S)_PROXY` 指向门卫。
- **门卫代理**：用 Go + elazarl/goproxy 做 HTTPS 中间人代理。
  - 自签 CA，装进 agent 容器的系统信任库，同时设置 `NODE_EXTRA_CA_CERTS`。
  - 先按域名、端口、方法与路径授权，显式 deny 优先，再校验占位符与注入密钥；不能因域名有密钥就直接放行。
  - 对配置了密钥的域名，把请求头里的占位符替换成真 key。
  - 直接拒绝外来或缺失认证值；固定 goproxy v1.9.2，上游证书必须正常校验。
  - 门卫生成脱敏事件、执行额度上限；桌面应用订阅 SSE 并追加写 JSONL，记录补发缺口。
- 管理端口与代理端口分离，管理接口只向宿主回环发布并用随机 token 鉴权；Agent 和代理都不能访问管理接口。
- **密钥**：持久化只在 macOS 钥匙串。宿主传输时短暂持有；门卫和批准的连接器按需在运行期持有本服务秘密。OAuth 刷新令牌优先仅由宿主持有；Agent 不获得上游秘密。
- **MCP**：门卫增加独立 MCP 入口 `8890`，用会话、服务和工具绑定的短期内部凭据鉴权，不能用它透传上游 token。HTTP/OAuth 由可信网关连接；有密码的 stdio/数据库服务由宿主编排的可信连接器运行，Agent 不能直达。无密钥本地 stdio 可留在沙箱。详见目标文档 §4；旧 HTTP 注入例子仅是实验链路，不能替代网关工具权限。
- **权限与审计边界**：文件权限靠挂载，网络权限靠门卫，MCP 工具与参数权限靠协议网关；不声称逐命令审批、全部文件访问日志或容器逃逸后的密钥保护。授权仅影响后续请求，代理不缓存、重放请求体。MCP 参数与结果不能写入审计；内部凭据也须脱敏。
- **参考**：mattolson/agent-sandbox（MIT 协议）的“边车代理 + 防火墙”设计；Docker Sandboxes（sbx）的占位符方案。

## 前期实验记录（2026-10-22，在 Docker sbx 里实测）

这些是团队记录的其他运行环境实验，需在 AIofCage 专用实例独立复现，补充客户端版本、脚本和脱敏结果后再标为本项目已验证。

- 占位符替换可行：沙箱的环境变量里只有占位符，代理对 xapi.qiniu.io 替换后调用成功；发往白名单以外的域名返回 403；在沙箱的文件和环境变量里搜真 key，0 处。
- 团队记录曾出现 Codex 0.149.1 不识别所选模型、较新版本可用的情况。该实验不能推出通用最低版本，需在 G2 固定实际客户端、模型和协议后复测。
- Codex 自定义 provider 的写法：`[model_providers.xxx]` 写 `base_url`、`env_key`、`wire_api = "responses"`。Codex 要信任代理的 CA（sbx 的做法是把 CA 装进系统信任库）。
- 比赛用的模型接口：`https://xapi.qiniu.io/v1`，OpenAI 兼容，`/chat/completions` 和 `/responses` 都能用。模型有 gpt-5.5、gpt-5.6-sol、gpt-5.6-terra、gpt-6-astra、gpt-6-sol、gpt-6.1-sol。
- 比赛 key 存在钥匙串里，service 名是 `sbx-builderday-key`，读取命令：`security find-generic-password -a "$USER" -s sbx-builderday-key -w`。**不要写进代码、仓库或日志。**

## 待验证（不构成已选定的认证路线）

- Claude Code 在沙箱里怎么认证：考虑用 `CLAUDE_CODE_OAUTH_TOKEN` 放占位符，由门卫对 api.anthropic.com 注入。
- dsh 用哪个 provider、从哪里读 key。
- 在 AIofCage 专用 Colima 实例上，直连、IPv6/DNS、管理面和跨会话访问是否全部受控。
- 议题所指 dsh 的产品身份与 Linux/arm64 产物，OAuth 注册条件、MCP SDK 能力、数据库连接器及秘密交付通道，见目标文档 G2—G4。

## 设计冻结后的实现顺序

1. 最小闭环：专用隔离 + 门卫 + 一个 Agent 真实任务 + 一个 HTTP MCP，只作为阶段结果。
2. 议题基线：三款 Agent、四类 MCP、工具/资源权限、撤销与故障关闭；不以自定义命令入口算兼容。
3. 普通用户流程：环境准备、导入、宿主认证、启动、拒绝与重试、停止清理；无需手改 Docker、CA 或 JSON。
4. 安全及交付验收：反向测试、审计、运行说明、模块规格和真实分工、录屏。用量保护随后补齐；AI 解说、丰富模拟攻击和视觉细节有余量再做。

OAuth、密码连接器和 MCP 工具/资源授权是覆盖目标的一部分，缺失时明确阻塞，不能只改成路线图后宣称满足议题。Hash 链、逐命令审批、写操作的交互式审批和高级策略编辑器可延后；未经授权的写工具保持拒绝。AI 解说不参与授权和拦截。

## 约定

- 文档、README 和界面文案都用中文。
- 永远不要提交密钥；提交前扫描 `sk-`、`ghp_` 这类字符串。
- README 里的功能清单，完成一项勾一项。
- Git：一个功能一个分支一个 PR，不直接往 `main` 推。分支名用 `feat/xxx`、`fix/xxx`、`docs/xxx`，合并用 squash。队友不 fork，直接在本仓库开分支。
