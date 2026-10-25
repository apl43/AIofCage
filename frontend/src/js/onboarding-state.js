// 教程只引用实际资源；点击“继续”本身不代表配置成功。
export const TUTORIAL_STEPS = ["检查环境", "配置供应商", "创建沙箱", "启动沙箱", "开始对话"];
export const TUTORIAL_EXAMPLE = "你好，请用一句话介绍你能帮我做什么。";

export function tutorialResources(state, providers, sandboxes) {
  const provider = providers.find((p) => p.id === state?.provider_id && p.agent === state?.agent);
  const choices = provider ? sandboxes.filter((s) => s.bindings.some((b) => b.agent === state.agent && b.provider_id === provider.id)) : [];
  const sandbox = choices.find((s) => s.id === state?.sandbox_id);
  const binding = sandbox?.bindings.find((b) => b.agent === state.agent);
  const running = Boolean(sandbox?.state === "running" && !sandbox.operation && !sandbox.config_pending && binding?.active_provider_id === provider?.id);
  return { provider, sandbox, choices, running };
}

export function tutorialStep(state, resources) {
  let step = state?.step ?? 0;
  if (step > 1 && !resources.provider?.key_saved) step = 1;
  if (step > 2 && !resources.sandbox) step = 2;
  if (step > 3 && !resources.running) step = 3;
  return step;
}

export function tutorialRoute(step, sandboxID) {
  if (step === 0) return { page: "settings", detail: null };
  if (step === 1) return { page: "providers", detail: null };
  return { page: "sandboxes", detail: step >= 3 && sandboxID ? { id: sandboxID, tab: step === 4 ? "terminal" : "overview" } : null };
}

export function tutorialHasReply(sessions, state) {
  return sessions.some((s) => s.sandboxId === state.sandbox_id && s.agent === state.agent && s.info?.provider_id === state.provider_id &&
    s.messages?.some((m) => m.role === "assistant" && m.completed && !m.error));
}

export function fillTutorialDraft(session) {
  if (!session?.info || session.exited || session.busy || session.draft.busy || session.draft.reading || session.draft.text || session.draft.images.length) return false;
  session.draft.text = TUTORIAL_EXAMPLE;
  return true;
}
