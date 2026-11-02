// 只处理结构化消息；正文始终作为文本渲染，不进入终端解释器。
export function applyConversationEvent(session, event) {
  if (event.type === "closed") {
    const last = session.messages[session.messages.length - 1];
    if (last?.role === "assistant" && session.busy && !last.endedAt) last.endedAt = Date.now();
    session.busy = false;
    session.exited = true;
    session.status = "沙箱会话已结束";
    return;
  }
  if (event.type === "user") {
    session.messages = session.messages.slice(-198);
    session.messages.push({ role: "user", text: event.text, images: event.images || 0 });
    session.messages.push({ role: "assistant", text: "", thinking: "", tools: [], error: "", startedAt: Date.now() });
    session.busy = true;
    session.started = true;
    session.status = "正在处理";
    return;
  }
  const message = session.messages[session.messages.length - 1];
  if (event.type === "done" || event.type === "stopped") {
    session.busy = false;
    session.status = event.type === "stopped" ? "已停止" : "本轮结束";
    if (message?.role === "assistant") {
      message.completed = event.type === "done" && !message.error && Boolean(message.text?.trim());
      message.endedAt = Date.now();
    }
    if (message?.role === "assistant" && !message.text && !message.error) message.text = event.type === "stopped" ? "任务已停止。" : "本轮已结束，未返回文本。";
    return;
  }
  if (message?.role !== "assistant") return;
  const text = String(event.text || "");
  if (event.type === "text") message.text += (message.text ? "\n\n" : "") + text;
  else if (event.type === "delta") message.text += text;
  else if (event.type === "final") message.text = text || message.text;
  else if (event.type === "thinking") message.thinking += text;
  else if (event.type === "tool" && message.tools.length < 100) message.tools.push(text);
  else if (event.type === "error") message.error = text;
  if (message.text.length > 524288) { message.text = message.text.slice(0, 524288); message.truncated = true; }
  if (message.thinking.length > 131072) message.thinking = message.thinking.slice(0, 131072);
}

// formatElapsed 把一轮用时写成“8 秒”“1 分 12 秒”“1 小时 3 分”。
export function formatElapsed(ms) {
  const total = Math.max(1, Math.round(ms / 1000));
  if (total < 60) return `${total} 秒`;
  const minutes = Math.floor(total / 60), seconds = total % 60;
  if (minutes < 60) return seconds ? `${minutes} 分 ${seconds} 秒` : `${minutes} 分`;
  const hours = Math.floor(minutes / 60), rest = minutes % 60;
  return rest ? `${hours} 小时 ${rest} 分` : `${hours} 小时`;
}

// activitySummary 是回复上方那一行灰字：用时、工具活动次数、有没有思考过程；什么都没有时返回空字符串。
export function activitySummary(message, busy) {
  const parts = [];
  if (busy) parts.push("正在处理");
  else if (message.startedAt && message.endedAt) parts.push(`已处理 ${formatElapsed(message.endedAt - message.startedAt)}`);
  if (message.tools?.length) parts.push(`${message.tools.length} 次工具活动`);
  if (message.thinking) parts.push("思考过程");
  return parts.join(" · ");
}

// tabTitle 给新标签起名：同名的标签按序号区分，用最小的空闲序号，例如“Codex 1”“Codex 2”。
export function tabTitle(name, titles) {
  const used = new Set(titles);
  let n = 1;
  while (used.has(`${name} ${n}`)) n++;
  return `${name} ${n}`;
}

// recoveryPanel 把一个沙箱的恢复记录分成两类：ready 可以直接恢复（已经打开的除外），
// stale 不能恢复的只作为诊断信息，不出现“恢复”按钮。
export function recoveryPanel(recoverable, sandboxId, openIds) {
  const open = new Set(openIds);
  const mine = (recoverable ?? []).filter((item) => item.sandbox_id === sandboxId && !open.has(item.id));
  return { ready: mine.filter((item) => item.ready), stale: mine.filter((item) => !item.ready) };
}
