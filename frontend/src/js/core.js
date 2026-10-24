// 公共工具：Preact/htm、后端调用与中文标签。
import { h } from "preact";
import htm from "htm";

export const html = htm.bind(h);

// 调用 Wails 绑定的 Go 方法；错误统一转成可直接展示的中文字符串。
export async function call(method, ...args) {
  const app = window.go?.service?.App;
  if (!app) throw new Error("桌面后端尚未连接，请用 make desktop 构建后启动应用。");
  try {
    return await app[method](...args);
  } catch (error) {
    throw new Error(error instanceof Error ? error.message : String(error));
  }
}

export const AGENTS = [
  { id: "codex", name: "Codex" },
  { id: "claude-code", name: "Claude Code" },
  { id: "dsh", name: "dsh" },
];

export const agentName = (id) => AGENTS.find((agent) => agent.id === id)?.name ?? id;

export const PROTOCOLS = {
  "openai-responses": "OpenAI Responses",
  "openai-chat": "OpenAI Chat Completions",
  anthropic: "Anthropic Messages",
};

// 与后端 provider.Compatible 保持一致。
export const COMPATIBLE = {
  codex: ["openai-responses"],
  "claude-code": ["anthropic"],
  dsh: ["openai-chat", "anthropic"],
};

export const AUTHS = { bearer: "Authorization: Bearer", "x-api-key": "x-api-key 请求头" };

export const STATES = {
  running: { label: "运行中", tone: "ok" },
  stopped: { label: "已停止", tone: "idle" },
  not_created: { label: "未启动过", tone: "idle" },
  partial: { label: "状态异常", tone: "warn" },
  unknown: { label: "未知", tone: "idle" },
};
