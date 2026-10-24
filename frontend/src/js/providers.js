// 供应商页：参考 CC Switch，按 Agent 分组管理模型供应商；密钥只写入钥匙串，界面从不读取。
import { useState } from "preact/hooks";
import { html, call, AGENTS, agentName, PROTOCOLS, COMPATIBLE, AUTHS, formatTokens, ConfirmDialog } from "./core.js";

export function ProvidersPage({ providers, presets, summary, reload, notify }) {
  const [agent, setAgent] = useState("codex");
  const [dialog, setDialog] = useState(null); // { mode: "add" | "edit", provider? }
  const list = providers.filter((p) => p.agent === agent);
  return html`
    <section class="page">
      <div class="section-heading">
        <div>
          <h2>模型供应商</h2>
          <p>按智能体管理供应商，在各个沙箱中独立选择。</p>
        </div>
        <button class="primary" onClick=${() => setDialog({ mode: "add" })}>＋ 添加供应商</button>
      </div>
      <div class="tabs" role="tablist">
        ${AGENTS.map(
          (item) => html`<button role="tab" aria-selected=${item.id === agent} class=${item.id === agent ? "tab active" : "tab"}
            onClick=${() => setAgent(item.id)}>${item.name}<span class="count">${providers.filter((p) => p.agent === item.id).length}</span></button>`
        )}
      </div>
      ${list.length === 0
        ? html`<div class="panel empty">
            <span class="empty-icon">◇</span>
            <h3>还没有 ${agentName(agent)} 的供应商</h3>
            <p>从预设里选一个，填入 API Key 即可。</p>
            <button class="primary" onClick=${() => setDialog({ mode: "add" })}>从预设添加</button>
          </div>`
        : html`<div class="card-grid">${list.map(
            (p) => html`<${ProviderCard} key=${p.id} provider=${p} usage=${summary?.providers?.[p.id]} onEdit=${() => setDialog({ mode: "edit", provider: p })}
              reload=${reload} notify=${notify} />`
          )}</div>`}
      ${dialog &&
      html`<${ProviderDialog} agent=${agent} presets=${presets} initial=${dialog.provider}
        onClose=${() => setDialog(null)} onSaved=${async (saved) => {
          setDialog(null);
          setAgent(saved.agent);
          notify(`已保存「${saved.name}」`);
          await reload();
        }} />`}
    </section>`;
}

function ProviderCard({ provider, usage, onEdit, reload, notify }) {
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState(null);
  const test = async () => {
    setTesting(true);
    setResult(null);
    try {
      setResult(await call("TestProvider", provider.id));
    } catch (error) {
      setResult({ ok: false, message: error.message });
    } finally {
      setTesting(false);
    }
  };
  const [confirming, setConfirming] = useState(false);
  const [removing, setRemoving] = useState(false);
  const remove = async () => {
    setRemoving(true);
    try {
      await call("DeleteProvider", provider.id);
      notify(`已删除「${provider.name}」`);
      setConfirming(false);
      await reload();
    } catch (error) {
      setConfirming(false);
      notify(error.message, true);
    } finally {
      setRemoving(false);
    }
  };
  return html`
    <article class="panel provider-card">
      <div class="card-top">
        <h3>${provider.name}</h3>
        ${provider.key_saved
          ? html`<span class="pill ok">密钥已保存</span>`
          : html`<span class="pill warn">缺少密钥</span>`}
      </div>
      <p class="mono">${provider.base_url}</p>
      <dl class="facts">
        <div><dt>模型</dt><dd class="mono">${provider.model}</dd></div>
        <div><dt>协议</dt><dd>${PROTOCOLS[provider.protocol] ?? provider.protocol}</dd></div>
        <div><dt>使用中</dt><dd>${provider.used_by.length ? provider.used_by.join("、") : "暂无沙箱"}</dd></div>
        <div><dt>累计用量</dt><dd title="包括已删除沙箱的用量；只统计经过门卫的模型请求">${usage
          ? `${usage.requests} 次请求 · 输入 ${formatTokens(usage.input_tokens)} / 输出 ${formatTokens(usage.output_tokens)} token`
          : "暂无"}</dd></div>
      </dl>
      ${result &&
      html`<p class=${result.ok ? "test-result ok" : "test-result bad"} role="status">
        ${result.ok ? "✓" : "✕"} ${result.message}${result.latency_ms ? ` · ${result.latency_ms} ms` : ""}
      </p>`}
      <div class="card-actions">
        <button onClick=${test} disabled=${testing}>${testing ? "测试中…" : "测试连接"}</button>
        <button onClick=${onEdit}>编辑</button>
        <button onClick=${async () => { try { await call("DuplicateProvider", provider.id); await reload(); notify("已复制供应商，请编辑名称和模型"); } catch (e) { notify(e.message, true); } }}>复制</button>
        <button class="danger" onClick=${() => setConfirming(true)} disabled=${provider.used_by.length > 0}
          title=${provider.used_by.length ? "仍有沙箱在用，先给它们换一个供应商" : ""}>删除</button>
      </div>
    ${confirming && html`<${ConfirmDialog} title=${`删除供应商「${provider.name}」？`} busy=${removing}
        message=${provider.key_managed ? "钥匙串里保存的密钥也会删除。" : "钥匙串里的条目不是 AIofCage 创建的，会保留。"}
        onConfirm=${remove} onClose=${() => setConfirming(false)} />`}
    </article>`;
}

function ProviderDialog({ agent, presets, initial, onClose, onSaved }) {
  const editing = Boolean(initial);
  const [preset, setPreset] = useState(editing ? presets.find((p) => p.id === initial.preset) ?? null : null);
  const [form, setForm] = useState(() =>
    editing
      ? { ...initial, api_key: "", use_existing: false }
      : null
  );
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const choose = (p) => {
    setPreset(p);
    setForm({
      id: "", name: p.name, agent: p.agent, protocol: p.protocol, base_url: p.base_url, model: p.model,
      auth: p.auth, preset: p.id, api_key: "", use_existing: Boolean(p.existing_key_ref),
    });
  };
  const set = (key) => (event) => setForm({ ...form, [key]: event.target.type === "checkbox" ? event.target.checked : event.target.value });
  const submit = async (event) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      const request = {
        id: form.id ?? "", name: form.name, agent: form.agent, protocol: form.protocol, base_url: form.base_url,
        model: form.model, auth: form.agent === "dsh" ? (form.protocol === "anthropic" ? "x-api-key" : "bearer") : form.auth, preset: form.preset ?? "",
        api_key: form.use_existing ? "" : form.api_key,
        existing_key_ref: form.use_existing && preset?.existing_key_ref ? preset.existing_key_ref : "",
      };
      const saved = await call("SaveProvider", request);
      setForm({ ...form, api_key: "" });
      onSaved(saved);
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  };
  return html`
    <div class="overlay" onClick=${(e) => e.target === e.currentTarget && onClose()}>
      <div class="dialog" role="dialog" aria-modal="true" aria-label=${editing ? "编辑供应商" : "添加供应商"}>
        <div class="dialog-heading">
          <h2>${editing ? "编辑供应商" : form ? `填写 ${agentName(form.agent)} 的供应商信息` : `选择 ${agentName(agent)} 的供应商预设`}</h2>
          <button type="button" class="icon-button" aria-label="关闭" onClick=${onClose}>×</button>
        </div>
        ${!form
          ? html`<div class="preset-grid">
              ${presets.filter((p) => p.agent === agent).map(
                (p) => html`<button class="preset" onClick=${() => choose(p)}>
                  <strong>${p.name}</strong>
                  <span class="mono">${p.base_url || "自己填写接口地址"}</span>
                  ${p.note && html`<small>${p.note}</small>`}
                </button>`
              )}
            </div>
            <p class="caption">预设里的模型名可能会过时，以服务商文档为准，添加时可以修改。</p>`
          : html`<form onSubmit=${submit}>
              <label for="p-name">名称</label>
              <input id="p-name" required maxlength="60" value=${form.name} onInput=${set("name")} />
              <label for="p-url">接口地址（Base URL）</label>
              <input id="p-url" required class="mono" placeholder="https://" value=${form.base_url} onInput=${set("base_url")} />
              <label for="p-model">模型</label>
              <input id="p-model" required class="mono" value=${form.model} onInput=${set("model")} />
              <details class="advanced" open=${editing || !preset || preset.id?.startsWith("custom")}>
                <summary>高级设置（协议、密钥请求头）</summary>
                <p class="caption">选了预设一般不用改。</p>
                <label for="p-protocol">协议</label>
                <select id="p-protocol" value=${form.protocol} onChange=${set("protocol")}>
                  ${COMPATIBLE[form.agent].map((id) => html`<option value=${id}>${PROTOCOLS[id]}</option>`)}
                </select>
                <label for="p-auth">密钥放在哪个请求头</label>
                <select id="p-auth" disabled=${form.agent === "dsh"} value=${form.agent === "dsh" ? (form.protocol === "anthropic" ? "x-api-key" : "bearer") : form.auth} onChange=${set("auth")}>
                  ${Object.entries(AUTHS).map(([id, label]) => html`<option value=${id}>${label}</option>`)}
                </select>
              </details>
              ${preset?.existing_key_ref &&
              html`<label class="check"><input type="checkbox" checked=${form.use_existing} onChange=${set("use_existing")} />
                使用钥匙串中已有的条目 <span class="mono">${preset.existing_key_ref}</span></label>`}
              ${!form.use_existing &&
              html`<label for="p-key">API Key</label>
                <input id="p-key" type="password" autocomplete="off" spellcheck="false" class="mono"
                  required=${!editing} placeholder=${editing ? "留空则保持原密钥不变" : "粘贴 API Key"}
                  value=${form.api_key} onInput=${set("api_key")} />
                <p class="caption">密钥直接写入 macOS 钥匙串，不会保存在应用数据里，也不会再显示出来。</p>`}
              ${error && html`<p class="form-error" role="alert">${error}</p>`}
              <div class="dialog-actions">
                ${!editing && html`<button type="button" onClick=${() => setForm(null)}>返回预设</button>`}
                <button class="primary" type="submit" disabled=${saving}>${saving ? "保存中…" : "保存"}</button>
              </div>
            </form>`}
      </div>
    </div>`;
}
