// MCP 页：令牌、OAuth 授权和数据库密码只保存在钥匙串，沙箱里只有占位符。
// 远程 HTTP 服务由门卫在请求发出时注入令牌（OAuth 令牌由应用在浏览器登录后获取并自动续期）；
// 数据库由沙箱外的可信连接器代持密码，Agent 经门卫访问。
import { useState } from "preact/hooks";
import { html, call, ConfirmDialog } from "./core.js";

export function MCPPage({ services, presets, reload, notify }) {
  const [dialog, setDialog] = useState(null);
  return html`
    <section class="page">
      <div class="section-heading">
        <div>
          <h2>MCP 服务</h2>
          <p>在这里登录 MCP 服务、保存访问令牌或数据库密码，再到沙箱里勾选启用。沙箱里的配置文件只有占位符，真实凭据始终留在沙箱外。</p>
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
      <p class="caption">支持四类：浏览器登录授权的远程 MCP（OAuth，例如 Notion、Linear、Sentry）、用令牌认证的远程 MCP（例如 GitHub 官方 MCP）、${""
        }需要密码的 PostgreSQL 数据库、内置的 filesystem。Codex 与 Claude Code 会自动配置；dsh 暂不支持 MCP。</p>
      ${dialog && html`<${ServiceDialog} presets=${presets} initial=${dialog.service} onClose=${() => { setDialog(null); reload(); }}
        onSaved=${async (saved) => { setDialog(null); notify(`已保存「${saved.name}」`); await reload(); }} />`}
    </section>`;
}

function ServiceCard({ service, onEdit, reload, notify }) {
  const [result, setResult] = useState(null);
  const [testing, setTesting] = useState(false);
  const [authorizing, setAuthorizing] = useState(false);
  const authorize = async () => {
    setAuthorizing(true);
    setResult(null);
    try { await call("AuthorizeMCPService", service.id); notify(`「${service.name}」已授权`); await reload(); }
    catch (e) { setResult({ ok: false, message: e.message }); }
    finally { setAuthorizing(false); }
  };
  const test = async () => {
    setTesting(true);
    setResult(null);
    try { setResult(await call("TestMCPService", service.id)); } catch (e) { setResult({ ok: false, message: e.message }); }
    finally { setTesting(false); }
  };
  const [confirming, setConfirming] = useState(false);
  const [removing, setRemoving] = useState(false);
  const remove = async () => {
    setRemoving(true);
    try { await call("DeleteMCPService", service.id); notify(`已删除「${service.name}」`); setConfirming(false); await reload(); }
    catch (e) { setConfirming(false); notify(e.message, true); }
    finally { setRemoving(false); }
  };
  const database = isDatabase(service);
  const oauth = isOAuth(service);
  const secret = database ? "密码" : "令牌";
  const status = oauth
    ? (service.key_saved ? html`<span class="pill ok">已授权</span>` : html`<span class="pill warn">未登录</span>`)
    : (service.key_saved ? html`<span class="pill ok">${secret}已保存</span>` : html`<span class="pill warn">缺少${secret}</span>`);
  const kind = database ? "数据库 · 可信连接器代持密码 · 只读查询"
    : oauth ? "远程 HTTP · 浏览器登录授权，自动续期 · 门卫注入令牌" : "远程 HTTP · 门卫注入令牌";
  return html`
    <article class="panel provider-card">
      <div class="card-top"><h3>${service.name}</h3>${status}</div>
      <p class="mono">${service.url}</p>
      <dl class="facts">
        <div><dt>类型</dt><dd>${kind}</dd></div>
        <div><dt>名称</dt><dd class="mono">${service.id}</dd></div>
        <div><dt>使用中</dt><dd>${service.used_by.length ? service.used_by.join("、") : "暂无沙箱"}</dd></div>
      </dl>
      ${result && html`<p class=${result.ok ? "test-result ok" : "test-result bad"} role="status">${result.ok ? "✓" : "✕"} ${result.message}</p>`}
      ${authorizing && html`<p class="test-result" role="status">请在打开的浏览器页面里登录并同意授权…</p>`}
      <div class="card-actions">
        ${oauth && html`<button class=${service.key_saved ? "" : "primary"} onClick=${authorize} disabled=${authorizing}>
          ${authorizing ? "等待授权…" : service.key_saved ? "重新登录" : "登录授权"}</button>`}
        <button onClick=${test} disabled=${testing || (oauth && !service.key_saved)}>${testing ? "测试中…" : "测试连接"}</button>
        <button onClick=${onEdit}>编辑</button>
        <button class="danger" onClick=${() => setConfirming(true)} disabled=${service.used_by.length > 0}
          title=${service.used_by.length ? "仍有沙箱在用，先在沙箱里停用它" : ""}>删除</button>
      </div>
      ${confirming && html`<${ConfirmDialog} title=${`删除 MCP 服务「${service.name}」？`} busy=${removing}
        message=${`钥匙串里的${database ? "密码" : oauth ? "授权" : "令牌"}也会删除。`}
        onConfirm=${remove} onClose=${() => setConfirming(false)} />`}
    </article>`;
}

function ServiceDialog({ presets, initial, onClose, onSaved }) {
  const editing = Boolean(initial);
  const [form, setForm] = useState(() => editing ? { ...initial, token: "" } : null);
  const [preset, setPreset] = useState(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState("");
  const set = (key) => (e) => setForm({ ...form, [key]: e.target.value });
  const submit = async (e) => {
    e.preventDefault();
    setSaving("保存中…");
    setError("");
    try {
      let saved = await call("SaveMCPService", { id: form.id ?? "", name: form.name, kind: form.kind ?? "", url: form.url,
        preset: form.preset ?? "", token: form.token });
      setForm({ ...form, id: saved.id, token: "" });
      if (isOAuth(saved) && !saved.key_saved) {
        setSaving("等待浏览器授权…");
        saved = await call("AuthorizeMCPService", saved.id);
      }
      onSaved(saved);
    } catch (err) { setError(err.message); } finally { setSaving(""); }
  };
  return html`
    <div class="overlay" onClick=${(e) => e.target === e.currentTarget && onClose()}>
      <div class="dialog" role="dialog" aria-modal="true" aria-label=${editing ? "编辑 MCP 服务" : "添加 MCP 服务"}>
        <div class="dialog-heading"><h2>${editing ? "编辑 MCP 服务" : form ? "填写 MCP 服务" : "选择 MCP 服务"}</h2>
          <button type="button" class="icon-button" aria-label="关闭" onClick=${onClose}>×</button></div>
        ${!form
          ? html`<div class="preset-grid">${presets.map((p) => html`<button class="preset" onClick=${() => { setPreset(p); setForm({ name: p.name, kind: p.kind ?? "", url: p.url, preset: p.id, token: "" }); }}>
              <strong>${p.name}</strong><span class="mono">${p.url || (p.kind === "postgres" ? "填写连接串" : "自己填写地址")}</span>${p.note && html`<small>${p.note}</small>`}</button>`)}</div>`
          : html`<form onSubmit=${submit}>
              <label for="m-name">名称</label>
              <input id="m-name" required maxlength="60" value=${form.name} onInput=${set("name")} />
              ${isOAuth(form)
                ? html`<label for="m-url">地址（streamable HTTP）</label>
                  <input id="m-url" required class="mono" placeholder="https://" value=${form.url} onInput=${set("url")} />
                  <p class="caption">保存后会打开浏览器，请在服务的官方页面登录并同意授权，不需要复制任何令牌。${""
                    }授权保存在 macOS 钥匙串，由 AiOf Cage 自动续期；沙箱里只有占位符，门卫在请求发出时注入令牌。${""
                    }${preset?.docs ? html` 参考：<span class="mono">${preset.docs}</span>` : ""}</p>
                  ${saving === "等待浏览器授权…" && html`<p class="test-result" role="status">请在打开的浏览器页面里登录并同意授权…</p>`}`
                : isDatabase(form)
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
                <button class="primary" type="submit" disabled=${Boolean(saving)}>
                  ${saving || (isOAuth(form) && !form.key_saved ? "保存并登录" : "保存")}</button>
              </div>
            </form>`}
      </div>
    </div>`;
}

const isOAuth = (service) => service?.kind === "oauth";
const isDatabase = (service) => Boolean(service?.kind) && service.kind !== "http" && !isOAuth(service);

// 沙箱里勾选启用哪些 MCP；创建向导和沙箱行共用。
export function MCPChoices({ services, selected, filesystem, onChange }) {
  const toggle = (id) => onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id], filesystem);
  return html`<div class="agent-pick">
    <label class="check"><input type="checkbox" checked=${filesystem} onChange=${(e) => onChange(selected, e.target.checked)} />
      项目文件（filesystem，内置，无需密钥）</label>
    ${services.map((s) => html`<label class="check"><input type="checkbox" checked=${selected.includes(s.id)} onChange=${() => toggle(s.id)} />
      ${s.name}${isDatabase(s) ? "（数据库，只读）" : ""}${!s.key_saved
        ? html` <span class="pill warn">${isOAuth(s) ? "未登录" : `缺少${isDatabase(s) ? "密码" : "令牌"}`}</span>` : ""}</label>`)}
  </div>`;
}
