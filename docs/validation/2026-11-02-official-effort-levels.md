# 思考强度和官方 CLI 一致

[验收记录索引](README.md)

## 问题

终端会话的“思考强度”只有“模型默认 / 低 / 中 / 高”，三款智能体都一样，后端也只认这三个值。官方 CLI 的档位比这多，而且 Codex 每个模型不一样。

## 官方档位

11-02 在智能体镜像里查锁定的版本（`internal/runtime/images.go`），用的是不联网的临时容器，用完即删。

- **Codex 0.164.0**：档位按模型不同，来自 `codex debug models` 打出的模型目录。

  | 模型 | 档位 | 不指定时 Codex 发 |
  |---|---|---|
  | gpt-6.1-sol、gpt-6-astra、gpt-5.6-sol、gpt-daybreak-blue-latest | low、medium、high、xhigh、max、ultra | low |
  | gpt-6-sol、gpt-5.6-terra、gpt-daybreak-red-latest | 同上 | medium |
  | gpt-6-luna、gpt-5.6-luna、codex-auto-review | low—max（没有 ultra） | medium |
  | gpt-5.5 | low、medium、high、xhigh | medium |

  - **极限（ultra）**：目录里的说明是“最大推理，并自动把任务分派给子智能体”。用本地假接口抓包，Codex 不会把 ultra 原样发出去：gpt-6.1-sol 发 xhigh，gpt-6-sol 发 max，目录外的模型发 medium。
  - **目录外的模型**（比如经 DeepSeek 供应商用 `deepseek-flash`）：不指定时不发思考强度；指定了就原样发。
  - **不校验**：Codex 自己不检查这个值，发 `bogus` 也原样发给接口。
- **Claude Code 2.1.309**：`--effort` 可选 low、medium、high、xhigh、max。
  - 发在 `output_config.effort`，不指定时发 high。
  - 对目录外的模型（`deepseek-flash`）原样发；对 `claude-opus-4-6`，xhigh 会降成 high。
  - 复查另抓了几个模型：`claude-sonnet-4-6` 和 opus 一样；`claude-haiku-4-5` 不带 `output_config`，选哪一档都不起作用，“模型默认（高）”对它也不成立。Claude Code 的档位表不分模型，界面上照列五档。
- **dsh 0.2.1-rc.2**：走 `llm-pi-ai` 适配器，`reasoningEfforts` 可用的键是 off、minimal、low、medium、high、xhigh、max。
  - 经 Anthropic 协议发给 DeepSeek 时，换成思考预算：minimal 1024、low 2048、medium 8192，high、xhigh、max 都是 16384。
  - 不指定时不开思考。

## 供应商实测（比赛 key，七牛 xapi）

直接调 `https://xapi.qiniu.io/v1/responses`，每个模型、每个值发一句话，`max_output_tokens` 为 64。key 只在脚本内存里，不打印、不落盘。另发了一个不存在的值 `bogus` 作对照：返回 400，并列出允许值 `none, minimal, low, medium, high, xhigh, max`，说明接口确实在校验。

| 模型 | 接受 | 拒绝 |
|---|---|---|
| gpt-6.1-sol、gpt-6-astra | low、medium、high、xhigh、max | none、minimal；ultra |
| gpt-6-sol、gpt-5.6-sol、gpt-5.6-terra | none、minimal（回显为 none）、low、medium、high、xhigh、max | ultra |
| gpt-5.5 | 无 | 整个模型被禁用（“model is disabled”） |

- 接口不认 ultra，但 Codex 不会把 ultra 原样发出去（见上），所以在 Codex 里选“极限”能用。
- 不指定时 xapi 默认用 medium；但经 Codex 时，Codex 会先填上自己的默认档，gpt-6.1-sol 实际是 low。
- **没测的**：DeepSeek 的三个供应商（Claude Code、dsh、Codex 的 DeepSeek 供应商）要用用户自己的 DeepSeek key，这次没测 xhigh、max、off、minimal 这些新档位。

## 改动

- `internal/runtime/efforts.go`：档位表 `AgentEfforts`。Codex 按模型，目录外的模型保留低、中、高；Claude Code 和 dsh 各一张。每档还记着 CLI 不指定时用的默认档。
- **会话信息**：`ConversationInfo.efforts` 下发档位表。
- **前端** `frontend/src/js/effort-levels.js`、`conversation-view.js`：
  - 按智能体和当前模型列出档位；
  - 知道 CLI 默认档时，“模型默认”后面写出来，比如“模型默认（低）”；
  - 换模型后原来选的档位不可选，就回到模型默认（按实际生效的模型判断，清空输入框时用供应商模型）；
  - 没有档位表的旧后端仍显示低、中、高。
- **后端校验**：先按全部已知档位做格式检查，再按智能体和实际运行的模型核对，不支持就提示换一档。
  - 继续原生会话时档位已经锁定，不再按档位表核对：换过 CLI 版本后旧档位可能不在表里，又不能改。格式检查照做，所以带不进任意字符串。
  - 本机 AI 控制 `run_task` 的取值范围同步扩大，同样按智能体和模型核对。带备用供应商时，每个备用供应商也按它自己的模型核对（没指定模型时换上去的是它自己的模型），不支持就在开始前报出是哪个。
- **dsh 的“关闭思考”**：pi-ai 要求至少声明一个思考档位，off 自己不能带值。所以写成 off 加 low、选中 off。
  - 走 Anthropic 协议时（现在两个 dsh 供应商都是），抓包确认请求带 `thinking: {type: disabled}`。
  - 走 OpenAI Chat 或 Responses 协议时，off 只是不发思考参数，用的是接口自己的默认值，不保证真的不思考（dsh-llm-pi-ai 的 README 原话）。
  - 原来的写法 `off: off` 会让 dsh 根本不发请求。
- **中文名**：关闭思考、最低、低、中、高、超高、最高、极限（自动分派子任务）。

## 自测

- **单元测试**：
  - Go：档位表与 CLI 一致、按智能体和模型核对、档位表的 JSON 形状、dsh 的 off、xhigh 和模型默认三种写法、本机 AI 控制按备用供应商的模型核对（去掉这处核对，测试会失败）；
  - 前端 `node --test`：按模型取档位、换模型重置、模型名是对象内置名、旧后端退回、中文名。
- **真实 WebKit 加模拟后端**（档位表取自后端的真实输出）：

  | 场景 | 下拉 |
  |---|---|
  | Codex · gpt-6.1-sol | 模型默认（低）/ 低 / 中 / 高 / 超高 / 最高 / 极限（自动分派子任务） |
  | 选“最高”后换成 gpt-5.5 | 模型默认（中）/ 低 / 中 / 高 / 超高，选中回到“模型默认（中）” |
  | 换成 deepseek-flash | 模型默认 / 低 / 中 / 高 |
  | Claude Code | 模型默认（高）/ 低 / 中 / 高 / 超高 / 最高 |
  | dsh | 模型默认 / 关闭思考 / 最低 / 低 / 中 / 高 / 超高 / 最高 |

  选“极限”发消息时，后端收到 `{model: gpt-6.1-sol, effort: ultra}`。截图确认“极限（自动分派子任务）”在会话栏里放得下。
- **真实环境端到端**：`AIOFCAGE_EFFORT_E2E=codex:xhigh,max,ultra go test -run TestConversationEffortEndToEnd ./internal/service/`。
  - 用户已配置的“七牛 xapi”供应商（比赛 key）建一次性 Codex 沙箱，每个档位开一个新会话发一句话。key 由门卫注入，和真实使用同一条链路。
  - gpt-6.1-sol 的超高、最高、极限三档都回复“好”，81 秒通过。
  - 测试结束后沙箱和项目文件夹都已删除，虚拟机里的 Docker 网络数回到 27。
  - 新加的测试和原来的两轮会话测试共用建沙箱、等回复的代码，原测试的行为不变。

## 范围与限制

- 档位表跟着镜像里锁定的 CLI 版本。在“智能体版本”里换了版本，档位可能对不上。
- DeepSeek 的三个供应商没测新档位，见上。
- Claude Code 会按模型降档或忽略思考强度；dsh 的“关闭思考”只在 Anthropic 协议下保证关闭。见“官方档位”和“改动”。

## 独立复查

子代理在临时工作树里只读复查：用不联网的临时容器重新核对了 Codex 的 11 个模型、Claude Code 的 `--effort` 和 dsh 的档位键，都和代码一致；两种协议各 8 档的 dsh 补丁用 dsh 自带的 yaml 包解析都通过；没找到能绕过校验把任意字符串传给 CLI 的路径。指出的问题和处理：

| 级别 | 问题 | 处理 |
|---|---|---|
| 中 | 本机 AI 控制只按主供应商的模型核对，换到备用供应商（比如 deepseek-flash）时会带着它不支持的档位 | 每个备用供应商也核对，加测试 |
| 低 | Claude Code 的“模型默认（高）”和档位对 haiku 等模型不成立 | 记进本文 |
| 低 | dsh 的“关闭思考”在 OpenAI 协议下不保证关闭 | 记进本文 |
| 低 | 手填模型时清空输入框也会把档位重置 | 按实际生效的模型判断 |
| 低 | 继续原生会话时，旧档位不在当前表里会提示“换一档”，可会话又不让改 | 继续原生会话时不再按表核对 |
- 选“极限”会自动分派子任务，用量明显更多。
- dsh 经 Anthropic 协议时，超高、最高和高的思考预算相同。
