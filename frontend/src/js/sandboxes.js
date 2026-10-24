// 沙箱页：参考 Docker Desktop 的容器列表——创建、启停、打开终端、切换供应商、删除。
import { useEffect, useState } from "preact/hooks";
import { html, call, AGENTS, agentName, STATES, formatTokens, formatBytes } from "./core.js";

export function SandboxesPage({ sandboxes, providers, overview, summary, reload, notify, onAddProvider, onLogs }) {
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState({}); // id → 正在进行的操作
  const [progress, setProgress] = useState({}); // id → 最近一条进度
  const [deleting, setDeleting] = useState(null);

  useEffect(() => {
    const off = window.runtime?.EventsOn?.("sandbox:progress", (event) =>
      setProgress((current) => ({ ...current, [event.id]: event.message })));
    return () => off?.();
  }, []);

  const run = async (sandbox, label, method, ...args) => {
    setBusy((current) => ({ ...current, [sandbox.id]: label }));
    setProgress((current) => ({ ...current, [sandbox.id]: "" }));
    try {
      await call(method, ...args);
      return true;
    } catch (error) {
      notify(`「${sandbox.name}」${label}失败：${error.message}`, true);
      return false;
    } finally {
      setBusy((current) => ({ ...current, [sandbox.id]: undefined }));
      await reload();
    }
  };

  return html`
    <section class="page">
      <div class="section-heading">
        <div>
          <h2>沙箱</h2>
          <p>每个沙箱只能看到自己的项目目录，所有网络请求都经过它自己的门卫；Agent 拿到的只是占位符。</p>
        </div>
        <button class="primary" onClick=${() => setCreating(true)} disabled=${!overview}>＋ 创建沙箱</button>
      </div>
      ${sandboxes.length === 0
        ? html`<div class="panel empty">
            <span class="empty-icon">▢</span>
            <h3>还没有沙箱</h3>
            <p>沙箱 = 一个项目文件夹 + 要用的 Agent + 每个 Agent 的供应商。</p>
            <button class="primary" onClick=${() => setCreating(true)} disabled=${!overview}>创建第一个沙箱</button>
          </div>`
        : html`<div class="sandbox-list">
            ${sandboxes.map((s) => html`<${SandboxRow} key=${s.id} sandbox=${s} providers=${providers}
              usage=${summary?.sandboxes?.find((u) => u.id === s.id)} onLogs=${() => onLogs(s.id)}
              busy=${busy[s.id]} progress=${progress[s.id]} run=${run} notify=${notify}
              onDelete=${() => setDeleting(s)} />`)}
          </div>`}
      <${EnvironmentPanel} environment=${overview?.environment} />
      ${creating && html`<${CreateSandboxDialog} providers=${providers} overview=${overview} onAddProvider=${onAddProvider}
        onClose=${() => setCreating(false)} onCreated=${async (created, start) => {
          setCreating(false);
          notify(`已创建沙箱「${created.name}」`);
          await reload();
          if (start) await run(created, "启动", "StartSandbox", created.id);
        }} />`}
      ${deleting && html`<${DeleteDialog} sandbox=${deleting} onClose=${() => setDeleting(null)} onConfirm=${async (keepHome) => {
          const target = deleting;
          setDeleting(null);
          if (await run(target, "删除", "DeleteSandbox", target.id, keepHome)) notify(`已删除沙箱「${target.name}」，项目文件夹保留`);
        }} />`}
    </section>`;
}

function SandboxRow({ sandbox, providers, usage, busy, progress, run, notify, onDelete, onLogs }) {
  const state = STATES[sandbox.state] ?? STATES.unknown;
  const running = sandbox.state === "running";
  const maxRequests = usage?.max_requests || sandbox.limits.max_requests;
  const maxTokens = usage?.max_tokens || sandbox.limits.max_tokens;
  const [terminalAgent, setTerminalAgent] = useState(sandbox.bindings[0]?.agent ?? "");
  const openTerminal = async () => {
    try {
      await call("OpenTerminal", sandbox.id, terminalAgent);
    } catch (error) {
      notify(error.message, true);
    }
  };
  const bind = (agent) => async (event) => {
    const providerID = event.target.value;
    if (await run(sandbox, "切换供应商", "BindProvider", sandbox.id, agent, providerID)) {
      notify(running ? `已切换：${agentName(agent)} 下次启动时使用新的供应商` : `已切换 ${agentName(agent)} 的供应商`);
    }
  };
  return html`
    <article class="panel sandbox-row">
      <div class="sandbox-main">
        <div class="sandbox-title">
          <span class=${`status-dot ${state.tone}`} aria-hidden="true"></span>
          <h3>${sandbox.name}</h3>
          <span class=${`pill ${busy ? "warn" : state.tone}`}>${busy ? `${busy}中…` : state.label}</span>
        </div>
        <button class="link mono" onClick=${() => call("OpenProjectFolder", sandbox.id).catch((e) => notify(e.message, true))}
          title="在访达中打开">${sandbox.project_path}</button>
        ${busy && progress && html`<p class="progress" role="status">${progress}</p>`}
      </div>
      <div class="bindings">
        ${sandbox.bindings.map((b) => html`<label class="binding-select">
          <span>${agentName(b.agent)}</span>
          <select value=${b.provider_id} onChange=${bind(b.agent)} disabled=${Boolean(busy)}>
            <option value="">未配置供应商</option>
            ${providers.filter((p) => p.agent === b.agent).map((p) => html`<option value=${p.id}>${p.name}</option>`)}
          </select>
        </label>`)}
      </div>
      <div class="usage">
        <span><strong>${usage?.requests ?? sandbox.usage.requests}</strong> 次请求${maxRequests ? ` / ${maxRequests}` : ""}
          ${quotaPercent(usage?.requests, maxRequests)}</span>
        <span><strong>${formatTokens(usage?.tokens ?? sandbox.usage.tokens)}</strong> token${maxTokens ? ` / ${formatTokens(maxTokens)}` : ""}
          ${quotaPercent(usage?.tokens, maxTokens)}</span>
        ${usage?.stats && html`<span class="mono">CPU ${usage.stats.cpu_percent.toFixed(1)}% · 内存 ${formatBytes(usage.stats.memory_bytes)}</span>`}
        <button class="link" onClick=${onLogs}>${usage?.denied ? html`<span class="count bad">拦截 ${usage.denied}</span> ` : ""}查看日志</button>
      </div>
      <div class="row-actions">
        ${running
          ? html`<button onClick=${() => run(sandbox, "停止", "StopSandbox", sandbox.id)} disabled=${Boolean(busy)}>停止</button>`
          : html`<button class="primary" onClick=${() => run(sandbox, "启动", "StartSandbox", sandbox.id)} disabled=${Boolean(busy)}>启动</button>`}
        <div class="terminal-picker">
          <select aria-label="终端里运行" value=${terminalAgent} onChange=${(e) => setTerminalAgent(e.target.value)} disabled=${!running}>
            ${sandbox.bindings.map((b) => html`<option value=${b.agent}>${agentName(b.agent)}</option>`)}
            <option value="">Shell</option>
          </select>
          <button onClick=${openTerminal} disabled=${!running || Boolean(busy)}>打开终端</button>
        </div>
        <button class="danger" onClick=${onDelete} disabled=${Boolean(busy)}>删除</button>
      </div>
    </article>`;
}

function CreateSandboxDialog({ providers, overview, onClose, onCreated, onAddProvider }) {
  const workspace = overview?.workspace ?? "";
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [mode, setMode] = useState("new");
  const [agents, setAgents] = useState({ codex: true });
  const [bindings, setBindings] = useState(() => {
    const first = (agent) => providers.find((p) => p.agent === agent)?.id ?? "";
    return Object.fromEntries(AGENTS.map((a) => [a.id, first(a.id)]));
  });
  const [maxRequests, setMaxRequests] = useState("");
  const [maxTokens, setMaxTokens] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const inside = path !== "" && (path === workspace || path.startsWith(workspace + "/"));

  const choose = async () => {
    try {
      const picked = await call("SelectProject");
      if (!picked) return;
      setPath(picked);
      const isInside = picked.startsWith(workspace + "/");
      setMode(isInside ? "use" : "copy");
      if (!name) setName(picked.split("/").filter(Boolean).at(-1) ?? "");
    } catch (e) {
      setError(e.message);
    }
  };
  const submit = async (start) => {
    setSaving(true);
    setError("");
    try {
      const selected = AGENTS.filter((a) => agents[a.id]);
      if (selected.length === 0) throw new Error("至少选择一个 Agent");
      const created = await call("CreateSandbox", {
        name, project_path: mode === "new" ? "" : path, project_mode: mode,
        bindings: selected.map((a) => ({ agent: a.id, provider_id: bindings[a.id] })),
        max_requests: Number(maxRequests) || 0, max_tokens: Number(maxTokens) || 0,
      });
      onCreated(created, start);
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  };
  return html`
    <div class="overlay" onClick=${(e) => e.target === e.currentTarget && onClose()}>
      <div class="dialog wide" role="dialog" aria-modal="true" aria-label="创建沙箱">
        <div class="dialog-heading"><h2>创建沙箱</h2>
          <button type="button" class="icon-button" aria-label="关闭" onClick=${onClose}>×</button></div>
        <form onSubmit=${(e) => { e.preventDefault(); submit(true); }}>
          <label for="s-name">名称</label>
          <input id="s-name" required maxlength="60" value=${name} onInput=${(e) => setName(e.target.value)} placeholder="例如：我的网站" />

          <label>项目文件夹</label>
          <div class="choice-group">
            <label class="check"><input type="radio" name="mode" checked=${mode === "new"} onChange=${() => setMode("new")} />
              在工作区新建一个空文件夹</label>
            <label class="check"><input type="radio" name="mode" checked=${mode !== "new"} onChange=${() => setMode(inside ? "use" : "copy")} />
              使用已有文件夹</label>
          </div>
          ${mode !== "new" && html`<div class="input-row">
              <input readonly class="mono" value=${path} placeholder="请选择文件夹" />
              <button type="button" onClick=${choose}>选择…</button>
            </div>
            ${path && !inside && html`<p class="caption">这个文件夹不在工作区里。沙箱只能看到 <span class="mono">~/CageWorkspace</span>，
              所以会先<strong>复制一份</strong>进去，原文件夹不受影响。</p>`}
            ${path && inside && html`<p class="caption">直接使用这个文件夹，Agent 可以修改其中的文件。</p>`}`}

          <label>要安装的 Agent 和供应商</label>
          <div class="agent-pick">
            ${AGENTS.map((a) => {
              const options = providers.filter((p) => p.agent === a.id);
              return html`<div class="agent-line">
                <label class="check"><input type="checkbox" checked=${Boolean(agents[a.id])}
                  onChange=${(e) => setAgents({ ...agents, [a.id]: e.target.checked })} />${a.name}</label>
                ${agents[a.id] && (options.length
                  ? html`<select value=${bindings[a.id]} onChange=${(e) => setBindings({ ...bindings, [a.id]: e.target.value })}>
                      <option value="">暂不配置</option>
                      ${options.map((p) => html`<option value=${p.id}>${p.name}</option>`)}
                    </select>`
                  : html`<button type="button" class="link" onClick=${onAddProvider}>先去添加 ${a.name} 的供应商</button>`)}
              </div>`;
            })}
          </div>

          <details>
            <summary>用量上限（可选）</summary>
            <div class="form-row">
              <div><label for="s-req">最多请求次数</label>
                <input id="s-req" type="number" min="0" placeholder="不限" value=${maxRequests} onInput=${(e) => setMaxRequests(e.target.value)} /></div>
              <div><label for="s-tok">最多 token</label>
                <input id="s-tok" type="number" min="0" placeholder="不限" value=${maxTokens} onInput=${(e) => setMaxTokens(e.target.value)} /></div>
            </div>
            <p class="caption">由沙箱的门卫执行，达到上限后模型请求会被拒绝；到 80% 时会提醒。</p>
          </details>

          ${error && html`<p class="form-error" role="alert">${error}</p>`}
          <div class="dialog-actions">
            <button type="button" onClick=${() => submit(false)} disabled=${saving}>只创建</button>
            <button class="primary" type="submit" disabled=${saving}>${saving ? "处理中…" : "创建并启动"}</button>
          </div>
          <p class="caption">首次启动需要在专用虚拟机里构建镜像，大约 1 分钟。</p>
        </form>
      </div>
    </div>`;
}

function DeleteDialog({ sandbox, onClose, onConfirm }) {
  const [keepHome, setKeepHome] = useState(false);
  return html`
    <div class="overlay" onClick=${(e) => e.target === e.currentTarget && onClose()}>
      <div class="dialog" role="alertdialog" aria-modal="true" aria-label="删除沙箱">
        <div class="dialog-heading"><h2>删除沙箱「${sandbox.name}」？</h2></div>
        <p>会删除它的容器和网络。<strong>项目文件夹不会被删除：</strong><span class="mono">${sandbox.project_path}</span></p>
        <label class="check"><input type="checkbox" checked=${keepHome} onChange=${(e) => setKeepHome(e.target.checked)} />
          保留 Agent 的配置和会话记录（下次用同名沙箱时不会自动恢复）</label>
        <div class="dialog-actions">
          <button onClick=${onClose}>取消</button>
          <button class="danger-solid" onClick=${() => onConfirm(keepHome)}>删除</button>
        </div>
      </div>
    </div>`;
}

const CHECK_LABELS = { ready: "就绪", detected: "已检测", missing: "缺失", stopped: "未运行", error: "异常", unsupported: "不支持" };

export function EnvironmentPanel({ environment }) {
  if (!environment) return null;
  return html`
    <div class="section-heading compact"><div><h2>运行环境</h2><p>专用虚拟机只共享 ~/CageWorkspace，不影响你自己的 Docker。首次启动沙箱时会自动创建或启动它。</p></div></div>
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

function quotaPercent(used, limit) {
  if (!limit || !used) return "";
  const percent = Math.min(100, Math.round((used / limit) * 100));
  return html`<span class=${percent >= 100 ? "quota bad" : percent >= 80 ? "quota warn" : "quota"}>${percent}%</span>`;
}
