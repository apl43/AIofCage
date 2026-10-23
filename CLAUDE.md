# AIofCage

Qiniu Builder Day 2026 赛道一参赛作品：桌面级 AI Agent 安全沙箱。

题目原文要求：
- 核心是安全，其次是最终用户的友好性，不能是太技术化的配置
- 支持在沙箱内跑主流 agent 应用，如 claude code、codex、dsh 等
- 所有主流 MCP 配置时均无需在沙箱内填写任何敏感信息（api key、password 等）

## 架构（已定）

当前执行设计见 `docs/architecture.md`（v2），比赛要求原文摘要见 `docs/competition-rules.md`。文档描述是实现目标，不代表功能已经完成；旧 ADR 标记为被取代时不得照旧实现。

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
- **密钥**：持久化只在 macOS 钥匙串。桌面应用读取后，通过带 token 的管理接口下发给门卫；应用传输时短暂持有，门卫在运行期内存持有，Agent 不持有真值。
- **MCP**：HTTP 类的 MCP 在沙箱里只配占位符，由门卫注入真密钥。非 HTTP 的凭证（比如数据库密码）不在第一版范围内。
- **权限与审计边界**：第一版文件权限靠挂载，网络权限靠门卫；不声称逐命令审批、全部文件访问日志或容器逃逸后的密钥保护。授权仅影响后续请求，代理不缓存、重放请求体。MCP 共用 POST 入口时，HTTP 方法权限不能替代工具权限。
- **参考**：mattolson/agent-sandbox（MIT 协议）的“边车代理 + 防火墙”设计；Docker Sandboxes（sbx）的占位符方案。

## 前期实验记录（2026-10-22，在 Docker sbx 里实测）

这些是团队记录的其他运行环境实验，需在 AIofCage 专用实例独立复现，补充客户端版本、脚本和脱敏结果后再标为本项目已验证。

- 占位符替换可行：沙箱的环境变量里只有占位符，代理对 xapi.qiniu.io 替换后调用成功；发往白名单以外的域名返回 403；在沙箱的文件和环境变量里搜真 key，0 处。
- Codex 0.149.1 不认识 gpt-6 系列（报 `Model metadata … not found`，`/model` 列不出模型），需要 ≥ 0.161.0。Codex 的模型列表来自程序自带的目录，不读中转站的 `/v1/models`。
- Codex 自定义 provider 的写法：`[model_providers.xxx]` 写 `base_url`、`env_key`、`wire_api = "responses"`。Codex 要信任代理的 CA（sbx 的做法是把 CA 装进系统信任库）。
- 比赛用的模型接口：`https://xapi.qiniu.io/v1`，OpenAI 兼容，`/chat/completions` 和 `/responses` 都能用。模型有 gpt-5.5、gpt-5.6-sol、gpt-5.6-terra、gpt-6-astra、gpt-6-sol、gpt-6.1-sol。
- 比赛 key 存在钥匙串里，service 名是 `sbx-builderday-key`，读取命令：`security find-generic-password -a "$USER" -s sbx-builderday-key -w`。**不要写进代码、仓库或日志。**

## 待验证

- Claude Code 在沙箱里怎么认证：考虑用 `CLAUDE_CODE_OAUTH_TOKEN` 放占位符，由门卫对 api.anthropic.com 注入。
- dsh 用哪个 provider、从哪里读 key。
- 在 Docker Desktop for Mac 上，“internal 网络 + 边车容器”能不能真正挡住直接出网。

## MVP 范围（按这个顺序做）

1. 核心链路：容器 + 边车门卫 + 占位符注入，让 Codex 用 xapi 跑通；验证外传 key 会失败。
2. 桌面应用：密钥管理（钥匙串）、沙箱启停、一键打开 Agent 终端、实时请求日志。
3. MCP（真实 Token 由门卫持有，沙箱只配占位符）为核心交付；逐项验证 Claude Code 和 dsh，不把任意命令入口算作兼容性通过。
4. 产品验收：门卫用量上限与 80% 告警、“模拟攻击”按钮、AI 安全解说；不在安全链路未完成时提前标记通过。

用量上限和 80% 告警已在 v2 设计中列为 P0，由门卫执行；AI 解说不参与授权和拦截。Hash 链、逐命令审批、工具级 MCP 授权和 OAuth 支持列为路线图。

## 约定

- 文档、README 和界面文案都用中文。
- 永远不要提交密钥；提交前扫描 `sk-`、`ghp_` 这类字符串。
- README 里的功能清单，完成一项勾一项。
- Git：一个功能一个分支一个 PR，不直接往 `main` 推。分支名用 `feat/xxx`、`fix/xxx`、`docs/xxx`，合并用 squash。队友不 fork，直接在本仓库开分支。
