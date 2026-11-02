import test from "node:test";
import assert from "node:assert/strict";
import { createDraft, addAttachments, removeAttachment, disposeDraft, sendDraft, formatSize } from "../src/js/terminal-draft.js";

const image = (name = "截图.png", size = 64) => new File([new Uint8Array(size)], name, { type: "image/png" });

test("各终端独立保存草稿，发送前不调用后端", () => {
  const first = createDraft(), second = createDraft();
  first.text = "分析这张图";
  addAttachments(first, [image()]);
  assert.equal(first.attachments.length, 1);
  assert.equal(second.attachments.length, 0);
  assert.equal(second.text, "");
  disposeDraft(first);
  disposeDraft(second);
});

test("数量、空文件和体积校验失败时保留原来的附件", () => {
  const draft = createDraft();
  addAttachments(draft, [image()]);
  assert.throws(() => addAttachments(draft, [image(), image(), image(), image()]), /4 个附件/);
  assert.throws(() => addAttachments(draft, [new File([], "空.txt", { type: "text/plain" })]), /空文件/);
  assert.throws(() => addAttachments(draft, [image("大图.png", 10 * 1024 * 1024 + 1)]), /10 MB/);
  assert.throws(() => addAttachments(draft, [image("一.png", 10 * 1024 * 1024), image("二.png", 10 * 1024 * 1024)]), /20 MB/);
  assert.equal(draft.attachments.length, 1);
  disposeDraft(draft);
});

test("任意文件都能作为附件，只有四种图片生成预览，发送时带上文件名", async () => {
  const draft = createDraft();
  addAttachments(draft, [image(), new File(["# 说明"], "报告.md", { type: "text/markdown" }), new File(["<svg/>"], "图.svg", { type: "image/svg+xml" })]);
  assert.deepEqual(draft.attachments.map((item) => [item.file.name, item.image, Boolean(item.url)]), [["截图.png", true, true], ["报告.md", false, false], ["图.svg", false, false]]);
  let sent;
  assert.equal(await sendDraft(draft, async (text, attachments) => { sent = attachments; }, () => {}), true);
  assert.deepEqual(sent.map((item) => item.name), ["截图.png", "报告.md", "图.svg"]);
  assert.equal(atob(sent[1].data), unescape(encodeURIComponent("# 说明")));
  assert.equal(draft.attachments.length, 0);
});

test("附件大小写成 B、KB、MB", () => {
  assert.equal(formatSize(812), "812 B");
  assert.equal(formatSize(2458), "2.4 KB");
  assert.equal(formatSize(3250586), "3.1 MB");
});

test("发送失败后图片和文字可重试，成功后释放草稿", async () => {
  const draft = createDraft();
  draft.text = "第一行\n第二行";
  addAttachments(draft, [image()]);
  let calls = 0;
  assert.equal(await sendDraft(draft, async () => { calls++; throw new Error("沙箱已停止"); }, () => {}), false);
  assert.equal(draft.text, "第一行\n第二行");
  assert.equal(draft.attachments.length, 1);
  assert.equal(draft.error, "沙箱已停止");
  assert.equal(await sendDraft(draft, async (text, images) => {
    calls++;
    assert.equal(text, "第一行\n第二行");
    assert.equal(atob(images[0].data).length, 64);
  }, () => {}), true);
  assert.equal(calls, 2);
  assert.equal(draft.attachments.length, 0);
  assert.equal(draft.text, "");
});

test("纯图可发送，重复提交只发送一次，关闭后不发送", async () => {
  const draft = createDraft();
  addAttachments(draft, [image()]);
  let resolve, calls = 0;
  const pending = sendDraft(draft, () => { calls++; return new Promise((done) => { resolve = done; }); }, () => {});
  assert.equal(await sendDraft(draft, () => { calls++; }, () => {}), false);
  await new Promise((done) => setImmediate(done));
  assert.equal(calls, 1);
  resolve();
  await pending;
  addAttachments(draft, [image()]);
  removeAttachment(draft, draft.attachments[0].id);
  assert.equal(draft.attachments.length, 0);
  disposeDraft(draft);
  assert.equal(await sendDraft(draft, () => { calls++; }, () => {}), false);
  assert.equal(calls, 1);
});

test("读取附件时关闭终端，不提交已放弃的草稿", async () => {
  const draft = createDraft();
  const file = image();
  let finishReading, calls = 0;
  file.arrayBuffer = () => new Promise((done) => { finishReading = done; });
  addAttachments(draft, [file]);
  const pending = sendDraft(draft, () => { calls++; }, () => {});
  disposeDraft(draft);
  finishReading(new ArrayBuffer(64));
  assert.equal(await pending, false);
  assert.equal(calls, 0);
  assert.equal(draft.attachments.length, 0);
});
