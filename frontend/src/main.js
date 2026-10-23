const $ = (selector) => document.querySelector(selector);
const feedback = $("#feedback");
const dialog = $("#session-dialog");
let agents = [];

function backend() {
  const app = window.go?.service?.App;
  if (!app) throw new Error("桌面后端尚未连接，请使用 make desktop 构建后启动应用。");
  return app;
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}

function showFeedback(message) {
  feedback.textContent = message;
  feedback.hidden = false;
}

function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
}

async function refresh() {
  const app = backend();
  const [overview, sessions] = await Promise.all([app.GetOverview(), app.ListSessions()]);
  agents = overview.agents;
  $("#version").textContent = `v${overview.version} · ${overview.stage}`;
  const labels = { detected: "已检测", missing: "未找到", unsupported: "不支持", not_implemented: "待实现" };
  const environment = $("#environment");
  environment.replaceChildren();
  for (const check of overview.environment.checks) {
    const row = element("div", undefined, "check-row");
    const details = element("div");
    details.append(element("h3", check.name), element("p", check.detail));
    row.append(details, element("span", labels[check.status] ?? check.status, `check-status ${check.status}`));
    environment.append(row);
  }
  const select = $("#agent");
  const previous = select.value;
  select.replaceChildren();
  for (const agent of agents) {
    const option = element("option", `${agent.name} · ${agent.status}`);
    option.value = agent.id;
    select.append(option);
  }
  if (agents.some((agent) => agent.id === previous)) select.value = previous;
  const list = $("#session-list");
  if (!sessions.length) return;
  list.replaceChildren();
  for (const session of sessions) {
    const row = element("article", undefined, "session-row");
    const details = element("div");
    const name = agents.find((agent) => agent.id === session.agent)?.name ?? session.agent;
    details.append(element("h3", session.name), element("p", session.project_path), element("small", `${name} · 内存草稿 · 未挂载`));
    const start = element("button", "启动（未就绪）");
    start.disabled = true;
    start.title = "隔离编排、凭据和启动自检尚未实现";
    row.append(details, start);
    list.append(row);
  }
}

for (const button of document.querySelectorAll("[data-page]")) {
  button.addEventListener("click", () => {
    for (const page of document.querySelectorAll(".page")) page.hidden = page.id !== button.dataset.page;
    for (const item of document.querySelectorAll("[data-page]")) item.classList.toggle("active", item === button);
    $("#page-title").textContent = button.childNodes[0].textContent;
  });
}

$("#new-session").addEventListener("click", () => {
  $("#session-form").reset();
  $("#form-error").hidden = true;
  dialog.showModal();
});
$("#close-dialog").addEventListener("click", () => dialog.close());
$("#refresh").addEventListener("click", () => refresh().catch((error) => showFeedback(errorMessage(error))));
$("#choose-project").addEventListener("click", async () => {
  try {
    const project = await backend().SelectProject();
    if (project) {
      $("#project-path").value = project;
      if (!$("#session-name").value) $("#session-name").value = project.split("/").filter(Boolean).at(-1) ?? "项目";
    }
  } catch (error) {
    $("#form-error").textContent = errorMessage(error);
    $("#form-error").hidden = false;
  }
});
$("#session-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  $("#create-session").disabled = true;
  try {
    await backend().CreateSession({
      name: $("#session-name").value,
      project_path: $("#project-path").value,
      agent: $("#agent").value,
    });
    dialog.close();
    showFeedback("已保存内存草稿；项目尚未导入或挂载，退出应用后草稿会清除。");
    await refresh();
  } catch (error) {
    $("#form-error").textContent = errorMessage(error);
    $("#form-error").hidden = false;
  } finally {
    $("#create-session").disabled = false;
  }
});

refresh().catch((error) => showFeedback(errorMessage(error)));
