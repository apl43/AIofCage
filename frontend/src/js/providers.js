// 供应商页：参考 CC Switch，按 Agent 分组管理模型供应商；密钥只写入钥匙串，界面从不读取。
import { useState } from "preact/hooks";
import { html, call, AGENTS, agentName, PROTOCOLS, COMPATIBLE, AUTHS } from "./core.js";

export function ProvidersPage({ providers, presets, reload, notify }) {
  const [agent, setAgent] = useState("codex");
  const [dialog, setDialog] = useState(null); // { mode: "add" | "edit", provider? }
  const list = providers.filter((p) => p.agent === agent);
  return html`
    <section class="page">
      <div class="section-heading">
        <div>
          <h2>模型供应商</h2>
          <p>API Key 只保存在 macOS 钥匙串；沙箱里只有占位符，由门卫在请求发出时换成真 key。</p>
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
            (p) => html`<${ProviderCard} key=${p.id} provider=${p} onEdit=${() => setDialog({ mode: "edit", provider: p })}
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

function ProviderCard({ provider, onEdit, reload, notify }) {
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
  const remove = async () => {
    if (!confirm(`删除供应商「${provider.name}」？${provider.key_managed ? "钥匙串里保存的密钥也会删除。" : ""}`)) return;
    try {
      await call("DeleteProvider", provider.id);
      notify(`已删除「${provider.name}」`);
      await reload();
    } catch (error) {
      notify(error.message, true);
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
        <div><dt>钥匙串</dt><dd class="mono">${provider.key_ref}</dd></div>
        <div><dt>使用中</dt><dd>${provider.used_by.length ? provider.used_by.join("、") : "暂无沙箱"}</dd></div>
      </dl>
      ${result &&
      html`<p class=${result.ok ? "test-result ok" : "test-result bad"} role="status">
        ${result.ok ? "✓" : "✕"} ${result.message}${result.latency_ms ? ` · ${result.latency_ms} ms` : ""}
      </p>`}
      <div class="card-actions">
        <button onClick=${test} disabled=${testing}>${testing ? "测试中…" : "测试连接"}</button>
        <button onClick=${onEdit}>编辑</button>
        <button class="danger" onClick=${remove} disabled=${provider.used_by.length > 0}
          title=${provider.used_by.length ? "仍有沙箱在用，先给它们换一个供应商" : ""}>删除</button>
      </div>
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
        model: form.model, auth: form.auth, preset: form.preset ?? "",
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
          <h2>${editing ? "编辑供应商" : form ? "填写供应商信息" : `选择 ${agentName(agent)} 的供应商预设`}</h2>
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
              <div class="form-row">
                <div>
                  <label for="p-model">模型</label>
                  <input id="p-model" required class="mono" value=${form.model} onInput=${set("model")} />
                </div>
                <div>
                  <label for="p-protocol">协议</label>
                  <select id="p-protocol" value=${form.protocol} onChange=${set("protocol")}>
                    ${COMPATIBLE[form.agent].map((id) => html`<option value=${id}>${PROTOCOLS[id]}</option>`)}
                  </select>
                </div>
              </div>
              <label for="p-auth">密钥放在哪个请求头</label>
              <select id="p-auth" value=${form.auth} onChange=${set("auth")}>
                ${Object.entries(AUTHS).map(([id, label]) => html`<option value=${id}>${label}</option>`)}
              </select>
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
