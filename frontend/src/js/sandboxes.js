// 沙箱页：参考 Docker Desktop 的容器列表。创建、启停与终端在下一步接入。
import { html, agentName, STATES } from "./core.js";

export function SandboxesPage({ sandboxes, environment }) {
  return html`
    <section class="page">
      <div class="section-heading">
        <div>
          <h2>沙箱</h2>
          <p>每个沙箱只能看到自己的项目目录，所有网络请求都经过它自己的门卫。</p>
        </div>
        <button class="primary" disabled title="创建向导即将接入；现在可用 cagectl sandbox create">＋ 创建沙箱</button>
      </div>
      ${sandboxes.length === 0
        ? html`<div class="panel empty">
            <span class="empty-icon">▢</span>
            <h3>还没有沙箱</h3>
            <p>沙箱 = 一个项目目录 + 要用的 Agent + 每个 Agent 的供应商。</p>
          </div>`
        : html`<div class="panel table">
            <div class="row head"><span>名称</span><span>状态</span><span>Agent → 供应商</span><span>累计请求 / token</span></div>
            ${sandboxes.map((s) => {
              const state = STATES[s.state] ?? STATES.unknown;
              return html`<div class="row" key=${s.id}>
                <span><strong>${s.name}</strong><small class="mono">${s.project_path}</small></span>
                <span><span class=${`pill ${state.tone}`}>${state.label}</span></span>
                <span>${s.bindings.map(
                  (b) => html`<span class="binding">${agentName(b.agent)} → ${b.provider_name || "未配置"}</span>`
                )}</span>
                <span class="mono">${s.usage.requests} / ${s.usage.tokens}</span>
              </div>`;
            })}
          </div>`}
      <${EnvironmentPanel} environment=${environment} />
    </section>`;
}

const CHECK_LABELS = { ready: "就绪", detected: "已检测", missing: "缺失", stopped: "未运行", error: "异常", unsupported: "不支持" };

export function EnvironmentPanel({ environment }) {
  if (!environment) return null;
  return html`
    <div class="section-heading compact"><div><h2>运行环境</h2><p>专用虚拟机只共享 ~/CageWorkspace，不影响你自己的 Docker。</p></div></div>
    <div class="panel">
      ${environment.checks.map(
        (check) => html`<div class="check-row" key=${check.id}>
          <div><h3>${check.name}</h3><p>${check.detail}</p></div>
          <span class=${`pill ${check.status === "ready" || check.status === "detected" ? "ok" : check.status === "error" || check.status === "missing" ? "warn" : "idle"}`}>
            ${CHECK_LABELS[check.status] ?? check.status}</span>
        </div>`
      )}
    </div>`;
}
