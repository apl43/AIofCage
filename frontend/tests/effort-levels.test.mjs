import test from "node:test";
import assert from "node:assert/strict";
import { effortChoices, effortLabel, effortNote, keepEffort, pickEffort } from "../src/js/effort-levels.js";

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

test("档位显示英文原名，不翻译；xhigh 写成 Extra high", () => {
  assert.deepEqual(["off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"].map(effortLabel), ["Off", "Minimal", "Low", "Medium", "High", "Extra high", "Max", "Ultra"]);
  assert.equal(effortLabel("constructor"), "constructor");
  assert.equal(effortNote("ultra"), "自动分派子任务");
  assert.equal(effortNote("low"), "");
  assert.equal(effortNote("constructor"), "");
});

test("选中 CLI 的默认档时存成空字符串，和没动过一样", () => {
  const choices = effortChoices(codex, "gpt-6.1-sol");
  assert.equal(pickEffort(choices, "low"), "");
  assert.equal(pickEffort(choices, "max"), "max");
  assert.equal(pickEffort(effortChoices(codex, "deepseek-flash"), "low"), "low");
});
