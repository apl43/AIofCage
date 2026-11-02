import test from "node:test";
import assert from "node:assert/strict";
import { applyConversationEvent, tabTitle, recoveryPanel, formatElapsed, activitySummary } from "../src/js/conversation-state.js";

test("用户、回复、工具与完整 final 分开保留", () => {
  const session = { messages: [] };
  applyConversationEvent(session, { type: "user", text: "问题\n第二行", images: 1 });
  applyConversationEvent(session, { type: "text", text: "截断回复" });
  applyConversationEvent(session, { type: "tool", text: "query" });
  applyConversationEvent(session, { type: "final", text: "完整回复" });
  applyConversationEvent(session, { type: "done" });
  assert.equal(session.messages[0].role, "user");
  assert.equal(session.messages[0].text, "问题\n第二行");
  assert.equal(session.messages[1].text, "完整回复");
  assert.deepEqual(session.messages[1].tools, ["query"]);
  assert.equal(session.busy, false);
});

test("续聊不拼入上轮回复，停止保留部分输出", () => {
  const session = { messages: [] };
  for (const text of ["第一问", "第二问"]) {
    applyConversationEvent(session, { type: "user", text });
    applyConversationEvent(session, { type: "delta", text: "部分" });
    applyConversationEvent(session, { type: "stopped" });
  }
  assert.equal(session.messages.length, 4);
  assert.equal(session.messages[3].text, "部分");
  assert.equal(session.status, "已停止");
});

test("同名标签按最小的空闲序号编号", () => {
  assert.equal(tabTitle("Codex", []), "Codex 1");
  assert.equal(tabTitle("Codex", ["Codex 1", "Shell 1"]), "Codex 2");
  assert.equal(tabTitle("Codex", ["Codex 2"]), "Codex 1");
  assert.equal(tabTitle("Shell", ["Codex 1", "Codex 2"]), "Shell 1");
});

test("恢复面板只列出真能恢复、还没打开的会话，其余作为诊断", () => {
  const records = [
    { id: "a", sandbox_id: "s1", ready: true },
    { id: "b", sandbox_id: "s1", ready: false, reason: "供应商配置已变化" },
    { id: "c", sandbox_id: "s1", ready: true },
    { id: "d", sandbox_id: "s2", ready: true },
  ];
  const { ready, stale } = recoveryPanel(records, "s1", ["c"]);
  assert.deepEqual(ready.map((item) => item.id), ["a"]);
  assert.deepEqual(stale.map((item) => item.id), ["b"]);
  assert.deepEqual(recoveryPanel(undefined, "s1", []), { ready: [], stale: [] });
});

test("一轮用时写成秒、分、小时", () => {
  assert.equal(formatElapsed(0), "1 秒");
  assert.equal(formatElapsed(8400), "8 秒");
  assert.equal(formatElapsed(72000), "1 分 12 秒");
  assert.equal(formatElapsed(120000), "2 分");
  assert.equal(formatElapsed(3780000), "1 小时 3 分");
  assert.equal(formatElapsed(7200000), "2 小时");
});

test("回复上方的处理摘要：进行中、用时、工具次数和思考过程", () => {
  assert.equal(activitySummary({ tools: [], thinking: "" }, true), "正在处理");
  assert.equal(activitySummary({ tools: ["a", "b"], thinking: "想", startedAt: 1000, endedAt: 73000 }, false), "已处理 1 分 12 秒 · 2 次工具活动 · 思考过程");
  assert.equal(activitySummary({ tools: [], thinking: "" }, false), "");
  const session = { messages: [] };
  applyConversationEvent(session, { type: "user", text: "问" });
  assert.ok(session.messages[1].startedAt > 0);
  applyConversationEvent(session, { type: "done" });
  assert.ok(session.messages[1].endedAt >= session.messages[1].startedAt);
  assert.match(activitySummary(session.messages[1], false), /^已处理 \d+ 秒$/);
});

test("沙箱会话中途结束时记下这一轮的结束时间", () => {
  const session = { messages: [], busy: false };
  applyConversationEvent(session, { type: "user", text: "问" });
  applyConversationEvent(session, { type: "closed" });
  assert.ok(session.messages[1].endedAt > 0);
  assert.equal(session.exited, true);
});

test("用户消息记下附件里的文件名，旧后端没有 files 时为空", () => {
  const session = { messages: [] };
  applyConversationEvent(session, { type: "user", text: "看看", images: 1, files: ["报告.pdf", "数据.csv"] });
  assert.deepEqual(session.messages[0].files, ["报告.pdf", "数据.csv"]);
  applyConversationEvent(session, { type: "done" });
  applyConversationEvent(session, { type: "user", text: "再看", images: 0 });
  assert.deepEqual(session.messages[2].files, []);
});
