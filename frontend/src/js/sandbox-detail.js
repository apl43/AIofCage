// 沙箱详情页（参考 Docker Desktop 点开容器）：终端、概览、日志三个标签。
import { useEffect, useRef, useState } from "preact/hooks";
import { html, call, AGENTS, agentName, STATES, ConfirmDialog } from "./core.js";
import { BindingSelects, MCPLine, UsageStats } from "./sandboxes.js";
import { HealthPanel } from "./health.js";
import { UsagePage } from "./usage.js";
import { LogsPage } from "./logs.js";
import { TerminalView, openTerminal, closeTerminal, useTerminals, updateTerminalDraft, addTerminalImages, pasteTerminalImage, handleTerminalPaste, handleTerminalImageKey } from "./terminal.js";
import { IMAGE_ACCEPT, removeImage, sendDraft } from "./terminal-draft.js";

const activeTerminals = new Map();

const TABS = [
  { id: "overview", label: "概览" },
  { id: "agents", label: "智能体" },
  { id: "terminal", label: "终端" },
  { id: "usage", label: "用量" },
  { id: "health", label: "运行状态" },
  { id: "logs", label: "日志" },
];

export function SandboxDetail({ sandbox, detail, setDetail, providers, services, events, usage, vm, busy, progress, run, notify,
  onAddAgents, onEditMCP, onAttack, onDelete, onLogs, dialogs }) {
  const [restartConfirm, setRestartConfirm] = useState(false);
  const tab = detail.tab ?? "overview";
  const state = STATES[sandbox.state] ?? STATES.unknown;
  const running = sandbox.state === "running";
  return html`
    <section class="page detail">
      <button class="link back" onClick=${() => setDetail(null)}>← 全部沙箱</button>
      <div class="detail-heading">
        <div class="sandbox-title">
          <span class=${`status-dot ${state.tone}`} aria-hidden="true"></span>
          <h2>${sandbox.name}</h2>
          <span class=${`pill ${busy ? "warn" : state.tone}`}>${busy ? `${busy}中…` : state.label}</span>
        </div>
        <div class="row-actions">
          ${running
            ? html`<button onClick=${() => run(sandbox, "停止", "StopSandbox", sandbox.id)} disabled=${Boolean(busy)}>停止</button>`
            : html`<button class="primary" onClick=${() => run(sandbox, "启动", "StartSandbox", sandbox.id)} disabled=${Boolean(busy)}>启动</button>`}
          ${sandbox.config_pending && html`<button class="primary" disabled=${Boolean(busy)} onClick=${() => setRestartConfirm(true)}>重启以应用</button>`}
          <details class="action-menu"><summary aria-label="更多沙箱操作">•••</summary><div class="menu-items">
            <button onClick=${onAttack} disabled=${!running || Boolean(busy)}>隔离检查</button>
            <button class="danger" onClick=${onDelete} disabled=${Boolean(busy)}>删除沙箱…</button>
          </div></details>
        </div>
      </div>
      <button class="link mono" onClick=${() => call("OpenProjectFolder", sandbox.id).catch((e) => notify(e.message, true))}
        title="在访达中打开">${sandbox.project_path}</button>
      ${busy && progress && html`<p class="progress" role="status">${progress}</p>`}
      <div class="tabs" role="tablist">
        ${TABS.map((t) => html`<button role="tab" aria-selected=${t.id === tab} class=${t.id === tab ? "tab active" : "tab"}
          onClick=${() => setDetail({ ...detail, tab: t.id, open: undefined })}>${t.label}</button>`)}
      </div>
      ${tab === "terminal" && html`<${TerminalTab} sandbox=${sandbox} request=${detail.open} running=${running} busy=${busy}
        run=${run} notify=${notify} />`}
      ${tab === "agents" && html`<div class="panel detail-overview">
        <div class="section-heading"><div><h3>已安装智能体</h3><p class="caption">为每个智能体独立选择供应商。</p></div>
          <button onClick=${onAddAgents} disabled=${Boolean(busy) || sandbox.bindings.length === AGENTS.length}>＋ 添加智能体</button></div>
        <${BindingSelects} sandbox=${sandbox} providers=${providers} busy=${busy} run=${run} notify=${notify} />
      </div>`}
      ${tab === "overview" && html`<div class="panel detail-overview">
        <div><h3>智能体与供应商</h3><button class="link" onClick=${() => setDetail({ ...detail, tab: "agents" })}>管理智能体</button><p class="caption">选择在下次重启沙箱后应用。</p>
          <${BindingSelects} sandbox=${sandbox} providers=${providers} busy=${busy} run=${run} notify=${notify} /></div>
        <div><h3>MCP 服务</h3><${MCPLine} sandbox=${sandbox} services=${services} busy=${busy} onEditMCP=${onEditMCP} /></div>
        <div><h3>用量</h3><${UsageStats} sandbox=${sandbox} usage=${usage} /></div>
      </div>`}
      ${tab === "health" && html`<${HealthPanel} sandbox=${sandbox} vm=${vm} events=${events} />`}
      ${tab === "usage" && html`<${UsagePage} sandboxId=${sandbox.id} providers=${providers} embedded=${true} />`}
      ${tab === "logs" && html`<${LogsPage} events=${events} sandboxes=${[sandbox]} providers=${providers} notify=${notify}
        sandboxFilter=${sandbox.id} setSandboxFilter=${() => {}} embedded=${true} />`}
      ${restartConfirm && html`<${ConfirmDialog} title="重启并应用配置？" message="当前终端会话将结束，项目文件和智能体历史会保留。" confirmLabel="重启并应用" onClose=${() => setRestartConfirm(false)} onConfirm=${async () => {
        setRestartConfirm(false);
        if (await run(sandbox, "停止", "StopSandbox", sandbox.id)) await run(sandbox, "启动", "StartSandbox", sandbox.id);
      }} />`}
      ${dialogs}
    </section>`;
}

function TerminalTab({ sandbox, request, running, busy, run, notify }) {
  const sessions = useTerminals(sandbox.id);
  const [active, updateActive] = useState(() => activeTerminals.get(sandbox.id));
  const [dragging, setDragging] = useState(false);
  const setActive = (id) => { activeTerminals.set(sandbox.id, id); updateActive(id); };
  const open = async (agent) => setActive(await openTerminal(sandbox, agent));

  // 从沙箱列表点“打开终端”过来时，按选中的 Agent 新开一个标签。
  useEffect(() => {
    if (request && running) open(request.agent);
  }, [request?.nonce]);

  const current = sessions.find((s) => s.id === active) ?? sessions[sessions.length - 1];
  const close = (id) => {
    closeTerminal(id);
    if (id === current?.id) setActive(null);
  };
  const choices = [...sandbox.bindings.map((b) => ({ agent: b.agent, label: agentName(b.agent) })), { agent: "", label: "Shell" }];

  if (!running && sessions.length === 0) {
    return html`<div class="panel empty">
      <span class="empty-icon">›_</span><h3>沙箱还没有启动</h3>
      <p>启动沙箱后，打开智能体或 Shell 开始工作。</p>
      <button class="primary" onClick=${() => run(sandbox, "启动", "StartSandbox", sandbox.id)} disabled=${Boolean(busy)}>启动沙箱</button>
    </div>`;
  }
  return html`<div class=${`terminal-panel ${dragging ? "dragging-image" : ""}`}
    onPasteCapture=${(e) => handleTerminalPaste(current, e)}
    onKeyDownCapture=${(e) => handleTerminalImageKey(current, e)}
    onDragOver=${(e) => { if (e.dataTransfer.types.includes("Files")) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; setDragging(true); } }}
    onDragLeave=${(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setDragging(false); }}
    onDrop=${(e) => { e.preventDefault(); setDragging(false); if (current && !current.exited) addTerminalImages(current, e.dataTransfer.files); }}>
    <div class="terminal-tabs" role="tablist">
      ${sessions.map((s) => html`<div class=${`terminal-tab ${s.id === current?.id ? "active" : ""} ${s.exited ? "exited" : ""}`} key=${s.id}>
        <button role="tab" aria-selected=${s.id === current?.id} onClick=${() => setActive(s.id)}>${s.title}${s.exited ? "（已结束）" : ""}</button>
        <button class="close" aria-label=${`关闭 ${s.title}`} title="关闭" onClick=${() => close(s.id)}>×</button>
      </div>`)}
      <div class="terminal-new">
        ${choices.map((c) => html`<button onClick=${() => open(c.agent)} disabled=${!running} title=${`新开一个 ${c.label} 终端`}>＋ ${c.label}</button>`)}
      </div>
      <button class="link system-terminal" disabled=${!running} title="用 macOS 自带的“终端”打开"
        onClick=${() => call("OpenTerminal", sandbox.id, current?.agent ?? sandbox.bindings[0]?.agent ?? "").catch((e) => notify(e.message, true))}>
        在系统终端中打开</button>
    </div>
    ${current
      ? html`<${TerminalView} key=${current.id} id=${current.id} />
          ${current.exited && running && html`<div class="terminal-exited">
            <button onClick=${() => { const agent = current.agent; close(current.id); open(agent); }}>重新打开 ${current.title}</button></div>`}`
      : html`<div class="panel empty terminal-empty"><h3>开始工作</h3>
          <p>打开智能体，或使用 Shell 管理项目。</p>
          <div class="terminal-choices">${choices.map((c) => html`<button class="primary" onClick=${() => open(c.agent)}>${c.label}</button>`)}</div>
        </div>`}
    ${current && !current.exited && running && html`<${TerminalComposer} key=${current.id} session=${current} />`}
    ${dragging && html`<div class="terminal-drop-hint" aria-hidden="true">松开以添加图片</div>`}
  </div>`;
}

function TerminalComposer({ session }) {
  const picker = useRef(null);
  const draft = session.draft;
  const inputID = `terminal-message-${session.id}`;
  const errorID = `terminal-message-error-${session.id}`;
  const send = () => sendDraft(draft, (text, images) => call("SendTerminalMessage", session.id, text, images), updateTerminalDraft);
  return html`<form class="terminal-composer" onSubmit=${(e) => { e.preventDefault(); send(); }}>
    ${draft.images.length > 0 && html`<ul class="terminal-attachments" aria-label="待发送的图片">
      ${draft.images.map((item, index) => html`<li key=${item.id}>
        <img src=${item.url} alt=${`图片 ${index + 1}：${item.file.name}`} />
        <span title=${item.file.name}>${item.file.name}</span>
        <button type="button" class="attachment-remove" aria-label=${`移除图片 ${index + 1}：${item.file.name}`} disabled=${draft.busy}
          onClick=${() => { removeImage(draft, item.id); updateTerminalDraft(); }}>×</button>
      </li>`)}
    </ul>`}
    <div class="terminal-input-row">
      <label class="sr-only" for=${inputID}>${session.agent ? "输入任务或回复" : "输入 Shell 命令"}</label>
      <textarea id=${inputID} rows="2" value=${draft.text} placeholder=${session.agent ? "输入任务，或粘贴图片…" : "输入 Shell 命令…"}
        disabled=${draft.busy} aria-invalid=${Boolean(draft.error)} aria-describedby=${draft.error ? errorID : undefined}
        onInput=${(e) => { draft.text = e.target.value; draft.error = ""; updateTerminalDraft(); }}
        onKeyDown=${(e) => { if (e.key === "Enter" && !e.shiftKey && !e.isComposing && e.keyCode !== 229) { e.preventDefault(); send(); } }}></textarea>
    </div>
    <div class="terminal-composer-actions">
      <div class="terminal-attachment-actions">
        ${session.agent && html`<input ref=${picker} type="file" accept=${IMAGE_ACCEPT} multiple hidden aria-label="选择图片"
          onChange=${(e) => { addTerminalImages(session, e.target.files); e.target.value = ""; }} />
          <button type="button" disabled=${draft.busy || draft.reading} onClick=${() => picker.current?.click()}>添加图片</button>
          <button type="button" title="粘贴图片（⌘V 或 Ctrl+V）" disabled=${draft.busy || draft.reading} onClick=${() => pasteTerminalImage(session)}>粘贴图片</button>`}
        <span class="terminal-input-hint">${draft.images.length ? `${draft.images.length}/4 张 · 所选模型需支持图片` : "Shift + Enter 换行"}</span>
      </div>
      <button type="submit" class="terminal-send" disabled=${draft.busy || draft.reading}>${draft.busy ? "正在发送…" : "发送 ↵"}</button>
    </div>
    <span class="sr-only" role="status">${draft.status}</span>
    <div class="terminal-input-error" id=${errorID} role="alert">${draft.error}</div>
  </form>`;
}
