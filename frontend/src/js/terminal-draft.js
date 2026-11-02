// 每个终端独立持有草稿与预览；上传只发生在发送时。
// 附件可以是任意文件：PNG、JPEG、GIF、WebP 图片交给模型看，其他文件由后端复制进沙箱，路径附在消息后面，
// 智能体用自己的工具读取。后端按内容再判断一次是不是图片。
export const IMAGE_TYPES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);
const maxFiles = 4;
const maxFileBytes = 10 * 1024 * 1024;
const maxTotalBytes = 20 * 1024 * 1024;

export function createDraft() {
  return { text: "", attachments: [], busy: false, reading: false, error: "", status: "", disposed: false };
}

export function addAttachments(draft, files) {
  if (draft.disposed || draft.busy) return;
  files = Array.from(files);
  if (draft.attachments.length + files.length > maxFiles) throw new Error("每次最多发送 4 个附件");
  let total = draft.attachments.reduce((sum, item) => sum + item.file.size, 0);
  for (const file of files) {
    if (!file.size) throw new Error(`「${file.name}」是空文件，请重新选择`);
    if (file.size > maxFileBytes) throw new Error("每个附件需小于或等于 10 MB");
    total += file.size;
  }
  if (total > maxTotalBytes) throw new Error("附件总大小不能超过 20 MB");
  // 全部检查通过后再添加，避免多选时只保留一部分；只有图片生成预览。
  const added = [];
  try {
    for (const file of files) {
      const image = IMAGE_TYPES.has(file.type);
      added.push({ id: crypto.randomUUID(), file, image, url: image ? URL.createObjectURL(file) : "" });
    }
  } catch (error) {
    added.forEach((item) => item.url && URL.revokeObjectURL(item.url));
    throw error;
  }
  draft.attachments.push(...added);
  draft.error = "";
  draft.status = `已添加 ${draft.attachments.length} 个附件`;
}

export function removeAttachment(draft, id) {
  if (draft.busy) return;
  const index = draft.attachments.findIndex((item) => item.id === id);
  if (index < 0) return;
  if (draft.attachments[index].url) URL.revokeObjectURL(draft.attachments[index].url);
  draft.attachments.splice(index, 1);
  draft.error = "";
  draft.status = draft.attachments.length ? `还剩 ${draft.attachments.length} 个附件` : "已移除附件";
}

export function disposeDraft(draft) {
  draft.disposed = true;
  draft.attachments.forEach((item) => item.url && URL.revokeObjectURL(item.url));
  draft.attachments = [];
  draft.text = "";
}

// pasteKind 决定粘贴进来的文件怎么处理：只有一张 TIFF 时多半是截图，交给后端读剪贴板转成 PNG（"clipboard"）；
// 其他情况都把文件直接当附件（"files"）。从访达复制的 HEIC、SVG 等文件如果也走剪贴板，读到的会是文件图标。
export function pasteKind(files) {
  return files.length === 1 && files[0].type === "image/tiff" || !files.length ? "clipboard" : "files";
}

// formatSize 写附件大小，例如“812 B”“2.4 KB”“3.1 MB”。
export function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

async function encodeAttachment(file) {
  const bytes = new Uint8Array(await file.arrayBuffer());
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 8192) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
  }
  return { data: btoa(binary), name: file.name };
}

export async function sendDraft(draft, transport, changed) {
  if (draft.disposed || draft.busy || draft.reading) return false;
  if (!draft.text.trim() && !draft.attachments.length) {
    draft.error = "请输入内容或添加附件";
    changed();
    return false;
  }
  draft.busy = true;
  draft.error = "";
  draft.status = "正在发送…";
  changed();
  try {
    const attachments = await Promise.all(draft.attachments.map((item) => encodeAttachment(item.file)));
    if (draft.disposed) return false;
    await transport(draft.text, attachments);
    draft.attachments.forEach((item) => item.url && URL.revokeObjectURL(item.url));
    draft.attachments = [];
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
