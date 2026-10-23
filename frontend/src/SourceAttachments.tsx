import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { FileCode2, Loader2, X } from "lucide-react";
import { api } from "./api";
import type { SourceAttachment } from "./types";
import { readSourceAttachments, saveSourceAttachments, sourceExcerpt, verifiedSourceText } from "./source-attachment-model";
export { addSourceAttachment, readSourceAttachments, saveSourceAttachments } from "./source-attachment-model";

export function SourceAttachments({ projectId, workspaceId, conversationId, items, onRemove }: { projectId: string; workspaceId?: string; conversationId?: string; items: SourceAttachment[]; onRemove: (index: number) => void }) {
  const [selected, setSelected] = useState<SourceAttachment | null>(null), [content, setContent] = useState(""), [error, setError] = useState(""), [loading, setLoading] = useState(false);
  const [draftError, setDraftError] = useState(() => readSourceAttachments(projectId, conversationId).error || "");
  const request = useRef(0), abort = useRef<AbortController | null>(null), dialog = useRef<HTMLDivElement>(null), previous = useRef<HTMLElement | null>(null);
  const close = () => { request.current++; abort.current?.abort(); setSelected(null); };
  useEffect(() => () => { request.current++; abort.current?.abort(); }, []);
  useEffect(() => {
    if (!selected) return;
    previous.current = document.activeElement as HTMLElement;
    dialog.current?.querySelector<HTMLButtonElement>("button")?.focus();
    const overflow = document.body.style.overflow; document.body.style.overflow = "hidden";
    const keys = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); close(); }
      if (event.key === "Tab") {
        const nodes = [...(dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled),[tabindex="0"]') || [])];
        const first = nodes[0], last = nodes[nodes.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
      }
    };
    window.addEventListener("keydown", keys);
    return () => { document.body.style.overflow = overflow; window.removeEventListener("keydown", keys); if (previous.current?.isConnected) previous.current.focus(); };
  }, [selected]);
  const preview = async (item: SourceAttachment) => {
    abort.current?.abort(); const controller = new AbortController(); abort.current = controller;
    const current = ++request.current; setSelected(item); setContent(""); setError(""); setLoading(true);
    try {
      if (!workspaceId) throw Error("当前工作区不可用，请重新打开对话。");
      const archive = await api<{ files: { path: string; sha256: string; content: string }[] }>(`/api/projects/${projectId}/workspaces/${workspaceId}/sources/${item.sourceSnapshotId}/files`, { signal: controller.signal });
      const file = archive.files.find(file => file.path === item.path && file.sha256.toLowerCase() === item.sha256.toLowerCase());
      if (!file) throw Error("附件与保存的源码快照不一致，请移除后重新附加。");
      const text = sourceExcerpt(await verifiedSourceText(file), item);
      if (current === request.current) setContent(text);
    } catch (cause) { if (current === request.current) setError(String(cause)); }
    finally { if (current === request.current) setLoading(false); }
  };
  return <>
    {draftError && <div className="source-attachment-error" role="alert">{draftError}<button type="button" className="text-button" onClick={() => {
      try { saveSourceAttachments(projectId, conversationId, []); setDraftError(""); } catch (cause) { setDraftError(String(cause)); }
    }}>清除附件草稿</button></div>}
    {items.length > 0 && <div className="source-attachment-list" aria-label="源码附件">{items.map((item,index) => <div className="source-attachment" key={`${item.sourceSnapshotId}:${item.path}:${item.startLine}:${item.endLine}`}>
      <button type="button" className="source-attachment-preview" onClick={() => void preview(item)} title={`查看源码附件 ${item.path}`}><FileCode2 size={14}/><span>{item.path}</span>{item.startLine && <small>L{item.startLine}{item.endLine && item.endLine !== item.startLine ? `–${item.endLine}` : ""}</small>}</button>
      <button type="button" className="icon-button" aria-label={`移除附件 ${item.path}`} onClick={() => onRemove(index)}><X size={13}/></button>
    </div>)}</div>}
    {selected && createPortal(<div className="modal-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) close(); }}>
      <div className="source-preview-dialog" role="dialog" aria-modal="true" aria-label="源码附件预览" ref={dialog}>
        <header><div><strong>{selected.path}</strong><small>附加时保存的源码快照{selected.startLine ? ` · 第 ${selected.startLine}–${selected.endLine || selected.startLine} 行` : ""}</small></div><button type="button" className="icon-button" aria-label="关闭附件预览" onClick={close}><X size={18}/></button></header>
        {loading ? <p role="status"><Loader2 size={16}/>读取快照…</p> : error ? <p role="alert">{error}</p> : <pre tabIndex={0}>{content || "（空文件）"}</pre>}
      </div>
    </div>, document.body)}
  </>;
}
