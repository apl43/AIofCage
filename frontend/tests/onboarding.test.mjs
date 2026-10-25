import test from "node:test";
import assert from "node:assert/strict";
import { tutorialResources, tutorialStep, tutorialRoute, tutorialHasReply, fillTutorialDraft, TUTORIAL_EXAMPLE } from "../src/js/onboarding-state.js";
import { applyConversationEvent } from "../src/js/conversation-state.js";

const state = { step: 4, agent: "codex", provider_id: "p1", sandbox_id: "s1" };
const provider = { id: "p1", agent: "codex", key_saved: true };
const sandbox = { id: "s1", state: "running", bindings: [{ agent: "codex", provider_id: "p1", active_provider_id: "p1" }] };

test("恢复教程时按真实配置退回缺失步骤，不重复创建资源", () => {
  const step = (ps, ss) => tutorialStep(state, tutorialResources(state, ps, ss));
  assert.equal(step([], [sandbox]), 1);
  assert.equal(step([{ ...provider, key_saved: false }], [sandbox]), 1);
  assert.equal(step([provider], []), 2);
  assert.equal(step([provider], [{ ...sandbox, bindings: [] }]), 2);
  assert.equal(step([provider], [{ ...sandbox, state: "stopped" }]), 3);
  assert.equal(step([provider], [{ ...sandbox, operation: "启动" }]), 3);
  assert.equal(step([provider], [{ ...sandbox, config_pending: true }]), 3);
  assert.equal(step([provider], [{ ...sandbox, bindings: [{ ...sandbox.bindings[0], active_provider_id: "old" }] }]), 3);
  assert.equal(step([provider], [sandbox]), 4);
  assert.deepEqual(tutorialRoute(4, "s1"), { page: "sandboxes", detail: { id: "s1", tab: "terminal" } });
});

test("只提供智能体及供应商都匹配的已有沙箱", () => {
  const other = { ...sandbox, id: "other", bindings: [{ agent: "claude-code", provider_id: "p1" }] };
  assert.deepEqual(tutorialResources(state, [provider], [sandbox, other]).choices, [sandbox]);
});

test("错误、停止、空回复和其他会话不能标记体验完成", () => {
  const create = () => ({ messages: [], sandboxId: "s1", agent: "codex", info: { provider_id: "p1" } });
  for (const events of [
    [{ type: "done" }],
    [{ type: "delta", text: "部分回复" }, { type: "stopped" }],
    [{ type: "error", text: "认证失败" }, { type: "done" }],
    [{ type: "text", text: "部分回复" }, { type: "error", text: "网络中断" }, { type: "done" }],
    [{ type: "thinking", text: "思考中" }, { type: "done" }],
  ]) {
    const s = create();
    applyConversationEvent(s, { type: "user", text: "你好" });
    events.forEach((event) => applyConversationEvent(s, event));
    assert.equal(tutorialHasReply([s], state), false);
  }
  const s = create();
  applyConversationEvent(s, { type: "user", text: "你好" });
  applyConversationEvent(s, { type: "final", text: "你好，我可以帮助你。" });
  assert.equal(tutorialHasReply([s], state), false);
  applyConversationEvent(s, { type: "done" });
  assert.equal(tutorialHasReply([s], state), true);
  assert.equal(tutorialHasReply([{ ...s, sandboxId: "other" }], state), false);
  assert.equal(tutorialHasReply([{ ...s, info: { provider_id: "other" } }], state), false);
});

test("填入示例保留已有文字和图片，也不改动发送中的草稿", () => {
  const session = { info: {}, draft: { text: "", images: [] } };
  assert.equal(fillTutorialDraft(session), true);
  assert.equal(session.draft.text, TUTORIAL_EXAMPLE);
  for (const draft of [{ text: "用户草稿", images: [] }, { text: "", images: [{}] }, { text: "", images: [], busy: true }, { text: "", images: [], reading: true }]) {
    const before = structuredClone(draft);
    assert.equal(fillTutorialDraft({ info: {}, draft }), false);
    assert.deepEqual(draft, before);
  }
  assert.equal(fillTutorialDraft(null), false);
});
