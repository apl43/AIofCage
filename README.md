# AIofCage

> 桌面级 AI Agent 安全沙箱：让 Claude Code、Codex、dsh 放手干活，钥匙永远不进笼子。

Qiniu Builder Day 2026 · 赛道一（产品研发组）参赛项目 · 🚧 开发中

## 要解决的问题

Claude Code、Codex 这类编程 Agent 能直接在你的电脑上执行命令。它们会犯错，也可能被网页或 README 里藏的恶意指令"下套"（提示词注入），结果可能是删错文件，或者把 `~/.ssh`、环境变量里的 API Key 发到外面。

现有的沙箱方案要么只隔离命令、不管密钥，要么全靠命令行和配置文件，普通用户用不起来。

## 思路：隔离 + 门卫换钥匙

```
┌─── AIofCage 桌面应用 ─────────────────────────────────────┐
│  钥匙串：所有 API Key / MCP 密钥只存在 macOS 钥匙串        │
│  门卫代理：白名单放行 + 占位符换真 key + 用量上限 + 日志   │
│  沙箱管理：选文件夹 → 选 Agent → 一键启动                   │
└───────────────────────────┬───────────────────────────────┘
                            ▼
        沙箱容器：只挂载项目文件夹，出网只能经过门卫
        Claude Code / Codex / dsh 在里面运行，只拿得到占位符
```

1. **隔离**：Agent 跑在容器里，只能看到当前项目文件夹。
2. **出网管控**：容器没有直接出网的路，所有请求必须经过门卫代理，不在白名单里的一律拦截。
3. **密钥不进沙箱**：沙箱里只有假 key（占位符）。请求发往指定域名时，门卫才从钥匙串取出真 key 替换进去；发往其他地方的请求，真 key 永远不会被带出去。
4. **对普通用户友好**：图形界面操作，添加密钥就像填表，不用写一行配置。

## 功能规划

- [ ] 容器隔离 + 强制所有出网走门卫代理
- [ ] 钥匙串存储密钥，门卫按域名注入
- [ ] 桌面应用：密钥管理、沙箱启停、一键打开 Agent 终端
- [ ] 实时请求面板：放行 / 拦截 / 已注入密钥
- [ ] 支持 Codex、Claude Code、dsh
- [ ] MCP 免填密钥（以 GitHub MCP 为例）
- [ ] 用量上限与异常告警，防止被下套的 Agent 刷光额度
- [ ] 一键"模拟攻击"演示

## 技术栈

- Go
- [Wails](https://github.com/wailsapp/wails)：桌面界面
- [goproxy](https://github.com/elazarl/goproxy)：门卫代理
- Docker：容器隔离
- macOS 钥匙串：密钥存储

## 致谢

设计上参考了 [mattolson/agent-sandbox](https://github.com/mattolson/agent-sandbox)（MIT）和 [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) 的"代理注入凭证"思路。
