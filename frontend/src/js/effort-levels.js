// 思考强度：档位表由后端随会话下发（ConversationInfo.efforts），和沙箱里官方 CLI 认的档位一致，Codex 按模型区分。
// 档位名照 ChatGPT、Claude 自己界面上的写法显示英文原名，不翻译；只有 xhigh 写成 OpenAI 用的“Extra high”。
const LABELS = { off: "Off", minimal: "Minimal", low: "Low", medium: "Medium", high: "High", xhigh: "Extra high", max: "Max", ultra: "Ultra" };
const NOTES = { ultra: "自动分派子任务" };
const FALLBACK = { levels: ["low", "medium", "high"] };

export function effortLabel(value) {
  return Object.hasOwn(LABELS, value) ? LABELS[value] : value;
}

// effortNote 是档位旁边的灰字说明，没有就返回空字符串。
export function effortNote(value) {
  return Object.hasOwn(NOTES, value) ? NOTES[value] : "";
}

// 这个模型能选的档位和 CLI 自己的默认档；没有档位表（旧后端）时退回低、中、高。
export function effortChoices(table, model) {
  if (table?.models && Object.hasOwn(table.models, model)) return table.models[model];
  return table?.fallback?.levels ? table.fallback : FALLBACK;
}

// 换了模型，原来选的档位不再可选时回到模型默认。
export function keepEffort(table, model, effort) {
  return !effort || effortChoices(table, model).levels.includes(effort) ? effort : "";
}

// 选中 CLI 自己的默认档时存成空字符串：发出去的设置和没动过一样，由 CLI 用它的默认档。
export function pickEffort(choices, level) {
  return level === choices.default ? "" : level;
}
