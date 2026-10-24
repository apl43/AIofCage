import test from "node:test";
import assert from "node:assert/strict";
import { createDraft, addImages, removeImage, disposeDraft, sendDraft } from "../src/js/terminal-draft.js";

const image = (name = "截图.png", size = 64) => new File([new Uint8Array(size)], name, { type: "image/png" });

test("各终端独立保存草稿，发送前不调用后端", () => {
  const first = createDraft(), second = createDraft();
  first.text = "分析这张图";
  addImages(first, [image()]);
  assert.equal(first.images.length, 1);
  assert.equal(second.images.length, 0);
  assert.equal(second.text, "");
  disposeDraft(first);
  disposeDraft(second);
});

test("数量、格式和体积校验失败时保留原来的附件", () => {
  const draft = createDraft();
  addImages(draft, [image()]);
  assert.throws(() => addImages(draft, [image(), image(), image(), image()]), /4 张/);
  assert.throws(() => addImages(draft, [new File(["<svg/>"], "图片.png", { type: "image/svg+xml" })]), /请选择/);
  assert.throws(() => addImages(draft, [image("大图.png", 10 * 1024 * 1024 + 1)]), /10 MB/);
  assert.throws(() => addImages(draft, [image("一.png", 10 * 1024 * 1024), image("二.png", 10 * 1024 * 1024)]), /20 MB/);
  assert.equal(draft.images.length, 1);
  disposeDraft(draft);
});

test("发送失败后图片和文字可重试，成功后释放草稿", async () => {
  const draft = createDraft();
  draft.text = "第一行\n第二行";
  addImages(draft, [image()]);
  let calls = 0;
  assert.equal(await sendDraft(draft, async () => { calls++; throw new Error("沙箱已停止"); }, () => {}), false);
  assert.equal(draft.text, "第一行\n第二行");
  assert.equal(draft.images.length, 1);
  assert.equal(draft.error, "沙箱已停止");
  assert.equal(await sendDraft(draft, async (text, images) => {
    calls++;
    assert.equal(text, "第一行\n第二行");
    assert.equal(atob(images[0].data).length, 64);
  }, () => {}), true);
  assert.equal(calls, 2);
  assert.equal(draft.images.length, 0);
  assert.equal(draft.text, "");
});

test("纯图可发送，重复提交只发送一次，关闭后不发送", async () => {
  const draft = createDraft();
  addImages(draft, [image()]);
  let resolve, calls = 0;
  const pending = sendDraft(draft, () => { calls++; return new Promise((done) => { resolve = done; }); }, () => {});
  assert.equal(await sendDraft(draft, () => { calls++; }, () => {}), false);
  await new Promise((done) => setImmediate(done));
  assert.equal(calls, 1);
  resolve();
  await pending;
  addImages(draft, [image()]);
  removeImage(draft, draft.images[0].id);
  assert.equal(draft.images.length, 0);
  disposeDraft(draft);
  assert.equal(await sendDraft(draft, () => { calls++; }, () => {}), false);
  assert.equal(calls, 1);
});

test("读取附件时关闭终端，不提交已放弃的草稿", async () => {
  const draft = createDraft();
  const file = image();
  let finishReading, calls = 0;
  file.arrayBuffer = () => new Promise((done) => { finishReading = done; });
  addImages(draft, [file]);
  const pending = sendDraft(draft, () => { calls++; }, () => {});
  disposeDraft(draft);
  finishReading(new ArrayBuffer(64));
  assert.equal(await pending, false);
  assert.equal(calls, 0);
  assert.equal(draft.images.length, 0);
});
