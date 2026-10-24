// 每个终端独立持有草稿与预览；上传只发生在发送时。
export const IMAGE_ACCEPT = "image/png,image/jpeg,image/gif,image/webp";
const formats = new Set(IMAGE_ACCEPT.split(","));
const maxImageBytes = 10 * 1024 * 1024;
const maxTotalBytes = 20 * 1024 * 1024;

export function createDraft() {
  return { text: "", images: [], busy: false, reading: false, error: "", status: "", disposed: false };
}

export function addImages(draft, files) {
  if (draft.disposed || draft.busy) return;
  files = Array.from(files);
  if (draft.images.length + files.length > 4) throw new Error("每次最多发送 4 张图片");
  let total = draft.images.reduce((sum, item) => sum + item.file.size, 0);
  for (const file of files) {
    if (!formats.has(file.type)) throw new Error("请选择 PNG、JPEG、GIF 或 WebP 图片");
    if (!file.size || file.size > maxImageBytes) throw new Error("每张图片需小于或等于 10 MB");
    total += file.size;
  }
  if (total > maxTotalBytes) throw new Error("图片总大小不能超过 20 MB");
  // 全部检查通过后再添加，避免多图操作只保留一部分。
  const added = [];
  try {
    for (const file of files) added.push({ id: crypto.randomUUID(), file, url: URL.createObjectURL(file) });
  } catch (error) {
    added.forEach((item) => URL.revokeObjectURL(item.url));
    throw error;
  }
  draft.images.push(...added);
  draft.error = "";
  draft.status = `已添加 ${draft.images.length} 张图片`;
}

export function removeImage(draft, id) {
  if (draft.busy) return;
  const index = draft.images.findIndex((item) => item.id === id);
  if (index < 0) return;
  URL.revokeObjectURL(draft.images[index].url);
  draft.images.splice(index, 1);
  draft.error = "";
  draft.status = draft.images.length ? `还剩 ${draft.images.length} 张图片` : "已移除图片";
}

export function disposeDraft(draft) {
  draft.disposed = true;
  draft.images.forEach((item) => URL.revokeObjectURL(item.url));
  draft.images = [];
  draft.text = "";
}

async function encodeImage(file) {
  const bytes = new Uint8Array(await file.arrayBuffer());
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 8192) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
  }
  return { data: btoa(binary) };
}

export async function sendDraft(draft, transport, changed) {
  if (draft.disposed || draft.busy || draft.reading) return false;
  if (!draft.text.trim() && !draft.images.length) {
    draft.error = "请输入内容或添加图片";
    changed();
    return false;
  }
  draft.busy = true;
  draft.error = "";
  draft.status = "正在发送…";
  changed();
  try {
    const images = await Promise.all(draft.images.map((item) => encodeImage(item.file)));
    if (draft.disposed) return false;
    await transport(draft.text, images);
    draft.images.forEach((item) => URL.revokeObjectURL(item.url));
    draft.images = [];
    draft.text = "";
    draft.status = "已发送";
    return true;
  } catch (error) {
    draft.error = error.message;
    draft.status = "";
    return false;
  } finally {
    draft.busy = false;
    if (!draft.disposed) changed();
  }
}
