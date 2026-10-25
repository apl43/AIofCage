// 设置页：运行资源、智能体版本与应用信息。
import { html, PageTitle } from "./core.js";
import { EnvironmentPanel } from "./sandboxes.js";
import { ExplainerSettings } from "./ai.js";
import { AgentUpdateSettings } from "./agent-versions.js";
import { EnvironmentResources } from "./environment-resources.js";
import { AgentControlSettings } from "./agent-control.js";

export function SettingsPage({ overview, providers, notify, onEnvironmentChanged }) {
  if (!overview) return null;
  return html`
    <section class="page">
      <div class="section-heading"><${PageTitle} icon="settings" title="设置" description="工作区、运行环境与应用信息。" /></div>
      <dl class="panel settings-list">
        <div><dt>工作区（虚拟机唯一可见的目录）</dt><dd class="mono">${overview.workspace}</dd></div>
        <div><dt>应用数据（不含任何密钥）</dt><dd class="mono">${overview.data_dir}</dd></div>
        <div><dt>专用虚拟机</dt><dd class="mono">colima profile aiofcage</dd></div>
        <div><dt>Docker context</dt><dd class="mono">colima-aiofcage（不会改动你的默认 context）</dd></div>
        <${ExplainerSettings} providers=${providers} notify=${notify} />
        <div><dt>版本</dt><dd class="mono">${overview.version}</dd></div>
      </dl>
      <${EnvironmentResources} notify=${notify} onChanged=${onEnvironmentChanged} />
      <${AgentUpdateSettings} notify=${notify} />
      <${AgentControlSettings} notify=${notify} />
      <${EnvironmentPanel} environment=${overview.environment} />
    </section>`;
}
