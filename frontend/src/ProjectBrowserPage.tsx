import { useEffect, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, Code2, ExternalLink, Globe, Hand, Loader2, MousePointer2, RefreshCw, Send, X } from "lucide-react";
import { desktopApp } from "./api";
import type { BrowserState, Project } from "./types";

export function ProjectBrowserPage({ project, embedded = false, active = true, openRequest }: { project: Project; embedded?: boolean; active?: boolean; openRequest?: { url: string; revision: number } }) {
  const [url, setURL] = useState("http://127.0.0.1:3000"), [state, setState] = useState<BrowserState | null>(null);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [text, setText] = useState(""), [panel, setPanel] = useState("console");
  const lock = useRef(false), generation = useRef(0);
  const handle = project.localHandle, native = !!handle && !!desktopApp()?.ProjectBrowser;
  const commit = (next: BrowserState | null) => { setState(next); if (next?.url) setURL(next.url); };
  useEffect(() => { commit(null); setError(""); }, [project.id, handle]);
  useEffect(() => {
    const current = ++generation.current;
    setBusy(false);
    if (!native || !active) return;
    const inspect = async () => {
      if (lock.current || document.hidden) return;
      lock.current = true;
      try {
        await desktopApp()?.BindProjectBrowser?.(project.id, handle!);
        const next = await desktopApp()!.ProjectBrowser!(handle!, "inspect", "", 0, 0);
        if (current === generation.current) commit(next);
      } catch { /* Opening the first URL creates the project session. */ }
      finally { lock.current = false; }
    };
    void inspect(); const timer = setInterval(() => void inspect(), 2000);
    return () => { generation.current++; clearInterval(timer); };
  }, [project.id, handle, native, active]);
  const action = async (name: string, value = "", x = 0, y = 0) => {
    if (!native || lock.current) return;
    const current = generation.current; lock.current = true; setBusy(true); setError("");
    try {
      await desktopApp()?.BindProjectBrowser?.(project.id, handle!);
      const next = await desktopApp()!.ProjectBrowser!(handle!, name, value, x, y);
      if (current === generation.current) { commit(name === "close" ? null : next); if (name === "type") setText(""); }
    } catch (cause) { if (current === generation.current) setError(String(cause)); }
    finally { lock.current = false; if (current === generation.current) setBusy(false); }
  };
  useEffect(() => {
    if (!openRequest) return;
    setURL(openRequest.url);
    if (!native) return;
    let timer: ReturnType<typeof setTimeout>;
    const open = () => { if (lock.current) timer = setTimeout(open, 100); else void action("open", openRequest.url); };
    open(); return () => clearTimeout(timer);
  }, [openRequest, native]);
  return <section className="developer-page shared-browser-page">
    <header className="developer-heading"><div><h1><Globe size={embedded ? 15 : 22}/> 项目浏览器</h1><p>{project.name}</p></div><div className="browser-session-actions">
      {state && <span className={`browser-control-state ${state.owner === "human" ? "human" : "agent"}`}><span/>{state.owner === "human" ? "人工控制" : "AI 可操作"}</span>}
      <button className="secondary-button" disabled={!state || busy} onClick={() => void action(state?.owner === "human" ? "release" : "takeover")}><Hand size={15}/>{state?.owner === "human" ? "交还 AI" : "接管"}</button>
      <button className="icon-button" title="打开同一浏览器窗口并接管" disabled={!state || busy} onClick={() => void action("show")}><ExternalLink size={17}/></button>
      <button className="icon-button" title="检查此页面的 Chromium DevTools" disabled={!state || busy} onClick={() => void action("devtools")}><Code2 size={17}/></button>
    </div></header>
    <form className="browser-address-bar" onSubmit={event => { event.preventDefault(); void action("open", url); }}>
      <button type="button" className="icon-button" title="后退" disabled={!state || busy} onClick={() => void action("back")}><ArrowLeft size={17}/></button>
      <button type="button" className="icon-button" title="前进" disabled={!state || busy} onClick={() => void action("forward")}><ArrowRight size={17}/></button>
      <button type="button" className="icon-button" title="刷新" disabled={!state || busy} onClick={() => void action("refresh")}><RefreshCw size={16}/></button>
      <label><Globe size={15}/><input aria-label="浏览器地址" value={url} onChange={event => setURL(event.target.value)} placeholder="http://127.0.0.1:3000"/></label>
      <button className="icon-button" title="访问地址" disabled={!native || busy}>{busy ? <Loader2 size={17} className="spinning"/> : <ArrowRight size={17}/>}</button>
      {state && <button type="button" className="icon-button" title="关闭会话" disabled={busy} onClick={() => void action("close")}><X size={16}/></button>}
    </form>
    {error && <div className="data-error" role="alert">{error}</div>}
    {!state ? <div className="browser-start"><Globe size={36}/><h2>{native ? "打开开发页面" : "桌面浏览器未连接"}</h2><p>{native ? "输入开发服务器地址" : "请在桌面端打开当前工程"}</p></div> : <>
      <div className="browser-tab-strip"><Globe size={14}/><strong>{state.title || "未命名页面"}</strong><span>{state.width} × {state.height}</span></div>
      <div className="browser-viewport" tabIndex={0} aria-label="项目网页" onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); void action("enter"); } }}>
        {state.image && <img draggable={false} src={state.image} alt={state.title || "项目页面"} onClick={event => { const rect = event.currentTarget.getBoundingClientRect(); void action("click", "", Math.floor((event.clientX - rect.left) * state.width / rect.width), Math.floor((event.clientY - rect.top) * state.height / rect.height)); }}/>}
      </div>
      <div className="browser-input"><MousePointer2 size={15}/><input aria-label="页面文字输入" value={text} onChange={event => setText(event.target.value)} placeholder="文字输入" onKeyDown={event => { if (event.key === "Enter") void action("type", text); }}/><button className="icon-button" title="输入到选中的网页控件" disabled={busy || !text} onClick={() => void action("type", text)}><Send size={16}/></button><button className="text-button" disabled={busy} onClick={() => void action("scroll", "", 0, -600)}>上滚</button><button className="text-button" disabled={busy} onClick={() => void action("scroll", "", 0, 600)}>下滚</button></div>
      <div className="browser-observation"><div className="browser-observation-tabs" role="tablist">{[["console", `控制台 (${state.console.length})`], ["elements", `页面控件 (${state.elements?.length || 0})`], ["text", "页面文本"]].map(([key, label]) => <button key={key} role="tab" aria-selected={panel === key} className={panel === key ? "active" : ""} onClick={() => setPanel(key)}>{label}</button>)}</div>
        {panel === "console" ? <div className="browser-console-lines">{state.console.length ? state.console.map((line, index) => <div key={index}><span>{index + 1}</span><code>{line}</code></div>) : <p className="browser-muted">暂无输出</p>}</div> : panel === "elements" ? <div className="browser-elements">{state.elements?.map(item => <div key={item.selector}><span>{item.role}</span><strong>{item.label || "未命名"}</strong><code>{item.selector}</code></div>)}</div> : <pre className="browser-page-text">{state.text || "暂无文本"}</pre>}
      </div>
    </>}
  </section>;
}
