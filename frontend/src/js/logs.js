// 日志页：所有沙箱经过门卫的请求，实时滚动；拦截的请求标红。
import { useState } from "preact/hooks";
import { html, call, REASONS, DECISIONS, formatTokens } from "./core.js";
import { ExplainDialog } from "./ai.js";

const FILTERS = [
  { id: "all", label: "全部" },
  { id: "deny", label: "拦截" },
  { id: "inject", label: "注入密钥" },
  { id: "allow", label: "放行" },
  { id: "system", label: "系统" },
];

export function LogsPage({ events, sandboxes, providers, sandboxFilter, setSandboxFilter, notify }) {
  const [filter, setFilter] = useState("all");
  const [explain, setExplain] = useState(null);
  const providerName = (id) => providers.find((p) => p.id === id)?.name ?? id;
  const visible = events.filter((e) => (!sandboxFilter || e.session_id === sandboxFilter) && (
    filter === "all" || (filter === "system" ? e.decision === "not-applicable" : e.decision === filter)
  )).slice().reverse();
  const denied = events.filter((e) => (!sandboxFilter || e.session_id === sandboxFilter) && e.decision === "deny").length;
  return html`
    <section class="page">
      <div class="section-heading">
        <div>
          <h2>请求日志</h2>
          <p>每一条都来自门卫的真实判定。日志只记录主机、路径、结果和用量，不记录请求内容和任何密钥。</p>
        </div>
        <div class="heading-actions">
          ${sandboxFilter && html`<button onClick=${() => call("RevealAuditLog", sandboxFilter).catch((e) => notify(e.message, true))}>
            在访达中显示日志文件</button>`}
          <button class="primary" disabled=${!sandboxFilter} title=${sandboxFilter ? "" : "先选择一个沙箱"}
            onClick=${() => setExplain({ sandboxId: sandboxFilter, sandboxName: sandboxes.find((s) => s.id === sandboxFilter)?.name })}>AI 总结</button>
        </div>
      </div>
      <div class="toolbar">
        <select aria-label="沙箱" value=${sandboxFilter} onChange=${(e) => setSandboxFilter(e.target.value)}>
          <option value="">全部沙箱</option>
          ${sandboxes.map((s) => html`<option value=${s.id}>${s.name}</option>`)}
        </select>
        <div class="segmented" role="tablist">
          ${FILTERS.map((f) => html`<button role="tab" aria-selected=${f.id === filter} class=${f.id === filter ? "active" : ""}
            onClick=${() => setFilter(f.id)}>${f.label}${f.id === "deny" && denied ? html` <span class="count bad">${denied}</span>` : ""}</button>`)}
        </div>
      </div>
      ${visible.length === 0
        ? html`<div class="panel empty"><span class="empty-icon">≡</span><h3>还没有记录</h3>
            <p>启动沙箱后，Agent 发出的每一个网络请求都会实时出现在这里。</p></div>`
        : html`<div class="panel log-table">
            <div class="log-row head"><span>时间</span><span>沙箱</span><span>结果</span><span>说明</span><span>目标</span><span>供应商</span><span>token</span><span>耗时</span><span></span></div>
            ${visible.map((e) => {
              const decision = DECISIONS[e.decision] ?? { label: e.decision, tone: "idle" };
              return html`<div class=${`log-row ${decision.tone}`} key=${`${e.session_id}-${e.id}`}>
                <span class="mono">${new Date(e.ts).toLocaleTimeString("zh-CN", { hour12: false })}</span>
                <span>${e.sandbox_name}</span>
                <span><span class=${`pill ${decision.tone}`}>${decision.label}</span></span>
                <span>${REASONS[e.reason_code] ?? e.reason_code}</span>
                <span class="mono target">${e.action === "mcp.call"
                  ? `MCP ${e.service_id ?? ""} · ${e.tool_id || e.method || ""}`
                  : `${e.method ? `${e.method} ` : ""}${e.host ?? ""}${e.path ?? ""}`}${e.status ? ` · ${e.status}` : ""}</span>
                <span>${e.secret_ref ? providerName(e.secret_ref) : ""}</span>
                <span class="mono">${e.input_tokens || e.output_tokens ? `${formatTokens(e.input_tokens)}/${formatTokens(e.output_tokens)}` : ""}</span>
                <span class="mono">${e.duration_ms ? `${e.duration_ms} ms` : ""}</span>
                <span>${e.decision !== "not-applicable" && html`<button class="link" onClick=${() => setExplain({ sandboxId: e.session_id, eventId: e.id })}>AI 解读</button>`}</span>
              </div>`;
            })}
          </div>`}
      ${explain && html`<${ExplainDialog} request=${explain} onClose=${() => setExplain(null)} />`}
    </section>`;
}
