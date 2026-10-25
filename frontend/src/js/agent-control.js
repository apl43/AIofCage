import { useEffect, useState } from "preact/hooks";
import { html, call, Icon } from "./core.js";

export function AgentControlSettings({ notify }) {
  const [config, setConfig] = useState(null);
  useEffect(() => { call("AgentControlConfig").then(setConfig).catch((error) => notify(error.message, true)); }, []);
  const text = config ? JSON.stringify({ mcpServers: { "ai-of-cage": config } }, null, 2) : "";
  const copy = async () => {
    try {
      if (window.runtime?.ClipboardSetText) await window.runtime.ClipboardSetText(text);
      else await navigator.clipboard.writeText(text);
      notify("本机 MCP 配置已复制，添加到外部 AI 的 MCP 设置中");
    } catch { notify("复制失败，可选中下方配置手动复制", true); }
  };
  return html`<section class="panel agent-control-settings"><div class="section-heading"><div><h2>本机 AI 调用</h2><p>让外部 AI 按供应商创建沙箱、批量实验并读取结果。</p></div><${Icon} name="plug" size=${24} /></div>
    <p>每次连接只管理自己新建的沙箱，最多 2 个并发任务。正常断开后停止任务和沙箱，保留项目文件。</p>
    <div class="card-actions"><button onClick=${copy} disabled=${!config}><${Icon} name="copy" size=${18} />复制 MCP 配置</button><span class="pill idle">按需启动 · 本机连接</span></div>
    <details><summary>查看连接配置与额度</summary><label for="agent-control-config">外部 AI 的 MCP 配置</label><textarea id="agent-control-config" class="mono" rows="9" readonly value=${text} />
      <p class="caption">仅使用标准输入/输出，不开放网络端口。每个沙箱默认最多 100 次请求、100,000 token，外部 AI 可以调低额度。模型使用你选择的供应商，会产生对应费用。</p></details>
  </section>`;
}
