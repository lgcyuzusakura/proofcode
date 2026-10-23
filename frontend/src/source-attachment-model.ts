import type { SourceAttachment } from "./types";

const key = (projectId: string, conversationId?: string) => `proofcode.chat-attachments.${projectId || "scratch"}.${conversationId || "new"}`;
export function validAttachment(value: unknown): value is SourceAttachment {
  if (!value || typeof value !== "object") return false;
  const item = value as SourceAttachment;
  return typeof item.sourceSnapshotId === "string" && !!item.sourceSnapshotId && typeof item.path === "string" && !!item.path &&
    typeof item.sha256 === "string" && /^[a-f0-9]{64}$/i.test(item.sha256) &&
    [item.startLine, item.endLine].every(line => line == null || (Number.isSafeInteger(line) && line >= 1)) &&
    (item.endLine == null || item.endLine >= (item.startLine ?? 1));
}
export function readSourceAttachments(projectId: string, conversationId?: string): { items: SourceAttachment[]; error?: string } {
  try {
    const values: unknown = JSON.parse(localStorage.getItem(key(projectId, conversationId)) || "[]");
    if (!Array.isArray(values) || values.length > 8 || !values.every(validAttachment)) throw Error("invalid draft");
    return { items: values };
  } catch { return { items: [], error: "源码附件草稿无法读取，请清除附件草稿后重新附加。" }; }
}
export function saveSourceAttachments(projectId: string, conversationId: string | undefined, values: SourceAttachment[]) {
  if (values.length > 8 || !values.every(validAttachment)) throw Error("附件草稿无效，请重新选择文件或选区。");
  try {
    if (values.length) localStorage.setItem(key(projectId, conversationId), JSON.stringify(values));
    else localStorage.removeItem(key(projectId, conversationId));
  } catch { throw Error("无法保存源码附件草稿，请检查浏览器存储空间后重试。"); }
}
export function addSourceAttachment(projectId: string, conversationId: string, value: SourceAttachment) {
  if (!validAttachment(value)) throw Error("附件引用无效，请重新选择文件或选区。");
  const draft = readSourceAttachments(projectId, conversationId);
  if (draft.error) throw Error(draft.error);
  if (draft.items.some(item => item.sourceSnapshotId === value.sourceSnapshotId && item.path === value.path && (item.startLine ?? 1) === (value.startLine ?? 1) && item.endLine === value.endLine)) return;
  if (draft.items.length >= 8) throw Error("最多附加 8 个文件或选区，请先在对话中移除一些附件。");
  saveSourceAttachments(projectId, conversationId, [...draft.items, value]);
}
export async function verifiedSourceText(file: { sha256: string; content: string }): Promise<string> {
  const bytes = Uint8Array.from(atob(file.content), c => c.charCodeAt(0));
  const hash = [...new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))].map(byte => byte.toString(16).padStart(2, "0")).join("");
  if (hash !== file.sha256.toLowerCase()) throw Error("源码快照校验失败，请刷新文件后重试。");
  const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  if (text.includes("\0")) throw Error("二进制文件不能作为源码附件。");
  return text;
}
export function sourceExcerpt(text: string, item: Pick<SourceAttachment, "startLine" | "endLine">): string {
  const lines = text.replace(/\r\n?/g, "\n").split("\n"), start = item.startLine ?? 1, end = item.endLine ?? lines.length;
  if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end) || start < 1 || end < start || end > lines.length || end - start + 1 > 300) throw Error("附件选区无效或超过 300 行，请重新选择。");
  const excerpt = lines.slice(start - 1, end).join("\n");
  if (new TextEncoder().encode(excerpt).length > 32768) throw Error("附件超过 32 KiB，请缩小选区。");
  return excerpt;
}
