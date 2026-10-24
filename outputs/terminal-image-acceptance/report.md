# 终端图片附件验收

日期：2026-10-24。功能分支：`feat/terminal-image-attachments`，基于最终工作区验收分支，依赖 [PR #33](https://github.com/shaoxier/AIofCage/pull/33)。

## 结果与边界

已实现智能体终端中的图片粘贴、文件选择、拖入处理、附件预览和发送。Claude Code 与 DeepSeek Harness 已完成真实图片识别；Codex 已将两张图片转换为原生附件并提交，但七牛上游返回 502，未完成模型识图验收。跨应用拖入因界面控制工具报错，未完成实测。

测试使用独立应用和配置目录、一次性沙箱 `uat-image-acceptance`（`sbx-fshacqi7`）。测试图为自制的 [uat-image.png](uat-image.png)：左侧红色正方形、右侧蓝色三角形、底部数字 `7319`。识图提示只要求描述图形、颜色和数字，未提供答案。剪贴板图片通过 Preview 界面的复制操作取得。

## 实际界面验证

| 验收项 | 实际结果 | 证据 |
| --- | --- | --- |
| 输入框 `Command+V` | 通过：图片进入预览，发送前保留在草稿中 | [01](01-paste-preview.jpg) |
| 原生终端区域 `Command+V` | 通过：图片进入当前智能体的预览，未作为终端命令写入 | [01](01-paste-preview.jpg) |
| “粘贴图片”和“添加图片” | 通过：分别从原生剪贴板、文件选择器添加图片 | [01](01-paste-preview.jpg)、[02](02-independent-draft.jpg) |
| macOS `Ctrl+V` | 通过：在 Harness 终端添加图片预览 | [08](08-harness-vision.jpg)；实际操作中验证 |
| 独立草稿与多行文字 | 通过：切换终端不串文字或附件；多行内容与两张图片一起提交 | [02](02-independent-draft.jpg)、[03](03-codex-submitted.jpg) |
| Codex 原生附件 | 通过：终端显示 `[Image #1] [Image #2]` 并进入请求状态 | [03](03-codex-submitted.jpg) |
| Codex 最终识图 | 跳过：七牛上游反复返回 `502 gatekeeper upstream_failed`，已停止本次测试请求，不能据此判断模型识图结果 | [06](06-codex-upstream-limit.jpg) |
| Claude Code 识图 | 通过：DeepSeek-CC / `deepseek-flash` 正确描述两种图形、颜色及 `7319` | [05](05-claude-vision.jpg) |
| Claude Code 纯图一次发送 | 通过：修复粘贴与提交时序后，新终端仅添加图片、点击一次发送，未补按回车，自动完成识图 | [09](09-claude-one-click.jpg) |
| Harness 图片能力设置 | 通过：隔离配置中勾选“模型支持图片输入”，重启测试沙箱后生效 | [07](07-harness-image-setting.jpg) |
| Harness 识图 | 通过：实际调用 `read_image`，正确回答图形、颜色和 `7319` | [08](08-harness-vision.jpg) |
| 数量限制 | 通过：保留已添加的 4 张，拒绝第 5 张并显示错误 | [10](10-image-limit.jpg) |
| 离开详情再返回 | 通过：返回后 4 张图片和数量错误仍保留 | [10](10-image-limit.jpg)；实际往返操作中验证 |
| Shell 拒绝图片 | 通过：提示切换到智能体终端，未将图片当作 Shell 命令 | [11](11-shell-refuses-image.jpg) |
| 跨应用拖入 | 跳过：Preview 拖拽调用返回 `noWindowsAvailable`，未完成真实拖入；前端处理已实现，不视为实测通过 | 无成功操作证据 |
| 损坏图片与超宽图片的界面拒绝 | 跳过：原生选择器的“打开”按钮不可用，未到达应用后端；后端限制由自动测试覆盖 | 见下一节测试 |
| 删除测试沙箱 | 通过：经界面删除沙箱及其 home，列表为空 | [12](12-cleanup.jpg) |

首次 Claude Code 测试发现：粘贴与回车同时写入时，正文留在原生输入区，需要补按回车才发送。[04](04-claude-before-submit-fix.jpg) 保留修复前证据，[05](05-claude-vision.jpg) 是当时补按回车后的识图结果；最终结果以修复后的 [09](09-claude-one-click.jpg) 为准。

Harness 的图片能力选项必须与供应商实际模型能力一致，默认仍为文本；运行中的沙箱需要重启。模型关于“图片实际为 JPEG”的附加描述不作为文件格式证据，测试输入本身为 PNG，字节校验由容器测试完成。

## 自动测试与构建

| 检查 | 结果 | 记录 |
| --- | --- | --- |
| `make app` | 通过，生成 `bin/AIofCage.app` | [build.log](build.log) |
| `make check` | 通过，完成 `go vet` 与生产构建 | [check.log](check.log) |
| `go test -race ./internal/...` | 通过，覆盖图片内容、尺寸、体积、数量、控制字符、各智能体输入协议、供应商能力持久化等 | [go-test.log](go-test.log) |
| `AIOFCAGE_TERMINAL_E2E=1 go test -race ./internal/service -run '^(TestTerminalImagesEndToEnd\|TestTerminalLifecycleEndToEnd)$' -v` | 两项通过；验证真实容器内图片字节、`0600`/`0700` 权限、项目隔离、失败清理、符号链接拒绝，以及终端生命周期；不调用模型 | [container-test.log](container-test.log) |
| `node --test frontend/tests/*.test.js` | 8 项通过；包含独立草稿、添加校验、失败重试、释放预览、防重复提交、关闭时取消 | [frontend-test.log](frontend-test.log) |
| JavaScript / Python 语法检查 | 14 个 JavaScript、3 个 Python 文件通过 | [syntax.log](syntax.log) |
| `codesign --verify --deep --strict bin/AIofCage.app` | 通过 | 本轮命令返回 0 |
| 提交前复查 | `git diff --check` 通过；新增文本与 diff 的凭据规则扫描未发现新增疑似凭据，已有测试样例匹配已与基线核对 | 本轮命令结果 |
| 截图文件检查 | 12 张截图均可完整解码，尺寸为 2360 × 1664；已检查联系表 | 本目录 01–12 的 JPEG 原始截图 |

构建存在已有的重复链接 `-lobjc` 警告，无编译或链接错误。

## 复查与清理

- 附件只在发送时保存到当前沙箱 home 的随机批次目录，文件内容不放入 Docker 命令参数；不写入项目目录。发送失败清理本批附件，成功附件保留以便智能体后续读取。
- 后端独立解码并验证图片；目录逐级拒绝符号链接。Shell 在前后端均拒绝图片；正文拒绝终端控制字符。
- 通过界面删除测试沙箱及其 home 后，按 `sbx-fshacqi7` 精确查询容器、网络、卷，均无残留；空测试项目目录和独立测试应用、配置目录均已清理。
- 用户原有的运行容器 `aiofcage-sbx-vlbextkx-mcp-shopdb`、`aiofcage-sbx-vlbextkx-agent`、`aiofcage-sbx-vlbextkx-gate`、`aiofcage-demo-postgres` 均仍在运行。
- 本功能位于独立工作树 `/Users/dumpling/AIofCage-terminal-images`，构建产物为该目录下的 `bin/AIofCage.app`。原工作树正在进行的品牌图标改动未纳入本功能；未关闭用户原有应用会话。

使用与配置说明见 [终端图片附件](../../docs/terminal-images.md)。
