// 设置页：只读展示运行环境与 Agent 版本。
import { html } from "./core.js";
import { EnvironmentPanel } from "./sandboxes.js";

export function SettingsPage({ overview }) {
  if (!overview) return null;
  return html`
    <section class="page">
      <div class="section-heading"><div><h2>设置</h2><p>这些是固定的安全配置，不需要手动修改。</p></div></div>
      <dl class="panel settings-list">
        <div><dt>工作区（虚拟机唯一可见的目录）</dt><dd class="mono">${overview.workspace}</dd></div>
        <div><dt>应用数据（不含任何密钥）</dt><dd class="mono">${overview.data_dir}</dd></div>
        <div><dt>专用虚拟机</dt><dd class="mono">colima profile aiofcage · 4 CPU / 6 GiB / 40 GiB</dd></div>
        <div><dt>Docker context</dt><dd class="mono">colima-aiofcage（不会改动你的默认 context）</dd></div>
        <div><dt>可安装的 Agent</dt><dd>${overview.agents.map((a) => html`<span class="binding">${a.name} <span class="mono">${a.version}</span></span>`)}</dd></div>
        <div><dt>版本</dt><dd class="mono">${overview.version} · ${overview.stage}</dd></div>
      </dl>
      <${EnvironmentPanel} environment=${overview.environment} />
    </section>`;
}

export function ComingSoon({ title, text }) {
  return html`
    <section class="page">
      <div class="panel empty">
        <span class="empty-icon">◌</span>
        <h3>${title}</h3>
        <p>${text}</p>
      </div>
    </section>`;
}
