import { useEffect, useState } from "preact/hooks";
import { html, call, Icon, agentName, ConfirmDialog } from "./core.js";

export function AgentUpdateSettings({ notify }) {
  const [items, setItems] = useState([]);
  const [busy, setBusy] = useState(false);
  useEffect(() => { call("AgentUpdates").then(setItems).catch((error) => notify(error.message, true)); }, []);
  const check = async () => {
    setBusy(true);
    try { setItems(await call("CheckAgentUpdates")); }
    catch (error) { notify(error.message, true); }
    finally { setBusy(false); }
  };
  const choose = async (item, version) => {
    setBusy(true);
    try { await call("SetAgentDefaultVersion", item.agent, version); setItems(await call("AgentUpdates")); notify("默认版本已保存，已有沙箱保持原版本"); }
    catch (error) { notify(error.message, true); }
    finally { setBusy(false); }
  };
  return html`<section class="panel agent-version-settings">
    <div class="section-heading"><div><h2>智能体版本</h2><p>选择新沙箱的默认版本。已有沙箱在详情中单独更新。</p></div>
      <button onClick=${check} disabled=${busy}><${Icon} name="refresh" size=${18} />${busy ? "正在处理…" : "检查更新"}</button></div>
    <div class="agent-version-list">${items.map((item) => html`<article><div><h3>${item.name}</h3><p>新沙箱默认 <strong class="mono">${item.default_version}</strong></p><small class="mono">${item.package}</small></div>
      <div><span class=${`pill ${item.newer && !item.error ? "ok" : "idle"}`}>${item.error ? "检查失败" : item.latest ? `${item.newer ? "可用更新" : "仓库最新"} ${item.latest}` : "尚未检查"}</span>
        ${item.checked && html`<small>上次成功 ${new Date(item.checked).toLocaleString()}</small>`}${item.error && html`<p class="form-error" role="status">${item.error}</p>`}</div>
      <div class="row-actions">${item.latest && item.latest !== item.default_version && !item.error && html`<button disabled=${busy} onClick=${() => choose(item, item.latest)}>设为新沙箱默认</button>`}
        ${item.default_version !== item.built_in && html`<button disabled=${busy} onClick=${() => choose(item, item.built_in)}>恢复内置 ${item.built_in}</button>`}</div>
    </article>`)}</div>
  </section>`;
}

export function SandboxAgentUpdates({ sandbox, busy, run }) {
  const [target, setTarget] = useState(null);
  const available = sandbox.bindings.filter((binding) => binding.default_version);
  if (!available.length) return null;
  return html`<div class="sandbox-agent-versions"><h3>版本</h3>${available.map((binding) => html`<div class="sandbox-agent-version"><div><strong>${agentName(binding.agent)}</strong>
      <span class="mono">${binding.version || "尚未安装"}${binding.target_version !== binding.version ? ` → ${binding.target_version}（下次启动）` : ""}</span></div>
      ${binding.target_version !== binding.default_version && html`<button disabled=${Boolean(busy)} onClick=${() => setTarget(binding)}>应用默认版本 ${binding.default_version}</button>`}</div>`)}
    ${target && html`<${ConfirmDialog} title=${`更新 ${agentName(target.agent)}？`} message=${`将从 ${target.target_version} 改为 ${target.default_version}。先准备镜像，成功后结束当前会话并替换容器；项目与主目录保留。停止中的沙箱在下次启动时应用。`}
      confirmLabel="更新版本" onClose=${() => setTarget(null)} onConfirm=${async () => { const binding = target; setTarget(null); await run(sandbox, "更新智能体", "UpgradeSandboxAgents", sandbox.id, [binding.agent]); }} />`}
  </div>`;
}
