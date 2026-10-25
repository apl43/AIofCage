import { useEffect, useState } from "preact/hooks";
import { html, call, Icon, ConfirmDialog, formatBytes } from "./core.js";

export function EnvironmentResources({ notify, onChanged }) {
  const [settings, setSettings] = useState(null);
  const [form, setForm] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState(false);
  const load = async () => { const value = await call("EnvironmentResources"); setSettings(value); setForm(value.desired); };
  useEffect(() => { load().catch((error) => setError(error.message)); }, []);
  const set = (key) => (event) => setForm({ ...form, [key]: Number(event.target.value) });
  const save = async (event) => {
    event.preventDefault(); setBusy(true); setError("");
    try { await call("SaveEnvironmentResources", form); await load(); notify("资源配置已保存，下次启动运行环境时应用"); }
    catch (error) { setError(error.message); }
    finally { setBusy(false); }
  };
  const suspend = async () => {
    setBusy(true); setError(""); setConfirm(false);
    try { await call("SuspendEnvironment"); await load(); await onChanged(); notify("运行环境已停止，启动沙箱时应用保存的资源配置"); }
    catch (error) { setError(error.message); }
    finally { setBusy(false); }
  };
  return html`<section class="panel environment-resources"><div class="section-heading"><div><h2>运行环境资源</h2><p>所有沙箱共用专用虚拟机，按实际任务量分配资源。</p></div><${Icon} name="cpu" size=${24} /></div>
    ${settings && form && html`<p class="resource-actual">${settings.actual ? `当前：${settings.actual.cpus} CPU · ${formatBytes(settings.actual.memory)} 内存 · ${formatBytes(settings.actual.disk)} 磁盘 · ${settings.actual.status === "Running" ? "运行中" : "已停止"}` : "尚未创建运行环境"}${settings.pending && html` <span class="pill warn">待运行环境重启</span>`}</p>
      <form onSubmit=${save}><div class="resource-fields"><label>CPU 核数<input type="number" required min="1" max=${settings.host_cpus} step="1" value=${form.cpus} onInput=${set("cpus")} disabled=${busy} /></label>
        <label>内存（GiB）<input type="number" required min="2" max=${settings.host_memory_gib ? Math.min(128, settings.host_memory_gib - 2) : 128} step="1" value=${form.memory_gib} onInput=${set("memory_gib")} disabled=${busy} /></label>
        <label>磁盘上限（GiB）<input type="number" required min=${Math.max(20, Math.ceil((settings.actual?.disk || 0) / 2 ** 30))} max="1024" step="1" value=${form.disk_gib} onInput=${set("disk_gib")} disabled=${busy} /></label></div>
        <p class="caption">宿主 ${settings.host_cpus} 核${settings.host_memory_gib ? ` / ${settings.host_memory_gib} GiB` : ""}。内存为虚拟机分配上限；虚拟磁盘按需占用，扩容后不能缩小。</p>
        <div class="card-actions"><button class="primary" disabled=${busy}>${busy ? "处理中…" : "保存资源配置"}</button>
          <button type="button" disabled=${busy || settings.actual?.status !== "Running"} onClick=${() => setConfirm(true)}>停止运行环境</button></div>
        <p class="caption">保存后，先停止所有沙箱，再停止运行环境。下次启动沙箱时应用配置。</p>
      </form>`}
    ${(error || settings?.error) && html`<p class="form-error" role="alert">${error || settings.error}</p>`}
    ${confirm && html`<${ConfirmDialog} title="停止运行环境？" message="仅在专用虚拟机中没有运行容器时停止。项目、镜像和主目录保留；下次启动沙箱时重新启动。" confirmLabel="停止运行环境" onClose=${() => setConfirm(false)} onConfirm=${suspend} />`}
  </section>`;
}
