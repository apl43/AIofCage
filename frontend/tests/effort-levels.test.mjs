import test from "node:test";
import assert from "node:assert/strict";
import { effortChoices, effortLabel, keepEffort } from "../src/js/effort-levels.js";

// 和后端 runtime.AgentEfforts(Codex) 的形状一致，只取几个模型。
const codex = {
  models: {
    "gpt-6.1-sol": { levels: ["low", "medium", "high", "xhigh", "max", "ultra"], default: "low" },
    "gpt-5.5": { levels: ["low", "medium", "high", "xhigh"], default: "medium" },
  },
  fallback: { levels: ["low", "medium", "high"] },
};

test("Codex 按模型给档位和默认档，目录外的模型用后备", () => {
  assert.deepEqual(effortChoices(codex, "gpt-6.1-sol").levels, ["low", "medium", "high", "xhigh", "max", "ultra"]);
  assert.equal(effortChoices(codex, "gpt-6.1-sol").default, "low");
  assert.deepEqual(effortChoices(codex, "deepseek-flash").levels, ["low", "medium", "high"]);
  assert.equal(effortChoices(codex, "deepseek-flash").default, undefined);
});

test("模型名是对象内置名时不会取到原型上的东西", () => {
  for (const model of ["constructor", "__proto__", "toString"]) {
    assert.deepEqual(effortChoices(codex, model).levels, ["low", "medium", "high"]);
  }
});

test("没有档位表（旧后端）时退回低、中、高", () => {
  assert.deepEqual(effortChoices(undefined, "gpt-6.1-sol").levels, ["low", "medium", "high"]);
  assert.deepEqual(effortChoices({}, "x").levels, ["low", "medium", "high"]);
});

test("换模型后不再可选的档位回到模型默认，可选的保留", () => {
  assert.equal(keepEffort(codex, "gpt-5.5", "max"), "");
  assert.equal(keepEffort(codex, "gpt-5.5", "xhigh"), "xhigh");
  assert.equal(keepEffort(codex, "deepseek-flash", "ultra"), "");
  assert.equal(keepEffort(codex, "gpt-6.1-sol", ""), "");
});

test("档位的中文名", () => {
  assert.deepEqual(["off", "minimal", "low", "medium", "high", "xhigh", "max"].map(effortLabel), ["关闭思考", "最低", "低", "中", "高", "超高", "最高"]);
  assert.match(effortLabel("ultra"), /^极限/);
  assert.equal(effortLabel("constructor"), "constructor");
});
