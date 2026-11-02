// 会话输入框（参考 ChatGPT、Claude 桌面端）：圆角卡片，左下角“+”菜单，右下角模型和思考强度合成的按钮，圆形发送键。
// 菜单里按下鼠标都不移动焦点：macOS 的 WebKit 点按钮时会把焦点交给外层，菜单因此收起，选项就点不中。
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { html, call, Icon, agentName } from "./core.js";
import { IMAGE_ACCEPT, removeImage, sendDraft } from "./terminal-draft.js";
import { addTerminalImages, updateTerminalDraft } from "./terminal.js";
import { effortChoices, effortLabel, effortNote, keepEffort, pickEffort } from "./effort-levels.js";
import { withModel } from "./model-list.js";

const SUGGESTIONS = ["解释这个项目的结构和主要文件", "运行测试，修复失败的用例", "检查代码里有没有安全隐患"];
const keepFocus = (event) => event.preventDefault();

// 菜单打开时，在菜单外按下鼠标或按 Esc 就收起。用鼠标打开时焦点还在输入框里，Esc 不经过菜单，所以在 document 上监听；
// 焦点在菜单里时，菜单自己先处理 Esc 并停止冒泡，这里收不到。
function usePopover() {
  const [open, setOpen] = useState(false);
  const root = useRef(null);
  useEffect(() => {
    if (!open) return;
    const dismiss = (event) => { if (root.current && !root.current.contains(event.target)) setOpen(false); };
    const escape = (event) => { if (event.key === "Escape" && !event.defaultPrevented) { event.preventDefault(); setOpen(false); } };
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("keydown", escape);
    return () => { document.removeEventListener("pointerdown", dismiss); document.removeEventListener("keydown", escape); };
  }, [open]);
  return [open, setOpen, root];
}

// 菜单从输入框向上弹出，终端面板会裁掉超出的部分：按按钮上方剩下的高度限制菜单，模型列表跟着变矮。
function fitAbove(root, selector) {
  const menu = root.current?.querySelector(selector);
  const panel = root.current?.closest(".terminal-panel");
  if (!menu || !panel) return;
  const room = root.current.getBoundingClientRect().top - panel.getBoundingClientRect().top - 12;
  menu.style.maxHeight = `${Math.max(120, Math.floor(room))}px`;
}

// 方向键在当前这一层菜单里移动，Esc 收起并把焦点还给触发按钮。
function moveInMenu(event, root) {
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return false;
  const menu = document.activeElement?.closest?.('[role="menu"]') ?? root.querySelector('[role="menu"]');
  if (!menu || !root.contains(menu)) return false;
  const items = [...menu.querySelectorAll('[role^="menuitem"]:not(:disabled)')].filter((item) => item.closest('[role="menu"]') === menu);
  if (!items.length) return false;
  event.preventDefault();
  const current = items.indexOf(document.activeElement);
  const next = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : current < 0
    ? (event.key === "ArrowUp" ? items.length - 1 : 0)
    : (current + (event.key === "ArrowUp" ? -1 : 1) + items.length) % items.length;
  items[next].focus();
  return true;
}

// 用键盘打开菜单时把焦点放到选中的一项（没有就第一项）；用鼠标打开时不动焦点。
// 等 Preact 把菜单画出来再移焦点；用 setTimeout 而不是 requestAnimationFrame，窗口在后台时也照样执行。
function focusMenu(root, selector) {
  setTimeout(() => {
    const menu = root.current?.querySelector(selector);
    (menu?.querySelector('[aria-checked="true"]:not(:disabled)') ?? menu?.querySelector('[role^="menuitem"]:not(:disabled)'))?.focus();
  }, 0);
}

// 回车和空格会触发按钮的 click（detail 为 0），方向键另外处理；这样按一次键不会先打开又被 click 关上。
const arrowOpens = (event) => event.key === "ArrowDown" || event.key === "ArrowUp";
// 用 Tab 把焦点移出菜单时收起；鼠标点菜单里的项不会移动焦点，不会走到这里。
const leaves = (event) => event.relatedTarget && !event.currentTarget.contains(event.relatedTarget);

function PlusMenu({ session, onShowFiles }) {
  const [open, setOpen, root] = usePopover();
  const picker = useRef(null);
  const draft = session.draft;
  const close = (refocus) => { setOpen(false); if (refocus) root.current?.querySelector(".composer-plus-trigger")?.focus(); };
  const onKeyDown = (event) => {
    if (event.key === "Escape" && open) { event.preventDefault(); event.stopPropagation(); close(true); return; }
    if (open) moveInMenu(event, root.current);
  };
  return html`<div class="composer-plus" ref=${root} onKeyDown=${onKeyDown} onFocusOut=${(event) => { if (open && leaves(event)) setOpen(false); }}>
    <input ref=${picker} type="file" accept=${IMAGE_ACCEPT} multiple hidden aria-label="选择图片"
      onChange=${(event) => { addTerminalImages(session, event.target.files); event.target.value = ""; }} />
    <button type="button" class="composer-plus-trigger" aria-label="添加图片或引用项目文件" aria-haspopup="menu" aria-expanded=${open}
      data-tip="添加" disabled=${draft.busy || draft.reading} onMouseDown=${keepFocus}
      onClick=${(event) => { setOpen(!open); if (!open && event.detail === 0) focusMenu(root, ".composer-plus-menu"); }}
      onKeyDown=${(event) => { if (!open && arrowOpens(event)) { event.preventDefault(); setOpen(true); focusMenu(root, ".composer-plus-menu"); } }}>
      <${Icon} name="plus" size=${18} /></button>
    ${open && html`<div class="composer-popover composer-plus-menu" role="menu" aria-label="添加" onMouseDown=${keepFocus}>
      <button type="button" role="menuitem" onClick=${() => { close(false); picker.current?.click(); }}><${Icon} name="image" size=${17} />添加图片</button>
      <button type="button" role="menuitem" onClick=${() => { close(false); onShowFiles(); }}><${Icon} name="folder" size=${17} />引用项目文件</button>
    </div>`}
  </div>`;
}

// 模型和思考强度合成一个按钮，写成“gpt-6.1-sol Low”；菜单照 Claude：上面供应商和模型列表，底下一行展开思考强度。
// 发出第一条消息后会话锁定，菜单只能看。
function ModelMenu({ session, loadModels }) {
  const [open, setOpen, root] = usePopover();
  const [effortOpen, setEffortOpen] = useState(false);
  // 鼠标从“思考强度”斜着移到左边的档位菜单时会经过模型行：离开后稍等再收起，进了档位菜单就取消。
  const closing = useRef(0);
  const openEffort = () => { clearTimeout(closing.current); setEffortOpen(true); };
  const leaveEffort = () => { clearTimeout(closing.current); closing.current = setTimeout(() => setEffortOpen(false), 300); };
  useEffect(() => { if (!open) { clearTimeout(closing.current); setEffortOpen(false); } }, [open]);
  useEffect(() => () => clearTimeout(closing.current), []);
  const info = session.info;
  const locked = session.started;
  const model = session.model || info?.provider_model || "";
  const choices = effortChoices(info?.efforts, model);
  const level = session.effort || choices.default || "";
  const models = withModel(withModel(session.models, info?.provider_model), model);
  useLayoutEffect(() => { if (open) fitAbove(root, ".composer-model-menu"); }, [open, models.length, session.loadingModels, session.modelsError]);
  useLayoutEffect(() => { if (effortOpen) fitAbove(root, ".composer-effort-menu"); }, [effortOpen]);
  const show = (next) => { setOpen(next); setEffortOpen(false); };
  // 只有用键盘选择时才把焦点还给按钮；用鼠标选时焦点留在输入框，接着按回车照常发送。
  const close = (refocus) => { show(false); if (refocus) root.current?.querySelector(".composer-model-trigger")?.focus(); };
  const chooseModel = (next, keyboard) => {
    if (next !== session.model) {
      session.model = next;
      session.effort = keepEffort(info?.efforts, next, session.effort);
      updateTerminalDraft();
    }
    close(keyboard);
  };
  const chooseEffort = (next, keyboard) => { session.effort = next; updateTerminalDraft(); close(keyboard); };
  const onKeyDown = (event) => {
    if (!open) return;
    if (event.key === "Escape") {
      event.preventDefault(); event.stopPropagation();
      if (effortOpen && root.current?.querySelector(".composer-effort-menu")?.contains(document.activeElement)) {
        setEffortOpen(false); root.current?.querySelector(".composer-effort-row")?.focus();
      } else close(true);
      return;
    }
    if (event.key === "ArrowRight" && event.target.closest?.(".composer-effort-row")) {
      event.preventDefault(); setEffortOpen(true); focusMenu(root, ".composer-effort-menu"); return;
    }
    if (event.key === "ArrowLeft" && event.target.closest?.(".composer-effort-menu")) {
      event.preventDefault(); setEffortOpen(false); root.current?.querySelector(".composer-effort-row")?.focus(); return;
    }
    moveInMenu(event, root.current);
  };
  const levelText = level ? effortLabel(level) : "";
  return html`<div class="composer-model" ref=${root} onKeyDown=${onKeyDown} onFocusOut=${(event) => { if (open && leaves(event)) show(false); }}>
    <button type="button" class="composer-model-trigger" aria-haspopup="menu" aria-expanded=${open} disabled=${!info}
      aria-label=${info ? `模型 ${model}${levelText ? `，思考强度 ${levelText}` : ""}${locked ? "，已锁定" : ""}` : "正在连接"}
      onMouseDown=${keepFocus} onClick=${(event) => { show(!open); if (!open && event.detail === 0) focusMenu(root, ".composer-model-menu"); }}
      onKeyDown=${(event) => { if (!open && arrowOpens(event)) { event.preventDefault(); show(true); focusMenu(root, ".composer-model-menu"); } }}>
      ${locked && html`<${Icon} name="lock" size=${13} />`}
      <span class="composer-model-name">${info ? model : "正在连接…"}</span>
      ${levelText && html`<span class="composer-model-effort">${levelText}</span>`}
      <${Icon} name="chevronDown" size=${14} /></button>
    ${open && html`<div class="composer-popover composer-model-menu" role="menu" aria-label="模型和思考强度" onMouseDown=${keepFocus}>
      <div class="composer-menu-head">
        <span><${Icon} name="layers" size=${15} />${info?.provider_name || agentName(session.agent)}</span>
        <button type="button" class="icon-button" aria-label="获取模型列表" data-tip="获取模型列表" data-tip-align="end" disabled=${locked || session.loadingModels}
          onClick=${() => loadModels(true)}><${Icon} name="refresh" size=${15} /></button>
      </div>
      ${session.loadingModels && html`<p class="composer-menu-note" role="status">正在获取模型列表…</p>`}
      ${session.modelsError && html`<p class="composer-menu-note error" role="status">${session.modelsError}</p>`}
      <div class="composer-model-list" role="group" aria-label="模型">
        ${models.map((name) => html`<button type="button" role="menuitemradio" aria-checked=${name === model} key=${name} disabled=${locked}
          onMouseEnter=${leaveEffort} onClick=${(event) => chooseModel(name, event.detail === 0)}>
          <span class="composer-model-option">${name}</span>${name === model && html`<${Icon} name="check" size=${16} />`}</button>`)}
      </div>
      <div class="composer-menu-divider" role="separator"></div>
      <button type="button" role="menuitem" class="composer-effort-row" aria-haspopup="menu" aria-expanded=${effortOpen}
        onMouseEnter=${openEffort} onClick=${(event) => { openEffort(); if (event.detail === 0) focusMenu(root, ".composer-effort-menu"); }}>
        <span>思考强度</span><span class="composer-menu-value">${levelText || "默认"}<${Icon} name="chevronRight" size=${15} /></span></button>
      ${locked && html`<p class="composer-menu-note"><${Icon} name="lock" size=${13} />要换模型或思考强度，请新建会话</p>`}
      ${effortOpen && html`<div class="composer-popover composer-effort-menu" role="menu" aria-label="思考强度" onMouseEnter=${openEffort}>
        <p class="composer-menu-note">强度越高越周全，但更慢、用量更多</p>
        ${!choices.default && html`<button type="button" role="menuitemradio" aria-checked=${!session.effort} disabled=${locked} onClick=${(event) => chooseEffort("", event.detail === 0)}>
          <span>默认<small>由供应商决定</small></span>${!session.effort && html`<${Icon} name="check" size=${16} />`}</button>`}
        ${choices.levels.map((value) => html`<button type="button" role="menuitemradio" aria-checked=${value === level} key=${value} disabled=${locked}
          onClick=${(event) => chooseEffort(pickEffort(choices, value), event.detail === 0)}>
          <span>${effortLabel(value)}${value === choices.default && html`<span class="composer-badge">默认</span>`}${effortNote(value) && html`<small>${effortNote(value)}</small>`}</span>
          ${value === level && html`<${Icon} name="check" size=${16} />`}</button>`)}
      </div>`}
    </div>`}
  </div>`;
}

export function TerminalComposer({ session, onShowFiles }) {
  const input = useRef(null);
  const draft = session.draft;
  const inputID = `terminal-message-${session.id}`;
  const errorID = `terminal-message-error-${session.id}`;
  const empty = !session.messages.length && !session.started;
  // 打开会话时用供应商页取过的列表；刷新会重新请求，供应商页的列表也随之更新。
  // 自动读取失败时不提示；点刷新才说明原因，取不到时保留原来的列表。
  const loadModels = async (refresh) => {
    session.loadingModels = true; session.modelsError = ""; updateTerminalDraft();
    try {
      const result = await call("ConversationModels", session.id, refresh);
      if (result.ok) session.models = result.models ?? [];
      session.modelsError = result.ok || !refresh ? "" : result.message;
    } catch (error) { if (refresh) session.modelsError = error.message; }
    finally { session.loadingModels = false; updateTerminalDraft(); }
  };
  useEffect(() => {
    if (!session.info || session.started || session.modelsRequested) return;
    session.modelsRequested = true; // 每个会话只自动读一次，之后靠刷新按钮
    loadModels(false);
  }, [session.info]);
  // 输入框随内容变高，最多到 220 像素再出现滚动条。
  useLayoutEffect(() => {
    const field = input.current;
    if (!field) return;
    field.style.height = "auto";
    field.style.height = `${Math.min(field.scrollHeight, 220)}px`;
  }, [draft.text]);
  const send = () => {
    if (session.busy) return;
    return sendDraft(draft, (text, images) => call("SendConversationMessage", session.id, text, images, { model: session.model, effort: session.effort }), updateTerminalDraft);
  };
  const suggest = (text) => {
    draft.text = text; draft.error = ""; updateTerminalDraft();
    requestAnimationFrame(() => { const field = document.getElementById(inputID); field?.focus(); field?.setSelectionRange(text.length, text.length); });
  };
  const ready = Boolean(draft.text.trim() || draft.images.length);
  const unpriced = session.info?.priced && session.model && session.model !== session.info.provider_model;
  return html`<form class="composer" onSubmit=${(e) => { e.preventDefault(); send(); }}>
    ${unpriced && !session.started && html`<p class="composer-notice" role="status">换成「${session.model}」后用量照常统计，但不估算费用：单价只配置了「${session.info.provider_model}」。</p>`}
    <div class="composer-card">
      ${draft.images.length > 0 && html`<ul class="terminal-attachments" aria-label="待发送的图片">
        ${draft.images.map((item, index) => html`<li key=${item.id}>
          <img src=${item.url} alt=${`图片 ${index + 1}：${item.file.name}`} />
          <span title=${item.file.name}>${item.file.name}</span>
          <button type="button" class="attachment-remove" aria-label=${`移除图片 ${index + 1}：${item.file.name}`} disabled=${draft.busy}
            onClick=${() => { removeImage(draft, item.id); updateTerminalDraft(); }}><${Icon} name="x" size=${14} /></button>
        </li>`)}
      </ul>`}
      <label class="sr-only" for=${inputID}>发送给 ${session.title}</label>
      <textarea id=${inputID} ref=${input} data-guide="message-input" rows="1" value=${draft.text} placeholder="描述任务，或粘贴图片…"
        disabled=${draft.busy} aria-invalid=${Boolean(draft.error)} aria-describedby=${draft.error ? errorID : undefined}
        onInput=${(e) => { draft.text = e.target.value; draft.error = ""; updateTerminalDraft(); }}
        onKeyDown=${(e) => { if (e.key === "Enter" && !e.shiftKey && !e.isComposing && e.keyCode !== 229) { e.preventDefault(); send(); } }}></textarea>
      <div class="composer-actions">
        <${PlusMenu} session=${session} onShowFiles=${onShowFiles} />
        ${draft.images.length > 0 && html`<span class="composer-hint">${draft.images.length}/4 张 · 所选模型需支持图片</span>`}
        <span class="composer-spacer"></span>
        <${ModelMenu} session=${session} loadModels=${loadModels} />
        ${session.busy
          ? html`<button type="button" class="composer-send stop" aria-label="停止" data-tip="停止" data-tip-align="end"
              onClick=${() => call("StopConversation", session.id).catch((error) => { draft.error = error.message; updateTerminalDraft(); })}><${Icon} name="stop" size=${14} /></button>`
          : html`<button type="submit" class=${`composer-send ${ready ? "" : "idle"}`} aria-label=${draft.busy ? "正在发送" : "发送"} data-tip="发送" data-tip-align="end"
              disabled=${draft.busy || draft.reading}><${Icon} name="arrowUp" size=${18} /></button>`}
      </div>
    </div>
    <span class="sr-only" role="status">${draft.status}</span>
    <div class="terminal-input-error" id=${errorID} role="alert">${draft.error}</div>
    ${empty && html`<div class="composer-suggestions" aria-label="常用任务">
      ${SUGGESTIONS.map((text) => html`<button type="button" key=${text} onClick=${() => suggest(text)}>${text}</button>`)}
    </div>`}
  </form>`;
}
