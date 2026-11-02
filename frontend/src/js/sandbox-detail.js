// 沙箱详情页（参考 Docker Desktop 点开容器）：终端、概览、日志三个标签。
import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { DevSourceLine } from "./dev-sources.js";
import { html, call, AGENTS, agentName, STATES, ConfirmDialog, Icon, agentIcon } from "./core.js";
import { BindingSelects, MCPLine, UsageStats } from "./sandboxes.js";
import { HealthPanel } from "./health.js";
import { UsagePage } from "./usage.js";
import { LogsPage } from "./logs.js";
import { TerminalView, openTerminal, closeTerminal, useTerminals, recoveryHidden, updateTerminalDraft, addTerminalImages, handleTerminalPaste, handleTerminalImageKey } from "./terminal.js";
import { TerminalComposer } from "./composer.js";
import { SandboxAgentUpdates } from "./agent-versions.js";
import { OperationProgress } from "./progress.js";
import { recoveryPanel } from "./conversation-state.js";
import { DemoTask } from "./demo-cases.js";
import { placeTerminalMenu, terminalMenuKey } from "./terminal-menu.js";
import { ProjectFiles } from "./project-files.js";
import { insertAt, insertable, shellQuote } from "./project-paths.js";

const activeTerminals = new Map();

// 文件栏按沙箱记住展开状态；窗口窄时总是先收起，展开后浮在终端上，不挤占对话区。
const filesKey = (id) => `aiofcage.files.${id}`;
function filesPreference(id) {
  if (window.innerWidth < 1180) return false;
  try { const value = localStorage.getItem(filesKey(id)); if (value) return value === "open"; } catch { /* 无痕或禁用存储 */ }
  return true;
}

const TABS = [
  { id: "overview", label: "概览" },
  { id: "agents", label: "智能体" },
  { id: "terminal", label: "终端" },
  { id: "usage", label: "用量" },
  { id: "health", label: "运行状态" },
  { id: "logs", label: "日志" },
];

export function SandboxDetail({ sandbox, detail, setDetail, providers, services, recoverable, events, usage, vm, busy, run, notify,
  onAddAgents, onEditMCP, onAttack, onReset, onDelete, onLogs, dialogs, devSources, onEditDev }) {
  const [restartConfirm, setRestartConfirm] = useState(false);
  const tab = detail.tab ?? "overview";
  const state = STATES[sandbox.state] ?? STATES.unknown;
  const running = sandbox.state === "running";
  const openFolder = () => call("OpenProjectFolder", sandbox.id).catch((e) => notify(e.message, true));
  // 标签和启停按钮排在名字右边的同一行（参考 Claude 桌面端顶部的分段按钮），省出的高度留给终端。
  return html`
    <section class="page detail">
      <button class="link back" onClick=${() => setDetail(null)}><${Icon} name="chevronLeft" size=${16} />全部沙箱</button>
      <div class="detail-heading">
        <div class="sandbox-title" data-guide="sandbox-status">
          <span class=${`status-dot ${state.tone}`} aria-hidden="true"></span>
          <h1>${sandbox.name}</h1>
          <span class=${`pill ${busy ? "warn" : state.tone}`}>${busy ? `${busy}中…` : state.label}</span>
        </div>
        <div class="detail-actions">
          <div class="detail-tabs" role="tablist" aria-label="沙箱详情">
            ${TABS.map((t) => html`<button role="tab" aria-selected=${t.id === tab} tabindex=${t.id === tab ? 0 : -1} class=${t.id === tab ? "segment active" : "segment"}
              onClick=${() => setDetail({ ...detail, tab: t.id, open: undefined })}>${t.label}</button>`)}
          </div>
          <div class="row-actions">
            ${running
              ? html`<button onClick=${() => run(sandbox, "停止", "StopSandbox", sandbox.id)} disabled=${Boolean(busy)}><${Icon} name="stop" size=${16} />停止</button>`
              : html`<button class="primary" data-guide="sandbox-start" onClick=${() => run(sandbox, "启动", "StartSandbox", sandbox.id)} disabled=${Boolean(busy)}><${Icon} name="play" size=${16} />启动</button>`}
            ${sandbox.config_pending && html`<button class="primary" disabled=${Boolean(busy)} onClick=${() => setRestartConfirm(true)}>重启以应用</button>`}
            <details class="action-menu"><summary aria-label="更多沙箱操作"><${Icon} name="more" /></summary><div class="menu-items">
              <button onClick=${openFolder}>在访达中显示项目</button>
              <button onClick=${onAttack} disabled=${!running || Boolean(busy)}>隔离检查</button>
              <button onClick=${onReset} disabled=${Boolean(busy)}>重置运行环境…</button>
              <button class="danger" onClick=${onDelete} disabled=${Boolean(busy)}>删除沙箱…</button>
            </div></details>
          </div>
        </div>
      </div>
      <${OperationProgress} sandboxId=${sandbox.id} activity=${sandbox.activity} notify=${notify} />
      ${sandbox.demo_case && tab === "overview" && html`<${DemoTask} caseID=${sandbox.demo_case} notify=${notify} />`}
      ${tab === "terminal" && html`<${TerminalTab} key=${sandbox.id} sandbox=${sandbox} request=${detail.open} recoverable=${recoverable} running=${running} busy=${busy}
        run=${run} notify=${notify} />`}
      ${tab === "agents" && html`<div class="panel detail-overview">
        <div class="section-heading"><div><h3>已安装智能体</h3><p class="caption">为每个智能体独立选择供应商。</p></div>
          <button onClick=${onAddAgents} disabled=${Boolean(busy) || sandbox.bindings.length === AGENTS.length}><${Icon} name="plus" size=${18} />添加智能体</button></div>
        <${BindingSelects} sandbox=${sandbox} providers=${providers} busy=${busy} run=${run} notify=${notify} />
        <${SandboxAgentUpdates} sandbox=${sandbox} busy=${busy} run=${run} />
      </div>`}
      ${tab === "overview" && html`<div class="panel detail-overview">
        <div><h3>项目文件夹</h3><p class="mono project-folder">${sandbox.project_path}</p>
          <button class="link" onClick=${openFolder}>在访达中显示</button></div>
        <div><h3>智能体与供应商</h3><button class="link" onClick=${() => setDetail({ ...detail, tab: "agents" })}>管理智能体</button><p class="caption">选择在下次重启沙箱后应用。</p>
          <${BindingSelects} sandbox=${sandbox} providers=${providers} busy=${busy} run=${run} notify=${notify} /></div>
        <div><h3>MCP 服务</h3><${MCPLine} sandbox=${sandbox} services=${services} busy=${busy} onEditMCP=${onEditMCP} /></div>
        <div><h3>开发环境</h3><${DevSourceLine} sandbox=${sandbox} sources=${devSources ?? []} busy=${busy} onEdit=${onEditDev} /></div>
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

function TerminalTab({ sandbox, request, recoverable = [], running, busy, run, notify }) {
  const sessions = useTerminals(sandbox.id);
  const [active, updateActive] = useState(() => activeTerminals.get(sandbox.id));
  const [dragging, setDragging] = useState(false);
  const [creating, setCreating] = useState(false);
  const [filesOpen, setFilesOpen] = useState(() => filesPreference(sandbox.id));
  const showFiles = (value) => {
    setFilesOpen(value);
    try { localStorage.setItem(filesKey(sandbox.id), value ? "open" : "closed"); } catch { /* 只影响下次是否默认展开 */ }
  };
  const closeFiles = useCallback(() => showFiles(false), []);
  const menu = useRef(null);
  // macOS 的 WebKit 点按钮时不把焦点给按钮，而是给外层可聚焦的元素（这里是 <main>，没有就直接失去焦点）；
  // 菜单的 focusout 会先收起菜单，选项的 click 就落空了。所以在选项上按下鼠标时不移动焦点，方向键选择不受影响。
  const keepMenuFocus = (event) => event.preventDefault();
  useEffect(() => {
    const dismiss = (event) => { if (menu.current && !menu.current.contains(event.target)) menu.current.open = false; };
    const reposition = () => placeTerminalMenu(menu.current);
    const observer = new ResizeObserver(reposition);
    if (menu.current) observer.observe(menu.current.closest(".terminal-panel"));
    document.addEventListener("pointerdown", dismiss);
    window.addEventListener("resize", reposition);
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      window.removeEventListener("resize", reposition);
      observer.disconnect();
    };
  }, []);
  const setActive = (id) => { activeTerminals.set(sandbox.id, id); updateActive(id); };
  const open = async (agent) => {
    if (creating) return;
    setCreating(true);
    if (menu.current?.open) { menu.current.open = false; menu.current.querySelector("summary").focus(); }
    try { setActive(await openTerminal(sandbox, agent)); }
    finally { setCreating(false); }
  };
  const recovering = useRef(false); // 同步加锁：第一次调用返回之前，重复点击不会再恢复一次
  const recover = async (item) => {
    if (creating || recovering.current || !item.ready) return;
    recovering.current = true;
    setCreating(true);
    try {
      // 先确认记录还在、还能恢复，再决定要不要启动沙箱。
      const current = (await call("ListRecoverableConversations").catch(() => [])).find((record) => record.id === item.id);
      if (!current?.ready) {
        notify(current ? `这条会话不能恢复：${current.reason}` : "这条会话记录已经不在了", true);
        return;
      }
      if (!running && !await run(sandbox, "启动", "StartSandbox", sandbox.id)) return;
      setActive(await openTerminal(sandbox, item.agent, item));
    } finally { recovering.current = false; setCreating(false); }
  };
  // 面板只列出真能恢复的会话；不能恢复的旧记录只作为诊断信息，不出现“恢复”字样。
  const { ready, stale } = recoveryPanel(recoverable, sandbox.id, recoveryHidden(sandbox.id));
  const recoveries = html`${ready.length > 0 && html`<div class="conversation-recovery panel" role="status">
      <div><strong>可恢复会话</strong><p class="caption">这些记录只保存原生会话元数据，不保存消息正文。${running ? "" : "恢复时会先启动沙箱。"}</p></div>
      ${ready.map((item) => html`<div class="conversation-recovery-row" key=${item.id}>
        <span><${Icon} name="sparkles" size=${17} />${agentName(item.agent)} · ${item.provider_name} · ${item.model}</span>
        <button class="primary" onClick=${() => recover(item)} disabled=${Boolean(busy) || creating}>恢复</button>
      </div>`)}</div>`}
    ${stale.length > 0 && html`<details class="caption recovery-diagnostics"><summary>${stale.length} 条旧会话记录不能继续</summary>
      <ul>${stale.map((item) => html`<li key=${item.id}>${agentName(item.agent)} · ${item.model}：${item.reason}</li>`)}</ul></details>`}`;

  // 从沙箱列表点“打开终端”过来时，按选中的 Agent 新开一个标签。
  useEffect(() => {
    if (request && running) {
      const existing = request.reuse && sessions.findLast((s) => !s.exited && s.agent === request.agent && s.info?.provider_id === sandbox.bindings.find((b) => b.agent === request.agent)?.active_provider_id);
      if (existing) setActive(existing.id);
      else open(request.agent);
    }
  }, [request?.nonce]);

  const current = sessions.find((s) => s.id === active) ?? sessions[sessions.length - 1];
  const close = (id) => {
    closeTerminal(id);
    if (id === current?.id) setActive(null);
  };
  const choices = [...sandbox.bindings.map((b) => ({ agent: b.agent, label: agentName(b.agent) })), { agent: "", label: "Shell" }];
  // 路径插入当前会话：对话插到输入框光标处（项目内的相对路径），Shell 当作键入、不带回车
  // （绝对路径，cd 到别处也对；容器里项目的路径和宿主相同）。
  const conversation = current?.kind === "conversation";
  const canInsert = Boolean(current && !current.exited && (!conversation || (current.info && running && !current.draft.busy)));
  const insertPath = (path) => {
    if (!canInsert || !insertable(path)) return;
    if (!conversation) {
      call("WriteTerminal", current.id, shellQuote(`${sandbox.project_path}/${path}`) + " ").catch((error) => notify(error.message, true));
      current.term?.focus();
      return;
    }
    const inputID = `terminal-message-${current.id}`;
    const input = document.getElementById(inputID);
    const next = insertAt(current.draft.text, path, input?.selectionStart ?? current.draft.text.length, input?.selectionEnd ?? current.draft.text.length);
    current.draft.text = next.text;
    current.draft.error = "";
    updateTerminalDraft();
    requestAnimationFrame(() => { const field = document.getElementById(inputID); field?.focus(); field?.setSelectionRange(next.cursor, next.cursor); });
  };
  // 回调保持不变，文件栏才不会随对话的流式输出整棵重绘。
  const insertRef = useRef(insertPath);
  insertRef.current = insertPath;
  const onInsert = useCallback((path) => insertRef.current(path), []);
  // “+”菜单里的“引用项目文件”：展开文件栏，等列表读出来后把焦点放到第一项，选好后点“插入”。
  const revealFiles = () => {
    showFiles(true);
    let tries = 0;
    const focusFirst = () => {
      const entry = document.querySelector(".project-files .project-entry");
      if (entry) entry.focus();
      else if (++tries < 40) setTimeout(focusFirst, 50);
    };
    setTimeout(focusFirst, 0);
  };
  // 新会话还没有消息时，欢迎语和输入框一起放在中间（参考 ChatGPT、Claude 的新对话页）。
  const emptyChat = conversation && current.info && !current.exited && running && !current.messages.length && !current.started && !current.error;

  if (!running && sessions.length === 0) {
    return html`<div>${recoveries}<div class="panel empty">
      <span class="empty-icon"><${Icon} name="terminal" size=${40} /></span><h3>沙箱还没有启动</h3>
      <p>启动沙箱后，打开智能体或 Shell 开始工作。</p>
      <button class="primary" onClick=${() => run(sandbox, "启动", "StartSandbox", sandbox.id)} disabled=${Boolean(busy)}>启动沙箱</button>
    </div></div>`;
  }
  // 拖进来的文件只有落在左侧终端区才当图片；落在别处也要拦住，否则窗口会跳去显示那个文件。
  return html`<div class="terminal-panel"
    onDragOver=${(e) => { if (!e.defaultPrevented && e.dataTransfer.types.includes("Files")) { e.preventDefault(); e.dataTransfer.dropEffect = "none"; } }}
    onDrop=${(e) => e.preventDefault()}>
    ${recoveries}
    <div class="terminal-tabs">
      ${sessions.length > 0 && html`<div class="terminal-session-tabs" role="tablist" aria-label="终端会话">${sessions.map((s) => html`<div class=${`terminal-tab ${s.id === current?.id ? "active" : ""} ${s.exited ? "exited" : ""}`} key=${s.id}>
        <button role="tab" aria-selected=${s.id === current?.id} tabindex=${s.id === current?.id ? 0 : -1} onClick=${() => setActive(s.id)}><${Icon} name=${agentIcon(s.agent)} size=${15} />${s.title}${s.exited ? "（已结束）" : ""}</button>
        <button class="close" aria-label=${`关闭 ${s.title}`} onClick=${() => close(s.id)}><${Icon} name="x" size=${14} /></button>
      </div>`)}</div>`}
      <div class="terminal-new">
        <details class="terminal-new-menu" ref=${menu} onKeyDown=${terminalMenuKey} onToggle=${(event) => placeTerminalMenu(event.currentTarget)}
          onFocusOut=${(event) => { if (!event.currentTarget.contains(event.relatedTarget)) event.currentTarget.open = false; }}>
          <summary aria-label="新建会话" data-tip="新建会话" data-guide="new-conversation"><${Icon} name="plus" size=${17} /></summary>
          <div class="terminal-menu-choices" onMouseDown=${keepMenuFocus}>${choices.map((choice) => html`<button onClick=${() => open(choice.agent)} disabled=${!running || creating}><${Icon} name=${agentIcon(choice.agent)} size=${18} />${choice.label}</button>`)}</div>
        </details>
      </div>
      <div class="terminal-tools">
        <button class="icon-button" aria-label="在系统终端中打开" data-tip="在系统终端中打开" data-tip-align="end" disabled=${!running}
          onClick=${() => call("OpenTerminal", sandbox.id, current?.agent ?? sandbox.bindings[0]?.agent ?? "").catch((e) => notify(e.message, true))}>
          <${Icon} name="terminal" size=${17} /></button>
        <button class=${`icon-button files-toggle ${filesOpen ? "active" : ""}`} aria-pressed=${filesOpen} aria-label="项目文件"
          data-tip=${filesOpen ? "收起项目文件" : "展开项目文件"} data-tip-align="end" onClick=${() => showFiles(!filesOpen)}>
          <${Icon} name="panelRight" size=${17} /></button>
      </div>
    </div>
    <div class="terminal-body">
    <div class=${`terminal-main ${dragging ? "dragging-image" : ""} ${emptyChat ? "chat-empty" : ""}`}
      onPasteCapture=${(e) => handleTerminalPaste(current, e)}
      onKeyDownCapture=${(e) => handleTerminalImageKey(current, e)}
      onDragOver=${(e) => { if (e.dataTransfer.types.includes("Files")) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; setDragging(true); } }}
      onDragLeave=${(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setDragging(false); }}
      onDrop=${(e) => { e.preventDefault(); setDragging(false); if (current?.agent && !current.exited) addTerminalImages(current, e.dataTransfer.files); }}>
    ${current
      ? html`<${TerminalView} key=${current.id} id=${current.id} />
          ${current.exited && running && html`<div class="terminal-exited">
            <button onClick=${() => { const agent = current.agent; close(current.id); open(agent); }}>重新打开 ${current.title}</button></div>`}`
      : html`<div class="panel empty terminal-empty"><h3>开始工作</h3>
          <p>打开智能体，或使用 Shell 管理项目。</p>
          <div class="terminal-choices">${choices.map((c) => html`<button class="primary" onClick=${() => open(c.agent)}><${Icon} name=${agentIcon(c.agent)} size=${18} />${c.label}</button>`)}</div>
        </div>`}
    ${current?.agent && current.info && !current.exited && running && html`<${TerminalComposer} key=${current.id} session=${current} onShowFiles=${revealFiles} />`}
    ${dragging && html`<div class="terminal-drop-hint" aria-hidden="true">松开以添加图片</div>`}
    </div>
    ${filesOpen && html`<${ProjectFiles} sandboxId=${sandbox.id} busy=${sessions.filter((s) => s.busy).length} canInsert=${canInsert}
      onInsert=${onInsert} onClose=${closeFiles} notify=${notify} />`}
    </div>
  </div>`;
}
