import type { EditorFile } from "./types";

export type EditorDraftState = { kind: "empty" } | { kind: "invalid" } | { kind: "saved" | "dirty"; file: EditorFile; draft: string };

// Desktop paths are case-insensitive. Reject ambiguous paths rather than
// assuming that an unrecognised path cannot overlap the selected patch.
function pathKey(path: string): string | null {
  const parts = path.replace(/\\/g, "/").split("/");
  if (parts.some(part => !part || part === "." || part === ".." || /["<>:|?*\x00-\x1f]|[. ]$/.test(part))) return null;
  return parts.join("/").toLowerCase();
}

export function parseEditorDraft(raw: string | null): EditorDraftState {
  if (raw === null) return { kind: "empty" };
  try {
    const value = JSON.parse(raw);
    if (!value || typeof value.file?.path !== "string" || !pathKey(value.file.path) || typeof value.file.content !== "string" || typeof value.file.hash !== "string" || typeof value.draft !== "string") return { kind: "invalid" };
    return { kind: value.draft === value.file.content ? "saved" : "dirty", file: value.file, draft: value.draft };
  } catch { return { kind: "invalid" }; }
}

export function patchDraftBlock(raw: string | null, paths: string[]): string {
  const state = parseEditorDraft(raw);
  if (state.kind === "invalid") return "无法确认编辑器草稿状态，已停止应用。请打开代码页检查，草稿仍保留在当前设备。";
  if (state.kind !== "dirty") return "";
  const keys = paths.map(pathKey);
  if (keys.includes(null)) return "无法确认补丁与编辑器草稿是否重叠，请先保存草稿再应用。";
  return keys.includes(pathKey(state.file.path)) ? `“${state.file.path}”还有未保存修改。请先在代码页保存，再返回重试；草稿已保留。` : "";
}
