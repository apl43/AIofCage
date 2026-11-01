# 终端“新建”菜单鼠标点选修复

[验收记录索引](README.md)

## 问题

11-01 决赛前全量实测的问题 F02：在沙箱详情的“终端”页点“新建”，菜单能展开，再用鼠标点 Codex、Claude Code 或 Shell 都没有反应，不开新会话。同一轮测试里，Claude Code 在长任务沙箱里一次模型请求都没有，和这个问题一致。

## 原因

- macOS 的 WebKit 点按钮时不把焦点交给按钮，而是交给最近的可聚焦祖先。界面的 `<main id="main-content" tabindex="-1">` 可以聚焦，焦点就从“新建”移到了 `<main>`。
- 菜单 `<details>` 的 focusout 处理是“焦点移到菜单外就收起”。`<main>` 在菜单外，所以鼠标一按下菜单就收起了；抬起时选项已经不在原处，click 落在下面的终端面板上。
- 这段 focusout 是 10-25 的菜单修复（2052279）加的。当时用方向键和 Enter 验证，键盘选择时焦点就在菜单里的按钮上，不受影响，所以没发现。内置浏览器（Chromium）点按钮会把焦点交给按钮本身，也看不出这个问题。

## 改动

`frontend/src/js/sandbox-detail.js`：在菜单选项上按下鼠标时取消默认动作（`preventDefault`），焦点不移动，菜单保持展开，直到选项的 click 处理完。供应商表单和终端里的模型下拉，选项本来就是这样做的。focusout、点菜单外收起、方向键和 Escape 的逻辑都没改。

## 实测

用 WKWebView（macOS 26.6.1，WebKit 21624.4.5.11.5）加载真实前端 `frontend/src` 和模拟后端（只返回固定数据：一个运行中的沙箱，绑定 Codex），向窗口发真实的鼠标和键盘事件。同一套步骤分别跑 main（3d1bb4c）和本分支：

| 步骤 | main | 本分支 |
|---|---|---|
| 鼠标点“新建” | 菜单展开 | 菜单展开 |
| 鼠标点 Codex | 菜单收起，没有新会话，焦点停在 `<main>` | 开出 Codex 会话（`StartConversation` 收到 codex） |
| 再点“新建”，点 Shell | 没有新会话 | 开出 Shell 会话，共 2 个标签 |
| 打开菜单后点菜单外的“项目文件”按钮 | 收起 | 收起 |
| 焦点在“新建”上按 ↓ | 展开，焦点到 Codex | 同左 |
| 再按 Enter | 开出 Codex 会话 | 同左 |
| 按 ↓ 后按 Escape | 收起，焦点回到“新建” | 同左 |
| 按 ↓ 后按 Tab 离开 | 收起，焦点到下一个控件 | 同左 |

- 事件追踪印证了原因：在 main 上点 Codex，依次是按钮上的 pointerdown、mousedown，然后“新建”的 focusout（relatedTarget 是 `MAIN`），菜单收起，最后 mouseup 和 click 落在终端面板上。
- 前端 `node --test` 28 项通过，`make check` 通过。

## 范围与限制

- 没有在打包好的应用里用真实沙箱点一遍：本机没有能操作应用窗口的工具。WKWebView 和应用用的是同一个系统 WebKit、同一份前端代码，后端没有改动。彩排时请在“沙箱详情 → 终端 → 新建”里用鼠标点一次 Claude Code 确认。
- WKWebView 运行器和模拟后端是临时测试工具，没有放进仓库。
- 界面里没有别的“焦点离开就收起”的菜单。
