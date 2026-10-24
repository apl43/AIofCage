import test from "node:test";
import assert from "node:assert/strict";
import { installFrameFallback } from "../src/js/frames.js";

function scheduler() {
  let next = 0;
  const frames = new Map(), timers = new Map();
  const scope = {
    requestAnimationFrame: (fn) => { frames.set(++next, fn); return next; },
    cancelAnimationFrame: (id) => frames.delete(id),
    setTimeout: (fn) => { timers.set(++next, fn); return next; },
    clearTimeout: (id) => timers.delete(id),
    performance: { now: () => 100 },
  };
  installFrameFallback(scope);
  return { scope, frames, timers };
}

test("正常动画帧只执行一次，并清理补偿计时器", () => {
  const { scope, frames, timers } = scheduler();
  const calls = [];
  scope.requestAnimationFrame((time) => calls.push(time));
  const fallback = [...timers.values()][0];
  [...frames.values()][0](16);
  fallback();
  assert.deepEqual(calls, [16]);
  assert.equal(timers.size + frames.size, 0);
});

test("原生动画帧暂停时补偿刷新，恢复后不会重复", () => {
  const { scope, frames, timers } = scheduler();
  const calls = [];
  scope.requestAnimationFrame((time) => calls.push(time));
  const native = [...frames.values()][0];
  [...timers.values()][0]();
  native(116);
  assert.deepEqual(calls, [100]);
  assert.equal(timers.size + frames.size, 0);
});

test("取消会同时清理两种调度，回调可以继续申请下一帧", () => {
  const { scope, frames, timers } = scheduler();
  let calls = 0;
  const id = scope.requestAnimationFrame(() => calls++);
  const late = [...timers.values()][0];
  scope.cancelAnimationFrame(id);
  late();
  assert.equal(calls, 0);
  assert.equal(timers.size + frames.size, 0);
  scope.requestAnimationFrame(() => scope.requestAnimationFrame(() => calls++));
  [...frames.values()][0](16);
  assert.equal(timers.size, 1);
  [...frames.values()][0](32);
  assert.equal(calls, 1);
  assert.equal(timers.size + frames.size, 0);
});
