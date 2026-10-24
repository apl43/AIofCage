import { useEffect, useState } from "preact/hooks";
import { html, call, Icon, AGENTS } from "./core.js";

export function DemoCases({ providers, reload, notify, onCreated }) {
  const [cases, setCases] = useState([]);
  const [selected, setSelected] = useState(null);
  useEffect(() => { call("ListDemoCases").then(setCases).catch((error) => notify(error.message, true)); }, []);
  return html`<section class="demo-library"><div class="section-heading"><div><h2>案例库</h2><p>创建独立沙箱，通过只读 MCP 分析合成业务数据并生成报告。</p></div><span class="pill idle">合成数据</span></div>
    <div class="card-grid">${cases.map((item) => html`<article class="panel demo-case-card"><div class="card-identity"><span class="card-icon"><${Icon} name=${({ sales: "chart", incident: "activity", release: "shield" })[item.id]} size=${24} /></span><div><small>${item.category}</small><h3>${item.name}</h3></div></div><p>${item.description}</p><button onClick=${() => setSelected(item)}><${Icon} name="plus" size=${18} />创建案例沙箱</button></article>`)}</div>
    ${selected && html`<${CreateDemoDialog} item=${selected} providers=${providers} onClose=${() => setSelected(null)} onCreated=${async (result) => { setSelected(null); await reload(); onCreated(result.id); notify("案例沙箱已创建，启动后即可开始任务"); }} />`}
  </section>`;
}

function CreateDemoDialog({ item, providers, onClose, onCreated }) {
  const [agent, setAgent] = useState(providers[0]?.agent || "dsh");
  const [provider, setProvider] = useState(providers.find((p) => p.agent === agent)?.id || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submit = async (event) => {
    event.preventDefault(); setBusy(true); setError("");
    try { await onCreated(await call("CreateDemoSandbox", { case_id: item.id, agent, provider_id: provider })); }
    catch (error) { setError(error.message); }
    finally { setBusy(false); }
  };
  return html`<div class="overlay"><div class="dialog" role="dialog" aria-modal="true" aria-label=${`创建${item.name}案例`}><div class="dialog-heading"><h2>${item.name}</h2><button class="icon-button" aria-label="关闭" disabled=${busy} onClick=${onClose}><${Icon} name="x" /></button></div>
    <form onSubmit=${submit}><p>新建项目和沙箱，自动连接案例数据与项目文件。创建后由你启动并发送任务。</p>
      <label for="demo-agent">智能体</label><select id="demo-agent" value=${agent} disabled=${busy} onChange=${(event) => { const value = event.target.value; setAgent(value); setProvider(providers.find((p) => p.agent === value)?.id || ""); }}>${AGENTS.map((a) => html`<option value=${a.id}>${a.name}</option>`)}</select>
      <label for="demo-provider">模型供应商</label><select id="demo-provider" value=${provider} disabled=${busy} onChange=${(event) => setProvider(event.target.value)}><option value="">稍后选择</option>${providers.filter((p) => p.agent === agent).map((p) => html`<option value=${p.id}>${p.name} · ${p.model}</option>`)}</select>
      <details class="demo-task-preview"><summary>查看任务</summary><p>${item.task}</p></details><p class="caption">每个案例默认限制 200 次请求、200,000 token。模型费用取决于所选供应商；可在沙箱中调整额度。</p>
      ${error && html`<p class="form-error" role="alert">${error}</p>`}<div class="dialog-actions"><button type="button" disabled=${busy} data-dismiss onClick=${onClose}>取消</button><button class="primary" disabled=${busy}>${busy ? "创建中…" : "创建沙箱"}</button></div>
    </form></div></div>`;
}

export function DemoTask({ caseID, notify }) {
  const [item, setItem] = useState(null);
  useEffect(() => { call("ListDemoCases").then((items) => setItem(items.find((item) => item.id === caseID))).catch((error) => notify(error.message, true)); }, [caseID]);
  if (!item) return null;
  const copy = async () => {
    try {
      if (window.runtime?.ClipboardSetText) await window.runtime.ClipboardSetText(item.task);
      else await navigator.clipboard.writeText(item.task);
      notify("任务已复制，在智能体会话中粘贴并发送");
    } catch { notify("复制失败，可从项目的 TASK.md 复制任务", true); }
  };
  return html`<div class="panel demo-task"><div class="section-heading"><div><h3>${item.name}</h3><span class="pill idle">合成数据 · 只读 MCP</span></div><button onClick=${copy}><${Icon} name="copy" size=${18} />复制任务</button></div><p>${item.task}</p><small>任务保存在项目的 TASK.md，报告写入同一项目。</small></div>`;
}
