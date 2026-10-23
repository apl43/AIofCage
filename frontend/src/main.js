// AIofCage 桌面界面：左侧导航 + 各页面。样式见 main.css。
import { render } from "preact";
import { useEffect, useState, useCallback } from "preact/hooks";
import { html, call } from "./js/core.js";
import { ProvidersPage } from "./js/providers.js";
import { SandboxesPage } from "./js/sandboxes.js";
import { SettingsPage, ComingSoon } from "./js/settings.js";

const PAGES = [
  { id: "sandboxes", title: "沙箱" },
  { id: "providers", title: "供应商" },
  { id: "mcp", title: "MCP" },
  { id: "logs", title: "日志" },
  { id: "settings", title: "设置" },
];

function App() {
  const [page, setPage] = useState(() => PAGES.some((p) => `#${p.id}` === location.hash) ? location.hash.slice(1) : "sandboxes");
  const [overview, setOverview] = useState(null);
  const [providers, setProviders] = useState([]);
  const [presets, setPresets] = useState([]);
  const [sandboxes, setSandboxes] = useState([]);
  const [toast, setToast] = useState(null);

  const notify = useCallback((message, isError = false) => {
    setToast({ message, isError });
    setTimeout(() => setToast((current) => (current?.message === message ? null : current)), 4000);
  }, []);

  const reload = useCallback(async () => {
    try {
      const [p, s] = await Promise.all([call("ListProviders"), call("ListSandboxes")]);
      setProviders(p);
      setSandboxes(s);
    } catch (error) {
      notify(error.message, true);
    }
  }, [notify]);

  useEffect(() => {
    call("ListPresets").then(setPresets).catch((error) => notify(error.message, true));
    reload();
    // 环境检查要访问虚拟机，单独加载，避免拖慢首屏。
    call("GetOverview").then(setOverview).catch((error) => notify(error.message, true));
  }, []);

  const content = {
    sandboxes: html`<${SandboxesPage} sandboxes=${sandboxes} environment=${overview?.environment} />`,
    providers: html`<${ProvidersPage} providers=${providers} presets=${presets} reload=${reload} notify=${notify} />`,
    mcp: html`<${ComingSoon} title="MCP 服务" text="GitHub、filesystem 等 MCP 的免密钥接入即将上线。" />`,
    logs: html`<${ComingSoon} title="请求日志" text="各沙箱门卫的实时请求、拦截与用量即将在这里汇总。" />`,
    settings: html`<${SettingsPage} overview=${overview} />`,
  }[page];

  return html`
    <aside class="sidebar">
      <div class="brand"><span class="brand-icon">C</span><span>AIofCage<small>Agent 的安全工作区</small></span></div>
      <nav aria-label="主导航">
        ${PAGES.map((item) => html`<button class=${item.id === page ? "nav-item active" : "nav-item"}
          aria-current=${item.id === page ? "page" : undefined} onClick=${() => setPage(item.id)}>${item.title}</button>`)}
      </nav>
      <div class="sidebar-footer"><span class=${overview?.environment?.ready ? "dot ok" : "dot"}></span>
        ${overview ? (overview.environment.ready ? "运行环境就绪" : "运行环境未就绪") : "正在检查环境…"}
        <small>${overview ? `v${overview.version}` : ""}</small></div>
    </aside>
    <main>
      <header><div><p class="eyebrow">YOUR AGENTS. YOUR BOUNDARIES.</p><h1>${PAGES.find((p) => p.id === page).title}</h1></div></header>
      ${toast && html`<p class=${toast.isError ? "feedback error" : "feedback"} role="status" aria-live="polite">${toast.message}</p>`}
      ${content}
    </main>`;
}

render(html`<${App} />`, document.getElementById("app"));
