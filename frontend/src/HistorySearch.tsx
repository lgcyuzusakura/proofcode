import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ArrowUpRight, FileText, Loader2, MessageSquare, Search, X } from "lucide-react";
import { api } from "./api";
import type { Project } from "./types";
import "./history.css";

export type HistoryHit = { kind: "CONVERSATION" | "MESSAGE" | "TASK"; id: string; projectId: string; projectName: string; workspaceId: string; conversationId: string; conversationTitle: string; taskId: string | null; messageId: string | null; role: string | null; excerpt: string; createdAt: string };
type Results = { items: HistoryHit[]; nextCursor: string | null };
const labels = { CONVERSATION: "对话", MESSAGE: "消息", TASK: "任务" };

export function HistorySearch({ projects, currentProjectId, initialQuery, onClose, onOpen }: { projects: Project[]; currentProjectId: string; initialQuery: string; onClose: () => void; onOpen: (hit: HistoryHit, signal: AbortSignal) => Promise<boolean> }) {
 const [query, setQuery] = useState(initialQuery), [project, setProject] = useState(""), [kind, setKind] = useState("ALL");
 const [items, setItems] = useState<HistoryHit[]>([]), [cursor, setCursor] = useState<string | null>(null), [busy, setBusy] = useState(false), [opening, setOpening] = useState(""), [error, setError] = useState("");
 const generation = useRef(0), request = useRef<AbortController | null>(null), navigation = useRef<AbortController | null>(null), dialog = useRef<HTMLElement>(null), input = useRef<HTMLInputElement>(null);
 const trigger = useRef(document.activeElement as HTMLElement | null);
 const restoreFocus = useRef(true);
 const key = `${project}\n${kind}\n${query}`;
 const latestKey = useRef(key); latestKey.current = key;
 const load = async (next: string | null = null) => {
  request.current?.abort();const controller = new AbortController();request.current = controller;
  const id = ++generation.current, identity = latestKey.current;setBusy(true);setError("");
  if (!next) { setItems([]);setCursor(null); }
  const params = new URLSearchParams({ query: query.trim(), kind, limit: "25" });if (project) params.set("projectId", project);if (next) params.set("cursor", next);
  try {
   const result = await api<Results>(`/api/history/search?${params}`, { signal: controller.signal });
   if (!Array.isArray(result.items) || (result.nextCursor && result.nextCursor === next)) throw new Error("搜索分页无效，请重新搜索。");
   if (id !== generation.current || identity !== latestKey.current || controller.signal.aborted) return;
   setItems(old => next ? [...old, ...result.items.filter(hit => !old.some(value => value.kind === hit.kind && value.id === hit.id))] : result.items);setCursor(result.nextCursor);
  } catch (cause) { if (!controller.signal.aborted && id === generation.current && identity === latestKey.current) setError(String(cause)); }
  finally { if (id === generation.current && identity === latestKey.current) setBusy(false); }
 };
 useEffect(() => { request.current?.abort();generation.current++;setItems([]);setCursor(null);setError("");setBusy(true);const timer = setTimeout(() => { void load(); }, 250);return () => clearTimeout(timer); }, [key]);
 useEffect(() => {
  input.current?.focus();const oldOverflow = document.body.style.overflow;document.body.style.overflow = "hidden";
  const keys = (event: KeyboardEvent) => {
   if (event.key === "Escape") { event.preventDefault();onClose(); }
   if (event.key === "Tab") {
    const nodes = [...(dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),[tabindex="0"]') || [])];
    const first = nodes[0], last = nodes[nodes.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault();last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault();first?.focus(); }
   }
  };
  window.addEventListener("keydown", keys);return () => {
   request.current?.abort();navigation.current?.abort();generation.current++;document.body.style.overflow = oldOverflow;window.removeEventListener("keydown", keys);
   if (restoreFocus.current) requestAnimationFrame(() => {
    // A viewport change can commit the sidebar layout after this portal unmounts.
    // Restore after layout, without stealing focus from another dialog or user action.
    if (document.activeElement !== document.body && document.activeElement?.isConnected) return;
    const target = trigger.current;
    if (target?.isConnected && target.getClientRects().length) target.focus({ preventScroll: true });
    else document.querySelector<HTMLButtonElement>('button[title="展开导航"]')?.focus({ preventScroll: true });
   });
  };
 }, []);
 const open = async (hit: HistoryHit) => {
  if (opening) return;const controller = new AbortController();navigation.current = controller;setOpening(`${hit.kind}:${hit.id}`);setError("");
  try { if (await onOpen(hit, controller.signal)) { restoreFocus.current = !hit.messageId;onClose(); } }
  catch (cause) { if (!controller.signal.aborted) setError(String(cause)); }
  finally { if (!controller.signal.aborted) setOpening(""); }
 };
 return createPortal(<div className="history-backdrop"><section className="history-search-panel" role="dialog" aria-modal="true" aria-labelledby="history-search-title" ref={dialog}>
  <header><div><h2 id="history-search-title">搜索历史</h2><p>查找对话、消息和任务结果，打开原来的位置。</p></div><button className="history-icon-button" aria-label="关闭历史搜索" onClick={onClose}><X size={18}/></button></header>
  <form className="history-search-form" onSubmit={event => { event.preventDefault();void load(); }}><label className="history-query"><Search size={18}/><input ref={input} aria-label="搜索历史内容" disabled={!!opening} maxLength={100} value={query} onChange={event => setQuery(event.target.value)} placeholder="例如：上下文压缩、接口报错…"/></label><div className="history-filters"><select aria-label="历史搜索工程" value={project} disabled={!!opening} onChange={event => setProject(event.target.value)}><option value="">所有工程</option>{projects.map(value => <option key={value.id} value={value.id}>{value.name}{value.id === currentProjectId ? "（当前）" : ""}</option>)}</select><select aria-label="历史搜索类型" value={kind} disabled={!!opening} onChange={event => setKind(event.target.value)}><option value="ALL">全部内容</option><option value="CONVERSATION">对话标题</option><option value="MESSAGE">消息正文</option><option value="TASK">任务与结果</option></select><button type="submit" disabled={busy || !!opening}>{busy ? <Loader2 size={14}/> : <Search size={14}/>}搜索</button></div></form>
  <div className="history-search-results" aria-busy={busy}>
   {error && <p className="history-error" role="alert">{error}<button type="button" onClick={() => void load()}>重新搜索</button></p>}
   {items.map(hit => <button className="history-hit" key={`${hit.kind}:${hit.id}`} disabled={!!opening} onClick={() => void open(hit)}>
    <span className="history-hit-icon">{hit.kind === "TASK" ? <FileText size={17}/> : <MessageSquare size={17}/>}</span><span className="history-hit-body"><strong>{hit.conversationTitle === "Legacy" ? "默认对话" : hit.conversationTitle}</strong><span className="history-hit-excerpt">{hit.excerpt || "暂无正文"}</span><small>{hit.projectName} · {labels[hit.kind]}{hit.role ? hit.role === "user" ? "（你）" : "（助手）" : ""} · {new Date(hit.createdAt).toLocaleString()}</small></span>{opening === `${hit.kind}:${hit.id}` ? <Loader2 size={16}/> : <ArrowUpRight size={16}/>}</button>)}
   {!items.length && !error && <p className="history-empty" role="status">{busy ? "正在搜索历史…" : query.trim() ? "没有找到匹配内容，试试更短的关键词。" : "还没有可查看的历史记录。"}</p>}
   {cursor && <button className="history-load-more" disabled={busy || !!opening} onClick={() => void load(cursor)}>{busy ? "加载中…" : "加载更多结果"}</button>}
  </div><footer>搜索不会改变聊天上下文。需要复用内容时，在原对话中选择问答或保存上下文包。</footer>
 </section></div>, document.body);
}
