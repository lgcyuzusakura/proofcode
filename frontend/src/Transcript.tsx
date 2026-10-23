import { ReactNode, useLayoutEffect, useRef, useState } from "react";
import { ArrowDown, ArrowUp, ChevronDown, ChevronUp, ChevronsDown, Search, X } from "lucide-react";

// Preserve a reader's place when switching between tasks in this window.
const positions = new Map<string, { top: number; pinned: boolean }>();
const bottomThreshold = 48;

export type MessageLocation = { id: string; revision: number };

export function Transcript({ sessionKey, children, location }: { sessionKey: string; children: ReactNode; location?: MessageLocation }) {
  const viewport = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  const restoring = useRef(false);
  const cancelRestore = useRef(() => {});
  const revealed = useRef("");
  const highlight = useRef<{ node: HTMLElement; timer: ReturnType<typeof setTimeout> } | null>(null);
  const [edges, setEdges] = useState({ top: true, bottom: true });
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const matches = useRef<HTMLElement[]>([]);
  const [matchState, setMatchState] = useState({ index: 0, count: 0 });

  const remember = () => {
    const node = viewport.current;
    if (!node || restoring.current) return;
    const bottom = node.scrollHeight - node.clientHeight - node.scrollTop <= bottomThreshold;
    pinned.current = bottom;
    setEdges((old) => {
      const top = node.scrollTop <= 2;
      return old.top === top && old.bottom === bottom ? old : { top, bottom };
    });
    positions.set(sessionKey, { top: node.scrollTop, pinned: bottom });
    if (positions.size > 100) positions.delete(positions.keys().next().value!);
  };

  useLayoutEffect(() => {
    const node = viewport.current;
    const body = content.current;
    if (!node || !body) return;
    const saved = positions.get(sessionKey);
    pinned.current = saved?.pinned ?? true;
    // History may arrive after the task selection. Keep its saved offset until
    // there is enough content to restore it, rather than overwriting it with 0.
    let restoreTop = saved && !saved.pinned ? saved.top : null;
    restoring.current = restoreTop !== null;
    const resize = () => {
      if (restoreTop !== null) {
        node.scrollTop = restoreTop;
        if (node.scrollHeight > node.clientHeight && node.scrollHeight - node.clientHeight >= restoreTop) {
          restoreTop = null;
          restoring.current = false;
        }
      } else if (pinned.current) {
        node.scrollTop = node.scrollHeight;
      }
      if (restoreTop === null) remember();
    };
    resize();
    const observer = new ResizeObserver(resize);
    observer.observe(body);
    observer.observe(node);
    const cancel = () => { restoreTop = null; restoring.current = false; };
    cancelRestore.current = cancel;
    node.addEventListener("wheel", cancel, { passive: true });
    node.addEventListener("pointerdown", cancel);
    node.addEventListener("keydown", cancel);
    return () => {
      observer.disconnect();
      node.removeEventListener("wheel", cancel);
      node.removeEventListener("pointerdown", cancel);
      node.removeEventListener("keydown", cancel);
    };
  }, [sessionKey]);

  const go = (end: "top" | "bottom") => {
    const node = viewport.current;
    if (!node) return;
    cancelRestore.current();
    pinned.current = end === "bottom";
    node.scrollTop = end === "bottom" ? node.scrollHeight : 0;
    remember();
    node.focus({ preventScroll: true });
  };

  const find = (index: number) => {
    const node = viewport.current;
    const match = matches.current[index];
    if (!node || !match) return;
    cancelRestore.current();
    pinned.current = false;
    node.scrollTop += match.getBoundingClientRect().top - node.getBoundingClientRect().top - 12;
    setMatchState(old => ({ ...old, index }));
    remember();
  };

  useLayoutEffect(() => {
    const key = `${sessionKey}:${location?.revision}:${location?.id}`;
    if (!location?.id || revealed.current === key) return;
    const node = viewport.current;
    const target = content.current?.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(location.id)}"]`);
    if (!node || !target) return;
    // Source navigation supersedes both a saved position and follow-to-bottom.
    // Scroll only this viewport: scrollIntoView could move the entire workbench.
    cancelRestore.current();pinned.current = false;revealed.current = key;
    node.scrollTop += target.getBoundingClientRect().top - node.getBoundingClientRect().top - Math.max(12, (node.clientHeight - target.offsetHeight) / 2);
    remember();target.tabIndex = -1;target.focus({ preventScroll: true });
    if (highlight.current) { clearTimeout(highlight.current.timer);delete highlight.current.node.dataset.searchHighlight; }
    target.dataset.searchHighlight = "true";
    highlight.current = { node: target, timer: setTimeout(() => { delete target.dataset.searchHighlight;highlight.current = null; }, 5000) };
  }, [location?.id, location?.revision, sessionKey, children]);

  useLayoutEffect(() => () => {
    if (highlight.current) { clearTimeout(highlight.current.timer);delete highlight.current.node.dataset.searchHighlight;highlight.current = null; }
  }, [sessionKey]);

  useLayoutEffect(() => {
    const rows = Array.from(content.current?.querySelectorAll<HTMLElement>(".message-row") || []);
    const needle = query.trim().toLocaleLowerCase();
    matches.current = rows.filter(row => needle && row.textContent?.toLocaleLowerCase().includes(needle));
    rows.forEach(row => row.classList.toggle("search-match", matches.current.includes(row)));
    setMatchState({ index: 0, count: matches.current.length });
    if (matches.current.length) find(0);
    return () => rows.forEach(row => row.classList.remove("search-match"));
  }, [query, sessionKey]);

  return <div className="transcript-shell">
    <div className="transcript-tools" aria-label="聊天滚动控制">
      <span>对话记录</span>
      <div>
        <button type="button" title="搜索聊天内容" aria-label="搜索聊天内容" aria-expanded={searching} onClick={() => { setSearching(value => !value); setQuery(""); }}><Search size={14}/></button>
        <button type="button" onClick={() => go("top")} disabled={edges.top} title="滚动到最早消息" aria-label="滚动到最早消息"><ArrowUp size={14}/></button>
        <button type="button" onClick={() => go("bottom")} disabled={edges.bottom} title="滚动到最新消息" aria-label="滚动到最新消息"><ArrowDown size={14}/></button>
      </div>
    </div>
    {searching && <div className="transcript-search"><Search size={14}/><input autoFocus aria-label="查找聊天内容" value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === "Escape") { setSearching(false); setQuery(""); } else if (event.key === "Enter" && matchState.count) { event.preventDefault(); find((matchState.index + (event.shiftKey ? -1 : 1) + matchState.count) % matchState.count); } }} placeholder="搜索这段对话…"/><span>{matchState.count ? `${matchState.index + 1} / ${matchState.count}` : query ? "无匹配" : ""}</span><button type="button" aria-label="上一个匹配" disabled={!matchState.count} onClick={() => find((matchState.index - 1 + matchState.count) % matchState.count)}><ChevronUp size={14}/></button><button type="button" aria-label="下一个匹配" disabled={!matchState.count} onClick={() => find((matchState.index + 1) % matchState.count)}><ChevronDown size={14}/></button><button type="button" aria-label="关闭聊天搜索" onClick={() => { setSearching(false); setQuery(""); }}><X size={14}/></button></div>}
    <div className="conversation-stream" ref={viewport} tabIndex={0} role="region" aria-label="聊天记录，可上下滚动" onScroll={remember}>
      <div className="transcript-content" ref={content}>{children}</div>
    </div>
    {!edges.bottom && <button type="button" className="jump-to-latest" onClick={() => go("bottom")}><ChevronsDown size={15}/>回到最新消息</button>}
  </div>;
}
