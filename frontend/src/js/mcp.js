// MCP 页：令牌和数据库密码只保存在钥匙串，沙箱里只有占位符。
// 远程 HTTP 服务由门卫在请求发出时注入令牌；数据库由沙箱外的可信连接器代持密码，Agent 经门卫访问。
import { useState } from "preact/hooks";
import { html, call } from "./core.js";

export function MCPPage({ services, presets, reload, notify }) {
  const [dialog, setDialog] = useState(null);
  return html`
    <section class="page">
      <div class="section-heading">
        <div>
          <h2>MCP 服务</h2>
          <p>在这里保存 MCP 的访问令牌或数据库密码，再到沙箱里勾选启用。沙箱里的配置文件只有占位符，真实凭据始终留在沙箱外。</p>
        </div>
        <button class="primary" onClick=${() => setDialog({})}>＋ 添加 MCP 服务</button>
      </div>
      <div class="card-grid">
        <article class="panel provider-card builtin">
          <div class="card-top"><h3>项目文件（filesystem）</h3><span class="pill ok">内置</span></div>
          <p>让 Agent 通过 MCP 读写当前项目文件夹。不需要任何密钥，只能访问项目目录，在创建沙箱时勾选即可。</p>
        </article>
        ${services.map((s) => html`<${ServiceCard} key=${s.id} service=${s} onEdit=${() => setDialog({ service: s })} reload=${reload} notify=${notify} />`)}
      </div>
      <p class="caption">支持三类：用 Authorization: Bearer 令牌认证的远程 MCP（例如 GitHub 官方 MCP）、需要密码的 PostgreSQL 数据库、${""
        }内置的 filesystem。Codex 与 Claude Code 会自动配置；dsh 暂不支持 MCP。OAuth 登录类 MCP 在后续版本支持。</p>
      ${dialog && html`<${ServiceDialog} presets=${presets} initial=${dialog.service} onClose=${() => setDialog(null)}
        onSaved=${async (saved) => { setDialog(null); notify(`已保存「${saved.name}」`); await reload(); }} />`}
    </section>`;
}

function ServiceCard({ service, onEdit, reload, notify }) {
  const [result, setResult] = useState(null);
  const [testing, setTesting] = useState(false);
  const test = async () => {
    setTesting(true);
    setResult(null);
    try { setResult(await call("TestMCPService", service.id)); } catch (e) { setResult({ ok: false, message: e.message }); }
    finally { setTesting(false); }
  };
  const remove = async () => {
    if (!confirm(`删除 MCP 服务「${service.name}」？钥匙串里的${isDatabase(service) ? "密码" : "令牌"}也会删除。`)) return;
    try { await call("DeleteMCPService", service.id); notify(`已删除「${service.name}」`); await reload(); }
    catch (e) { notify(e.message, true); }
  };
  const database = isDatabase(service);
  const secret = database ? "密码" : "令牌";
  return html`
    <article class="panel provider-card">
      <div class="card-top"><h3>${service.name}</h3>
        ${service.key_saved ? html`<span class="pill ok">${secret}已保存</span>` : html`<span class="pill warn">缺少${secret}</span>`}</div>
      <p class="mono">${service.url}</p>
      <dl class="facts">
        <div><dt>类型</dt><dd>${database ? "数据库 · 可信连接器代持密码 · 只读查询" : "远程 HTTP · 门卫注入令牌"}</dd></div>
        <div><dt>名称</dt><dd class="mono">${service.id}</dd></div>
        <div><dt>使用中</dt><dd>${service.used_by.length ? service.used_by.join("、") : "暂无沙箱"}</dd></div>
      </dl>
      ${result && html`<p class=${result.ok ? "test-result ok" : "test-result bad"} role="status">${result.ok ? "✓" : "✕"} ${result.message}</p>`}
      <div class="card-actions">
        <button onClick=${test} disabled=${testing}>${testing ? "测试中…" : "测试连接"}</button>
        <button onClick=${onEdit}>编辑</button>
        <button class="danger" onClick=${remove} disabled=${service.used_by.length > 0}>删除</button>
      </div>
    </article>`;
}

function ServiceDialog({ presets, initial, onClose, onSaved }) {
  const editing = Boolean(initial);
  const [form, setForm] = useState(() => editing ? { ...initial, token: "" } : null);
  const [preset, setPreset] = useState(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const set = (key) => (e) => setForm({ ...form, [key]: e.target.value });
  const submit = async (e) => {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      const saved = await call("SaveMCPService", { id: form.id ?? "", name: form.name, kind: form.kind ?? "", url: form.url,
        preset: form.preset ?? "", token: form.token });
      setForm({ ...form, token: "" });
      onSaved(saved);
    } catch (err) { setError(err.message); } finally { setSaving(false); }
  };
  return html`
    <div class="overlay" onClick=${(e) => e.target === e.currentTarget && onClose()}>
      <div class="dialog" role="dialog" aria-modal="true" aria-label=${editing ? "编辑 MCP 服务" : "添加 MCP 服务"}>
        <div class="dialog-heading"><h2>${editing ? "编辑 MCP 服务" : form ? "填写 MCP 服务" : "选择 MCP 服务"}</h2>
          <button type="button" class="icon-button" aria-label="关闭" onClick=${onClose}>×</button></div>
        ${!form
          ? html`<div class="preset-grid">${presets.map((p) => html`<button class="preset" onClick=${() => { setPreset(p); setForm({ name: p.name, kind: p.kind ?? "", url: p.url, preset: p.id, token: "" }); }}>
              <strong>${p.name}</strong><span class="mono">${p.url || (p.kind ? "填写连接串" : "自己填写地址")}</span>${p.note && html`<small>${p.note}</small>`}</button>`)}</div>`
          : html`<form onSubmit=${submit}>
              <label for="m-name">名称</label>
              <input id="m-name" required maxlength="60" value=${form.name} onInput=${set("name")} />
              ${isDatabase(form)
                ? html`<label for="m-url">连接串</label>
                  <input id="m-url" required class="mono" spellcheck="false" placeholder="postgresql://用户名@localhost:5432/数据库名"
                    value=${form.url} onInput=${set("url")} />
                  <label for="m-token">数据库密码</label>
                  <input id="m-token" type="password" autocomplete="off" spellcheck="false" class="mono"
                    placeholder=${editing ? "留空则保持原密码不变" : "连接串里已带密码可留空"} value=${form.token} onInput=${set("token")} />
                  <p class="caption">密码直接写入 macOS 钥匙串（连接串里带的密码也会自动移过去），只交给沙箱外的可信连接器；${""
                    }Agent 只能经门卫调用只读的 query 工具。本机数据库填 localhost 即可，建议使用只读数据库账号。</p>`
                : html`<label for="m-url">地址（streamable HTTP）</label>
                  <input id="m-url" required class="mono" placeholder="https://" value=${form.url} onInput=${set("url")} />
                  <label for="m-token">访问令牌</label>
                  <input id="m-token" type="password" autocomplete="off" spellcheck="false" class="mono" required=${!editing}
                    placeholder=${editing ? "留空则保持原令牌不变" : "粘贴令牌，例如 GitHub 的 PAT"} value=${form.token} onInput=${set("token")} />
                  <p class="caption">令牌直接写入 macOS 钥匙串，沙箱里只会看到占位符。${preset?.docs ? html` 参考：<span class="mono">${preset.docs}</span>` : ""}</p>`}
              ${error && html`<p class="form-error" role="alert">${error}</p>`}
              <div class="dialog-actions">
                ${!editing && html`<button type="button" onClick=${() => setForm(null)}>返回</button>`}
                <button class="primary" type="submit" disabled=${saving}>${saving ? "保存中…" : "保存"}</button>
              </div>
            </form>`}
      </div>
    </div>`;
}

const isDatabase = (service) => Boolean(service?.kind) && service.kind !== "http";

// 沙箱里勾选启用哪些 MCP；创建向导和沙箱行共用。
export function MCPChoices({ services, selected, filesystem, onChange }) {
  const toggle = (id) => onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id], filesystem);
  return html`<div class="agent-pick">
    <label class="check"><input type="checkbox" checked=${filesystem} onChange=${(e) => onChange(selected, e.target.checked)} />
      项目文件（filesystem，内置，无需密钥）</label>
    ${services.map((s) => html`<label class="check"><input type="checkbox" checked=${selected.includes(s.id)} onChange=${() => toggle(s.id)} />
      ${s.name}${isDatabase(s) ? "（数据库，只读）" : ""}${!s.key_saved ? html` <span class="pill warn">缺少${isDatabase(s) ? "密码" : "令牌"}</span>` : ""}</label>`)}
  </div>`;
}
