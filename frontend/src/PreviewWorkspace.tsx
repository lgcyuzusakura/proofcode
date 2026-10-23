import { lazy, Suspense, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Download, FileText, FolderOpen, Globe, Maximize2, Minimize2, PanelRightClose, X } from "lucide-react";
import { ProjectBrowserPage } from "./ProjectBrowserPage";
import type { Project } from "./types";
import "./preview-workspace.css";

const DocumentPreview = lazy(() => import("./DocumentPreview"));
export type PreviewRequest = { revision: number; file?: File; url?: string };
type DocumentTab = { id: string; file: File };
const maxFileBytes = 32 * 1024 * 1024;

export function PreviewWorkspace({ project, visible, open, onOpenChange, request, children }: {
  project: Project; visible: boolean; open: boolean; onOpenChange: (value: boolean) => void;
  request: PreviewRequest | null; children: ReactNode;
}) {
  const [tabs, setTabs] = useState<DocumentTab[]>([]), [active, setActive] = useState("");
  const [browserOpened, setBrowserOpened] = useState(false), [browserURL, setBrowserURL] = useState<{ url: string; revision: number }>();
  const [fullscreen, setFullscreen] = useState(false), [error, setError] = useState("");
  const [width, setWidth] = useState(50);
  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);
  const root = useRef<HTMLDivElement>(null), pane = useRef<HTMLElement>(null), picker = useRef<HTMLInputElement>(null);
  const restoreFocus = useRef<HTMLElement | null>(null), serial = useRef(0);
  const shown = open && visible;
  const current = tabs.find(tab => tab.id === active);
  const addFiles = (files: File[]) => {
    let next = [...tabs], selected = "", problem = "";
    for (const file of files) {
      if (!/\.(md|markdown|txt|pdf|docx)$/i.test(file.name)) { problem = "支持 Markdown、TXT、PDF 和 Word .docx；旧版 .doc 请先另存为 .docx。"; continue; }
      if (!file.size || file.size > maxFileBytes) { problem = "请选择非空且不超过 32 MB 的文件。"; continue; }
      if (/\.(md|markdown|txt)$/i.test(file.name) && file.size > 2 * 1024 * 1024) { problem = "文本预览支持不超过 2 MB 的文件，请先拆分较大的文档。"; continue; }
      if (next.length >= 8 || next.reduce((sum, tab) => sum + tab.file.size, 0) + file.size > 96 * 1024 * 1024) { problem = "最多打开 8 份文档、合计 96 MB，请先关闭部分标签。"; break; }
      selected = `document-${++serial.current}`; next.push({ id: selected, file });
    }
    setTabs(next); setError(problem);
    if (selected) setActive(selected);
  };
  const browse = (url?: string) => {
    setBrowserOpened(true); setActive("browser"); setError("");
    if (url) setBrowserURL({ url, revision: Date.now() });
  };
  useEffect(() => {
    if (!request) return;
    if (request.file) addFiles([request.file]); else browse(request.url);
  }, [request]);
  useEffect(() => { if (!shown) { setFullscreen(false); setDragging(false); dragDepth.current = 0; } }, [shown]);
  useEffect(() => {
    if (!fullscreen) return;
    restoreFocus.current = document.activeElement as HTMLElement;
    pane.current?.querySelector<HTMLElement>('[title="退出全屏"]')?.focus();
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); setFullscreen(false); }
      if (event.key !== "Tab") return;
      const nodes = [...(pane.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],input:not(:disabled),iframe,[tabindex="0"]') || [])].filter(node => node.getClientRects().length);
      const first = nodes[0], last = nodes[nodes.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    };
    window.addEventListener("keydown", key);
    return () => { window.removeEventListener("keydown", key); restoreFocus.current?.isConnected && restoreFocus.current.focus({ preventScroll: true }); };
  }, [fullscreen]);
  const close = (id: string) => {
    const remaining = tabs.filter(tab => tab.id !== id); setTabs(remaining);
    if (id === active) setActive(remaining.slice(-1)[0]?.id || (browserOpened ? "browser" : ""));
  };
  const resize = (clientX: number) => {
    const rect = root.current?.getBoundingClientRect(); if (!rect) return;
    setWidth(Math.min(68, Math.max(32, (rect.right - clientX) / rect.width * 100)));
  };
  return <div ref={root} className={`preview-workspace ${visible ? "is-chat" : ""} ${shown ? "has-preview" : ""} ${fullscreen ? "preview-expanded" : ""}`} style={{ "--preview-width": `${width}%` } as CSSProperties}>
    <div className="workspace-primary" inert={fullscreen || undefined}>{children}</div>
    <div className="preview-divider" hidden={!shown} role="separator" aria-label="调整对话与预览宽度" aria-orientation="vertical" aria-valuemin={32} aria-valuemax={68} aria-valuenow={Math.round(width)} tabIndex={0}
      onDoubleClick={() => setWidth(50)} onKeyDown={event => { if (["ArrowLeft", "ArrowRight", "Home"].includes(event.key)) { event.preventDefault(); setWidth(value => event.key === "Home" ? 50 : Math.min(68, Math.max(32, value + (event.key === "ArrowLeft" ? 2 : -2)))); } }}
      onPointerDown={event => { event.currentTarget.setPointerCapture(event.pointerId); event.preventDefault(); }} onPointerMove={event => { if (event.currentTarget.hasPointerCapture(event.pointerId)) resize(event.clientX); }} onPointerUp={event => { if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); }}/>
    <aside ref={pane} hidden={!shown} className={`preview-pane ${dragging ? "is-file-dragging" : ""}`} aria-label="文档与浏览器预览" role={fullscreen ? "dialog" : "complementary"} aria-modal={fullscreen || undefined}
      onDragEnter={event => { if (event.dataTransfer.types.includes("Files")) { event.preventDefault(); dragDepth.current++; setDragging(true); } }}
      onDragOver={event => { if (event.dataTransfer.types.includes("Files")) { event.preventDefault(); event.dataTransfer.dropEffect = "copy"; } }}
      onDragLeave={event => { if (event.dataTransfer.types.includes("Files") && --dragDepth.current <= 0) { dragDepth.current = 0; setDragging(false); } }}
      onDrop={event => {
        if (!event.dataTransfer.types.includes("Files")) return;
        event.preventDefault(); event.stopPropagation(); dragDepth.current = 0; setDragging(false);
        addFiles([...event.dataTransfer.files]);
      }}>
      {dragging && <div className="preview-drop-target" role="status"><FolderOpen size={28}/><strong>松开文件，在此预览</strong><span>仅在本机查看，不发送给模型</span></div>}
      <header className="preview-header"><div className="preview-tabs" role="tablist" aria-label="预览标签" onKeyDown={event => {
        if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key) || (event.target as HTMLElement).getAttribute("role") !== "tab") return;
        const nodes = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]')], index = nodes.indexOf(event.target as HTMLButtonElement);
        event.preventDefault(); const next = event.key === "Home" ? 0 : event.key === "End" ? nodes.length - 1 : (index + (event.key === "ArrowRight" ? 1 : -1) + nodes.length) % nodes.length;
        nodes[next]?.click(); nodes[next]?.focus();
      }}>
        {browserOpened && <div className={`preview-document-tab ${active === "browser" ? "is-active" : ""}`}><button id="preview-tab-browser" role="tab" tabIndex={active === "browser" ? 0 : -1} aria-controls="preview-browser-content" aria-selected={active === "browser"} onClick={() => setActive("browser")}><Globe size={14}/><span>浏览器</span></button><button className="preview-tab-close" title="关闭浏览器标签" onClick={() => { setBrowserOpened(false); if (active === "browser") setActive(tabs.slice(-1)[0]?.id || ""); }}><X size={12}/></button></div>}
        {tabs.map(tab => <div className={`preview-document-tab ${active === tab.id ? "is-active" : ""}`} key={tab.id}><button id={`preview-tab-${tab.id}`} role="tab" tabIndex={active === tab.id ? 0 : -1} aria-controls={`preview-content-${tab.id}`} aria-selected={active === tab.id} title={tab.file.name} onClick={() => setActive(tab.id)}><FileText size={14}/><span>{tab.file.name}</span></button><button className="preview-tab-close" title={`关闭 ${tab.file.name}`} onClick={() => close(tab.id)}><X size={12}/></button></div>)}
        {!browserOpened && !tabs.length && <span className="preview-heading">预览</span>}
      </div><div className="preview-header-actions"><button className="icon-button" title="打开文档" onClick={() => picker.current?.click()}><FolderOpen size={16}/></button><button className="icon-button" title={fullscreen ? "退出全屏" : "全屏预览"} aria-pressed={fullscreen} onClick={() => setFullscreen(value => !value)}>{fullscreen ? <Minimize2 size={16}/> : <Maximize2 size={16}/>}</button><button className="icon-button" title="收起预览" onClick={() => onOpenChange(false)}><PanelRightClose size={16}/></button></div></header>
      <input ref={picker} className="preview-file-picker" type="file" multiple accept=".md,.markdown,.txt,.pdf,.docx" aria-label="选择预览文档" onChange={event => { addFiles([...(event.currentTarget.files || [])]); event.currentTarget.value = ""; }}/>
      {error && <div className="preview-error" role="alert">{error}<button className="icon-button" title="关闭文档提示" onClick={() => setError("")}><X size={14}/></button></div>}
      {browserOpened && <div id="preview-browser-content" role="tabpanel" aria-labelledby="preview-tab-browser" className="preview-browser" hidden={active !== "browser"}><ProjectBrowserPage project={project} embedded active={shown && active === "browser"} openRequest={browserURL}/></div>}
      {tabs.map(tab => <div key={tab.id} hidden={active !== tab.id} className="preview-document-content" role="tabpanel" id={`preview-content-${tab.id}`} aria-labelledby={`preview-tab-${tab.id}`}><Suspense fallback={<div className="preview-status" role="status">正在加载文档预览…</div>}><DocumentPreview file={tab.file} onBrowse={browse}/></Suspense></div>)}
      {!active && <div className="preview-empty"><div className="preview-empty-icon"><FileText size={30}/></div><h2>边聊，边看。</h2><p>拖入文档，或打开开发页面，<br/>与对话并排查看。</p><button className="secondary-button" onClick={() => picker.current?.click()}><FolderOpen size={15}/>打开文档</button><button className="text-button" onClick={() => browse()}><Globe size={15}/>打开项目浏览器</button><small>Markdown · PDF · Word .docx<br/>本机预览，不自动加入上下文</small></div>}
      {current && <footer className="preview-file-footer"><span>{current.file.name} <small>{(current.file.size / 1024).toFixed(1)} KB</small></span><button className="text-button" title="保存文档副本" onClick={() => { const url = URL.createObjectURL(current.file); const link = document.createElement("a"); link.href = url; link.download = current.file.name; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); }}><Download size={14}/>保存副本</button></footer>}
    </aside>
  </div>;
}
