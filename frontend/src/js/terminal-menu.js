// 菜单始终限制在终端面板内，空会话时也不会向面板左侧溢出。
export function placeTerminalMenu(details) {
  if (!details?.open) return;
  const choices = details.querySelector(".terminal-menu-choices");
  const panel = details.closest(".terminal-panel");
  if (!choices || !panel) return;
  const bounds = panel.getBoundingClientRect();
  const anchor = details.getBoundingClientRect();
  choices.style.maxWidth = `${Math.max(0, bounds.width - 16)}px`;
  const width = choices.getBoundingClientRect().width;
  const left = Math.max(bounds.left + 8, Math.min(anchor.left, bounds.right - width - 8));
  choices.style.left = `${left - anchor.left}px`;
  choices.style.maxHeight = `${Math.max(40, bounds.bottom - choices.getBoundingClientRect().top - 8)}px`;
}

export function terminalMenuKey(event) {
  const details = event.currentTarget;
  const summary = details.querySelector("summary");
  if (event.key === "Escape" && details.open) {
    event.preventDefault();
    details.open = false;
    summary.focus();
    return;
  }
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
  if (!details.open && event.target !== summary) return;
  event.preventDefault();
  details.open = true;
  placeTerminalMenu(details);
  const items = [...details.querySelectorAll(".terminal-menu-choices button:not(:disabled)")];
  if (!items.length) return;
  const current = items.indexOf(document.activeElement);
  const next = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : current < 0
    ? (event.key === "ArrowUp" ? items.length - 1 : 0)
    : (current + (event.key === "ArrowUp" ? -1 : 1) + items.length) % items.length;
  items[next].focus();
}
