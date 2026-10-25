import { useState } from "preact/hooks";
import { html, call, Icon } from "./core.js";

export function PricingLookup({ model, onApply }) {
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState(null);
  const [error, setError] = useState("");
  const lookup = async () => {
    setLoading(true); setError(""); setResult(null);
    const requested = model.trim();
    try { setResult({ model: requested, items: await call("PricingSuggestions", requested) }); }
    catch (error) { setError(error.message); }
    finally { setLoading(false); }
  };
  return html`<div class="pricing-lookup">
    <button type="button" onClick=${lookup} disabled=${loading || !model.trim()}><${Icon} name="search" size=${17} />${loading ? "正在读取价格…" : "查找公开单价"}</button>
    <p class="caption">来源 models.dev，美元 / 百万 token，按同名模型精确匹配。中转加价、长上下文阶梯和工具费用请按供应商报价调整；缺少缓存单价时按普通输入估算。</p>
    ${error && html`<p class="form-error" role="alert">${error}</p>`}
    ${result?.model === model.trim() && html`<div role="status">${!result.items.length && html`<p class="caption">未找到同名模型的单价，请手动填写。</p>`}</div>
      <div class="pricing-candidates">${result.items.map((item) => html`<div class="pricing-candidate"><div><strong>${item.provider}</strong><span>输入 $${item.pricing.input} · 输出 $${item.pricing.output}</span><small>获取于 ${new Date(item.pricing.as_of).toLocaleString()}</small></div>
        <button type="button" onClick=${() => onApply(item.pricing)}>填入单价</button></div>`)}</div>`}
  </div>`;
}
