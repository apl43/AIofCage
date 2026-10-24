// 内置终端：xterm.js 显示，后端用 PTY 运行 docker exec。
// 会话保存在模块里，切换页面时终端继续运行，回来时把它的 DOM 挂回原处，输出不会丢。
import { useEffect, useRef, useState } from "preact/hooks";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-fit";
import { html, call, agentName } from "./core.js";

const sessions = new Map(); // id → { id, sandboxId, agent, title, term, fit, element, exited, code }
const listeners = new Set();
let wired = false;
let holder = null;

const notifyChange = () => listeners.forEach((fn) => fn());

// 终端在不显示时停放在屏幕外的容器里（不能 display:none，否则 xterm 无法测量字号）。
function parking() {
  if (!holder) {
    holder = document.createElement("div");
    holder.style.cssText = "position:fixed;left:-10000px;top:0;width:900px;height:500px;visibility:hidden";
    document.body.appendChild(holder);
  }
  return holder;
}

function decode(base64) {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

function wire() {
  if (wired) return;
  wired = true;
  window.runtime?.EventsOn?.("terminal:data", ({ id, data }) => sessions.get(id)?.term.write(decode(data)));
  window.runtime?.EventsOn?.("terminal:exit", ({ id, code }) => {
    const s = sessions.get(id);
    if (!s) return;
    s.exited = true;
    s.code = code;
    s.term.write(`\r\n\x1b[90m[进程已结束${code ? `，退出码 ${code}` : ""}。关闭这个标签，或点“重新打开”]\x1b[0m\r\n`);
    notifyChange();
  });
}

const THEME = {
  background: "#14231e", foreground: "#e3ece6", cursor: "#b9dcc8", cursorAccent: "#14231e",
  selectionBackground: "#3d5c4f", black: "#1e2b26", brightBlack: "#5f7068",
};

const newID = () => "t-" + Array.from(crypto.getRandomValues(new Uint8Array(8)), (b) => (b % 36).toString(36)).join("");

export async function openTerminal(sandbox, agent) {
  wire();
  const id = newID();
  const term = new Terminal({
    fontFamily: '"SF Mono", Menlo, ui-monospace, monospace', fontSize: 13, lineHeight: 1.15,
    cursorBlink: true, scrollback: 5000, theme: THEME, allowProposedApi: false,
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  const element = document.createElement("div");
  element.className = "terminal-host";
  parking().appendChild(element);
  term.open(element);
  try { fit.fit(); } catch { /* 尺寸会在显示时重新计算 */ }
  const session = { id, sandboxId: sandbox.id, agent, title: agent ? agentName(agent) : "Shell", term, fit, element, exited: false };
  sessions.set(id, session);
  term.onData((data) => { if (!session.exited) call("WriteTerminal", id, data).catch(() => {}); });
  term.onResize(({ cols, rows }) => { if (!session.exited) call("ResizeTerminal", id, cols, rows).catch(() => {}); });
  notifyChange();
  try {
    await call("StartTerminal", id, sandbox.id, agent, term.cols, term.rows);
  } catch (error) {
    session.exited = true;
    term.write(`\x1b[31m无法打开终端：${error.message}\x1b[0m\r\n`);
    notifyChange();
  }
  return id;
}

export function closeTerminal(id) {
  const s = sessions.get(id);
  if (!s) return;
  if (!s.exited) call("CloseTerminal", id).catch(() => {});
  s.term.dispose();
  s.element.remove();
  sessions.delete(id);
  notifyChange();
}

export function sandboxTerminals(sandboxId) {
  return [...sessions.values()].filter((s) => s.sandboxId === sandboxId);
}

export function runningTerminalCount(sandboxId) {
  return sandboxTerminals(sandboxId).filter((s) => !s.exited).length;
}

// 订阅会话列表的变化（新开、关闭、进程结束）。
export function useTerminals(sandboxId) {
  const [, setVersion] = useState(0);
  useEffect(() => {
    const fn = () => setVersion((v) => v + 1);
    listeners.add(fn);
    return () => listeners.delete(fn);
  }, []);
  return sandboxTerminals(sandboxId);
}

// 把某个会话的终端挂到这里显示，并跟随大小变化。
export function TerminalView({ id }) {
  const container = useRef(null);
  useEffect(() => {
    const s = sessions.get(id);
    if (!s || !container.current) return;
    container.current.appendChild(s.element);
    const refit = () => { try { s.fit.fit(); } catch { /* 容器还没有尺寸 */ } };
    refit();
    s.term.focus();
    const observer = new ResizeObserver(refit);
    observer.observe(container.current);
    return () => {
      observer.disconnect();
      if (sessions.has(id)) parking().appendChild(s.element);
    };
  }, [id]);
  return html`<div class="terminal-view" ref=${container} onClick=${() => sessions.get(id)?.term.focus()}></div>`;
}
