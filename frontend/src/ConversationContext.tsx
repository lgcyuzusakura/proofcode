import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Archive, Check, ChevronDown, Eye, EyeOff, GitBranch, Layers3, RefreshCw, RotateCcw, Search, X } from "lucide-react";
import { api, post } from "./api";
import type { ContextItem, ContextPack, ContextPreview, ContextView, Conversation, ConversationMessage } from "./types";
import "./conversation-context.css";

export function useConversationContext(path: string, revision: string) {
  const [preview, setPreview] = useState<ContextPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [undo, setUndo] = useState<ContextView | null>(null);
  const generation = useRef(0);
  const saving = useRef(false);
  const reload = async () => {
    if (!path || saving.current) return;
    const request = ++generation.current;
    try { const value = await api<ContextPreview>(path); if (request === generation.current) { setPreview(value); setError(""); } }
    catch (cause) { if (request === generation.current) setError(String(cause)); }
  };
  useEffect(() => { setPreview(null); setUndo(null); void reload(); return () => { generation.current++; }; }, [path]);
  useEffect(() => { void reload(); }, [revision]);
  const save = async (view: ContextView, restoring = false) => {
    if (!path || !preview || saving.current) return;
    const previous = preview.view;
    const request = ++generation.current;
    saving.current = true;
    setBusy(true); setError("");
    setPreview({ ...preview, view });
    try {
      const value = await api<ContextPreview>(path, { method: "PATCH", body: JSON.stringify({ version: preview.version, view }) });
      if (request === generation.current) { setPreview(value); setUndo(restoring ? null : previous); }
    } catch (cause) { if (request === generation.current) { setPreview(preview); setError(`${String(cause)}。刷新后再试。`); } }
    finally { saving.current = false; setBusy(false); }
  };
  const state = (id: string) => preview?.view.mode === "AUTO" ? "INCLUDE" : preview?.view.items.find(item => item.messageId === id)?.state || "EXCLUDE";
  const edit = (ids: string[], next: ContextItem["state"], replacement?: string, allUserIds: string[] = []) => {
    if (!preview) return;
    const base: ContextItem[] = preview.view.mode === "AUTO" ? allUserIds.map(messageId => ({ messageId, state: "INCLUDE" })) : preview.view.items;
    const items = base.filter(item => !ids.includes(item.messageId));
    items.push(...ids.map(messageId => ({ messageId, state: next, replacement: next === "REPLACE" ? replacement : null })));
    void save({ ...preview.view, mode: "FOCUS", items });
  };
  return { preview, busy, error, undo, reload, save, state, edit };
}
export type ContextManager = ReturnType<typeof useConversationContext>;

export function ConversationContext({ manager, path, messages, prompt, running, onClose, onFork }: {
  manager: ContextManager; path: string; messages: ConversationMessage[]; prompt: string; running: boolean; onClose: () => void; onFork: (conversation: Conversation) => void;
}) {
  const { preview, busy, error, state, edit, save } = manager;
  const [query, setQuery] = useState("");
  const [summaryId, setSummaryId] = useState("");
  const [summary, setSummary] = useState("");
  const [title, setTitle] = useState("");
  const [packs, setPacks] = useState<ContextPack[]>([]);
  const [working, setWorking] = useState(false);
  const [localError, setLocalError] = useState("");
  const [feedback, setFeedback] = useState("");
  const [showInput, setShowInput] = useState(false);
  const [mobile, setMobile] = useState(() => window.matchMedia("(max-width:760px)").matches);
  useEffect(() => { const media = window.matchMedia("(max-width:760px)"); const update = () => setMobile(media.matches); media.addEventListener("change", update); return () => media.removeEventListener("change", update); }, []);
  const root = useRef<HTMLElement>(null);
  const turns = messages.filter(message => message.role === "user");
  const manual = preview?.view.mode === "FOCUS";
  const selected = manual ? turns.filter(turn => state(turn.id) !== "EXCLUDE").map(turn => turn.id) : [];
  const filtered = turns.filter(message => !query || messages.filter(item => item.taskId === message.taskId && item.attempt === message.attempt).some(item => item.content.toLowerCase().includes(query.toLowerCase())));
  const workspacePath = path.slice(0, path.lastIndexOf("/conversations/"));
  const loadPacks = () => { void api<ContextPack[]>(`${workspacePath}/context-packs`).then(setPacks).catch(cause => setLocalError(String(cause))); };
  useEffect(() => { loadPacks(); root.current?.querySelector<HTMLButtonElement>("button")?.focus(); }, [path]);
  useEffect(() => {
    const keys = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); onClose(); }
      if (event.key !== "Tab" || !window.matchMedia("(max-width:760px)").matches) return;
      const nodes = root.current?.querySelectorAll<HTMLElement>('button:not(:disabled), input, textarea, select, summary');
      if (!nodes?.length) return;
      const first = nodes[0], last = nodes[nodes.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    };
    window.addEventListener("keydown", keys); return () => window.removeEventListener("keydown", keys);
  }, [onClose]);
  const toggle = (id: string) => edit([id], state(id) === "EXCLUDE" ? "INCLUDE" : "EXCLUDE");
  const selectedInputs = () => preview?.messages.filter(input => {
    const original = messages.find(message => message.id === input.id);
    return original && selected.some(id => { const user = turns.find(turn => turn.id === id); return user?.taskId === original.taskId && user?.attempt === original.attempt; });
  }).map(input => input.id) || [];
  const transfer = async (fork: boolean) => {
    if (!preview || !title.trim()) return;
    setWorking(true); setLocalError(""); setFeedback("");
    try {
      const messageIds = selectedInputs();
      if (!messageIds.length) throw new Error("先选择至少一段已包含的对话");
      const value = await post<Conversation & { name?: string }>(`${path}/${fork ? "fork" : "packs"}`, { version: preview.version, title: title.trim(), messageIds });
      if (fork) onFork(value); else { setFeedback("上下文包已保存，可以在其他对话中带入。"); loadPacks(); }
    } catch (cause) { setLocalError(String(cause)); }
    finally { setWorking(false); }
  };
  const content = <aside className="conversation-context" ref={root} aria-label="对话上下文" role={mobile ? "dialog" : "complementary"} aria-modal={mobile ? true : undefined}>
    <div className="context-panel-head"><div><Layers3 size={17}/><strong>上下文</strong></div><button className="icon-button" aria-label="关闭上下文" onClick={onClose}><X size={17}/></button></div>
    <div className="context-panel-scroll">
      <p className="context-intro">选择下次任务要读的对话。聊天记录始终保留。{running && "当前任务已固定上下文，更改从下一次任务生效。"}后续回复也可能提及旧话题，可以搜索后一并排除。</p>
      {(error || localError) && <div role="alert" className="context-error">{error || localError}<button className="text-button" onClick={() => { void manager.reload(); loadPacks(); }}><RefreshCw size={12}/>刷新</button></div>}
      {feedback && <p role="status" className="context-feedback"><Check size={13}/>{feedback}</p>}
      {!preview ? <p className="context-intro">{error ? "无法读取上下文，请刷新。" : "正在读取上下文…"}</p> : <>
        <div className="context-mode" role="group" aria-label="上下文模式"><button disabled={busy} aria-pressed={preview.view.mode === "AUTO"} onClick={() => void save({ ...preview.view, mode: "AUTO" })}>自动</button><button disabled={busy} aria-pressed={preview.view.mode === "FOCUS"} onClick={() => void save({ ...preview.view, mode: "FOCUS" })}>手动</button></div>
        <p className="context-mode-help">{preview.view.mode === "AUTO" ? "和普通对话一样自动使用历史，手动选择暂不生效。" : "只使用勾选的问答和已带入的上下文包。取消勾选即可排除。"}</p>
        <div className="context-totals"><span><strong>{preview.messages.length}</strong> 条输入</span><span><strong>{preview.excludedTurns}</strong> 段排除</span><span title={preview.estimateMethod}>估算约 {preview.estimatedTokens.toLocaleString()} tokens</span></div>
        <p className="context-estimate-note">历史内容估算，未含系统规则、工具和当前问题；实际输入还受模型预算约束。</p>
        <div className="context-utility"><button className="text-button" onClick={() => setShowInput(value => !value)}><Eye size={13}/>{showInput ? "收起输入预览" : "预览历史输入"}</button><button className="text-button" disabled={!manager.undo || busy} onClick={() => manager.undo && void save(manager.undo, true)}><RotateCcw size={13}/>撤销上次修改</button></div>
        {showInput && <section className="context-input-preview" aria-label="历史输入预览"><small>版本 {preview.version} / {preview.selectionHash.slice(0, 10)}</small>{preview.messages.map((input, index) => <details key={`${input.sourceConversationId}:${input.id}:${index}`}><summary>{input.role === "user" ? "你" : "ProofCode"} · {input.content.slice(0, 65)}</summary><pre>{input.content}</pre><small>来源 {input.sourceConversationId.slice(0, 8)} / {input.sourceHash.slice(0, 8)}</small></details>)}{!preview.messages.length && <p>本次不携带历史对话。</p>}{prompt && <div className="context-current-prompt"><strong>当前问题（另行发送）</strong><pre>{prompt}</pre></div>}</section>}
        <label className="context-search"><Search size={14}/><input aria-label="搜索上下文问答" placeholder="搜索要保留或排除的内容" value={query} onChange={event => setQuery(event.target.value)}/></label>
        {manual && <div className="context-batch"><label><input type="checkbox" disabled={busy} aria-label="包含搜索结果" checked={!!filtered.length && filtered.every(turn => selected.includes(turn.id))} onChange={event => edit(filtered.map(turn => turn.id), event.target.checked ? "INCLUDE" : "EXCLUDE")}/>已选 {selected.length} 段</label><button disabled={!filtered.length || busy} onClick={() => edit(filtered.map(turn => turn.id), "INCLUDE")}>全选结果</button><button disabled={!selected.length || busy} onClick={() => edit(selected, "EXCLUDE")}>清空选择</button></div>}
        <div className="context-turns">{filtered.map(turn => { const current = state(turn.id); const answer = messages.find(item => item.role === "assistant" && item.taskId === turn.taskId && item.attempt === turn.attempt); return <article className={`context-turn ${current.toLowerCase()}`} key={turn.id}><div className="context-turn-top"><label>{manual && <input type="checkbox" disabled={busy} checked={selected.includes(turn.id)} onChange={() => toggle(turn.id)} aria-label={`包含问答：${turn.content.slice(0, 60)}`}/>}<time>{new Date(turn.createdAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</time></label><span className="context-state">{!manual ? "自动纳入" : current === "EXCLUDE" ? "已排除" : current === "REPLACE" ? "使用摘要" : "已包含"}</span></div><p>{turn.content}</p>{answer?.content && <details><summary>查看回复 <ChevronDown size={12}/></summary><p>{answer.content}</p></details>}{manual && <div className="context-turn-actions"><button disabled={busy} onClick={() => edit([turn.id], current === "EXCLUDE" ? "INCLUDE" : "EXCLUDE")}>{current === "EXCLUDE" ? <Eye size={12}/> : <EyeOff size={12}/>} {current === "EXCLUDE" ? "恢复问答" : "排除问答"}</button><button disabled={busy} onClick={() => { setSummaryId(turn.id); setSummary(preview.view.items.find(item => item.messageId === turn.id)?.replacement || ""); }}>写摘要</button>{current === "REPLACE" && <button disabled={busy} onClick={() => edit([turn.id], "INCLUDE")}>恢复原文</button>}</div>}{summaryId === turn.id && <div className="context-summary-editor"><label>用短摘要替换这段问答<textarea aria-label="上下文摘要" maxLength={16000} value={summary} onChange={event => setSummary(event.target.value)} placeholder="只留下需要延续的约束、结论或进度…"/></label><div><button disabled={busy || !summary.trim()} onClick={() => { edit([turn.id], "REPLACE", summary); setSummaryId(""); }}>保存摘要</button><button onClick={() => setSummaryId("")}>取消</button></div></div>}</article>; })}{!filtered.length && <p className="context-intro">{query ? "没有匹配的问答。" : "还没有历史问答，可以带入已保存的上下文包。"}</p>}</div>
        <section className="context-pack-list"><h3>可带入的上下文包</h3>{packs.map(pack => { const mounted = preview.view.packIds.includes(pack.id); return <div className="context-pack" key={pack.id}><Archive size={14}/><div><strong>{pack.name}</strong><small>{pack.messageCount} 条 · 来源 {pack.sourceConversationId.slice(0, 8)}{pack.stale ? " · 来源已修改" : ""}</small></div><button disabled={busy || (pack.stale && !mounted)} onClick={() => void save({ ...preview.view, packIds: mounted ? preview.view.packIds.filter(id => id !== pack.id) : [...preview.view.packIds, pack.id] })}>{mounted ? "卸载" : "带入"}</button></div>; })}{!packs.length && <p className="context-intro">选中问答后保存，便可在同一工作区的其他对话复用。</p>}</section>
      </>}
    </div>
    <div className="context-panel-footer"><label>带走选中的内容<input aria-label="上下文包名称" maxLength={120} value={title} onChange={event => setTitle(event.target.value)} placeholder="例如：登录模块约束"/></label><div><button className="secondary-button" disabled={busy || working || !manual || !preview || !selected.length || !title.trim()} onClick={() => void transfer(false)}><Archive size={14}/>保存为包</button><button className="primary-button" disabled={busy || working || !manual || !preview || !selected.length || !title.trim()} onClick={() => void transfer(true)}><GitBranch size={14}/>带入新对话</button></div><small>切换手动后勾选要带走的内容，不迁移服务商缓存。</small></div>
  </aside>;
  return mobile ? createPortal(<><button className="context-mobile-scrim" aria-label="关闭上下文面板" onClick={onClose}/>{content}</>, document.body) : content;
}
