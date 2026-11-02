// 思考强度：档位表由后端随会话下发（ConversationInfo.efforts），和沙箱里官方 CLI 认的档位一致，Codex 按模型区分。
const LABELS = { off: "关闭思考", minimal: "最低", low: "低", medium: "中", high: "高", xhigh: "超高", max: "最高", ultra: "极限（自动分派子任务）" };
const FALLBACK = { levels: ["low", "medium", "high"] };

export function effortLabel(value) {
  return Object.hasOwn(LABELS, value) ? LABELS[value] : value;
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
