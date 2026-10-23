import { useEffect, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, Minus, Plus, ScanLine } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

function PreviewError({ message }: { message: string }) { return <div className="preview-status preview-render-error" role="alert"><strong>无法预览此文档</strong><p>{message}</p><small>原文件没有修改，可以保存副本后用本机应用打开。</small></div>; }

export default function DocumentPreview({ file, onBrowse }: { file: File; onBrowse: (url: string) => void }) {
  const kind = file.name.split(".").slice(-1)[0]?.toLowerCase();
  if (kind === "pdf") return <PDFPreview file={file}/>;
  if (kind === "docx") return <WordPreview file={file}/>;
  return <MarkdownPreview file={file} plain={kind === "txt"} onBrowse={onBrowse}/>;
}

function MarkdownPreview({ file, plain, onBrowse }: { file: File; plain: boolean; onBrowse: (url: string) => void }) {
  const [text, setText] = useState<string | null>(null), [error, setError] = useState("");
  useEffect(() => { let disposed = false; void file.text().then(value => { if (!disposed) setText(value); }, () => { if (!disposed) setError("文件读取失败，请重新打开。"); }); return () => { disposed = true; }; }, [file]);
  if (error) return <PreviewError message={error}/>;
  if (text === null) return <div className="preview-status" role="status">正在读取文档…</div>;
  return <article className="preview-markdown" aria-label="Markdown 文档">{plain ? <pre>{text}</pre> : <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={{
    a: ({ href, children }) => /^https?:\/\//i.test(href || "") ? <a href={href} onClick={event => { event.preventDefault(); onBrowse(href!); }}>{children}</a> : <span>{children}</span>,
    img: ({ alt }) => <span className="preview-image-placeholder">[图片：{alt || "未命名"}]</span>,
  }}>{text}</ReactMarkdown>}</article>;
}

function WordPreview({ file }: { file: File }) {
  const [markup, setMarkup] = useState(""), [error, setError] = useState(""), [zoom, setZoom] = useState(1);
  const [fit, setFit] = useState(1), viewport = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let disposed = false;
    void (async () => {
      const [{ renderAsync }, { default: JSZip }] = await Promise.all([import("docx-preview"), import("jszip")]);
      if (disposed) return;
      const data = await file.arrayBuffer();
      const archive = await JSZip.loadAsync(data);
      if (disposed) return;
      const entries = Object.values(archive.files);
      // JSZip stores directory metadata without inflating each entry. Bound the
      // expanded document before docx-preview materializes its XML and media.
      let expanded = 0;
      for (const entry of entries) {
        const size = (entry as unknown as { _data?: { uncompressedSize?: number } })._data?.uncompressedSize || 0;
        expanded += size;
        if (size > 48 * 1024 * 1024 || expanded > 96 * 1024 * 1024 || entries.length > 4096) throw new Error("Expanded Word document exceeds preview limits");
      }
      const container = document.createElement("div");
      await renderAsync(data, container, undefined, { useBase64URL: true, renderAltChunks: false, renderComments: false, ignoreFonts: true });
      // The generated document lives in an opaque sandbox. Strip navigable and active
      // content too; relationship targets must never fetch external resources.
      container.querySelectorAll("script,iframe,object,embed,form").forEach(node => node.remove());
      container.querySelectorAll("*").forEach(node => {
        for (const attribute of [...node.attributes]) if (/^on/i.test(attribute.name) || ["href", "srcset", "target"].includes(attribute.name) || (attribute.name === "src" && !attribute.value.startsWith("data:"))) node.removeAttribute(attribute.name);
      });
      if (!disposed) setMarkup(container.innerHTML);
    })().catch(() => { if (!disposed) setError("Word 文件损坏、被加密或格式不受支持；请使用未加密的 .docx 文件。"); });
    return () => { disposed = true; };
  }, [file]);
  useEffect(() => {
    const node = viewport.current; if (!node) return;
    const update = () => setFit(Math.min(1.5, Math.max(.15, (node.clientWidth - 32) / 794)));
    const observer = new ResizeObserver(update); observer.observe(node); update(); return () => observer.disconnect();
  }, []);
  const scale = Math.round(fit * zoom * 100);
  return <><ZoomToolbar percent={scale} onZoom={delta => setZoom(value => Math.max(.5, Math.min(3, value + delta)))} onFit={() => setZoom(1)}><span>Word 预览</span></ZoomToolbar><div ref={viewport} className="preview-word-viewport">
    {error ? <PreviewError message={error}/> : markup ? <iframe title="Word 文档" sandbox="" srcDoc={`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; base-uri 'none'; form-action 'none'"><style>html{background:#e8eaed}body{margin:0} .docx-wrapper{padding:16px!important;min-height:100vh;zoom:${scale}%;background:transparent!important}.docx-wrapper>section.docx{margin:0 auto 20px!important;box-shadow:0 1px 8px #0002!important}a{color:inherit;pointer-events:none}</style></head><body>${markup}</body></html>`}/> : <div className="preview-status" role="status">正在排版 Word 文档…</div>}
  </div></>;
}

function ZoomToolbar({ percent, onZoom, onFit, children }: { percent: number; onZoom: (delta: number) => void; onFit: () => void; children: React.ReactNode }) {
  return <div className="preview-document-toolbar">{children}<div><button className="icon-button" title="缩小文档" onClick={() => onZoom(-.2)}><Minus size={15}/></button><span>{percent}%</span><button className="icon-button" title="放大文档" onClick={() => onZoom(.2)}><Plus size={15}/></button><button className="icon-button" title="适合宽度" onClick={onFit}><ScanLine size={16}/></button></div></div>;
}

function PDFPreview({ file }: { file: File }) {
  const [pdf, setPDF] = useState<import("pdfjs-dist").PDFDocumentProxy | null>(null), [error, setError] = useState("");
  const [page, setPage] = useState(1), [zoom, setZoom] = useState(1), [width, setWidth] = useState(0), [percent, setPercent] = useState(100), [rendering, setRendering] = useState(true);
  const [renderedKey, setRenderedKey] = useState("");
  const viewport = useRef<HTMLDivElement>(null), canvas = useRef<HTMLCanvasElement>(null), text = useRef<HTMLDivElement>(null);
  // Store the reading offset in PDF coordinates, independent of canvas scale.
  // Hidden tabs and cancelled resize renders must not replace it with zero.
  const reading = useRef({ page: 1, top: 0, left: 0, scale: 1, width: 0 });
  const updating = useRef(true);
  useEffect(() => {
    let disposed = false, loading: import("pdfjs-dist").PDFDocumentLoadingTask | undefined;
    void (async () => {
      const [engine, worker] = await Promise.all([import("pdfjs-dist"), import("pdfjs-dist/build/pdf.worker.min.mjs?url")]);
      if (disposed) return;
      engine.GlobalWorkerOptions.workerSrc = worker.default;
      const data = await file.arrayBuffer(); if (disposed) return;
      loading = engine.getDocument({ data, isEvalSupported: false, cMapUrl: new URL("pdfjs/cmaps/", document.baseURI).href, cMapPacked: true, standardFontDataUrl: new URL("pdfjs/standard_fonts/", document.baseURI).href, wasmUrl: new URL("pdfjs/wasm/", document.baseURI).href });
      const value = await loading.promise;
      if (!disposed) setPDF(value);
    })().catch(cause => { if (!disposed) setError(cause?.name === "PasswordException" ? "此 PDF 已加密，请先解密后重新打开。" : "PDF 无法读取，请确认文件完整且格式正确。"); });
    return () => { disposed = true; void loading?.destroy(); };
  }, [file]);
  useEffect(() => {
    const node = viewport.current; if (!node) return;
    const observer = new ResizeObserver(() => setWidth(node.clientWidth)); observer.observe(node); setWidth(node.clientWidth); return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (!pdf || !width || !canvas.current || !text.current) return;
    let disposed = false, task: import("pdfjs-dist").RenderTask | undefined, layer: import("pdfjs-dist").TextLayer | undefined;
    const target = canvas.current, textTarget = text.current;
    updating.current = true; setRendering(true); setError("");
    if (reading.current.page !== page) reading.current = { page, top: 0, left: 0, scale: 1, width: 0 };
    void (async () => {
      const value = await pdf.getPage(page); if (disposed) return;
      const base = value.getViewport({ scale: 1 });
      // Bound canvas allocation even for unusual page dimensions.
      const ratio = Math.min(2, window.devicePixelRatio || 1);
      const scale = Math.min(4, Math.max(1, width - 32) / base.width * zoom, 4096 / (Math.max(base.width, base.height) * ratio));
      const view = value.getViewport({ scale: Math.max(.00001, scale) });
      target.width = Math.floor(view.width * ratio); target.height = Math.floor(view.height * ratio);
      target.style.width = `${view.width}px`; target.style.height = `${view.height}px`;
      textTarget.replaceChildren(); textTarget.style.setProperty("--total-scale-factor", String(view.scale));
      textTarget.style.width = `${view.width}px`; textTarget.style.height = `${view.height}px`;
      setPercent(Math.round(view.scale * 100));
      task = value.render({ canvas: target, viewport: view, transform: ratio !== 1 ? [ratio, 0, 0, ratio, 0, 0] : undefined });
      await task.promise; if (disposed) return;
      const { TextLayer } = await import("pdfjs-dist"); if (disposed) return;
      layer = new TextLayer({ textContentSource: value.streamTextContent(), container: textTarget, viewport: view }); await layer.render();
      if (!disposed) {
        reading.current.scale = view.scale;
        reading.current.width = width;
        if (viewport.current) {
          viewport.current.scrollTop = reading.current.top * view.scale;
          viewport.current.scrollLeft = reading.current.left * view.scale;
        }
        updating.current = false; setRenderedKey(`${width}:${zoom}:${page}`); setRendering(false);
      }
    })().catch(cause => { if (!disposed && cause?.name !== "RenderingCancelledException") setError("此页渲染失败，请切换页面或重新打开 PDF。"); });
    return () => { disposed = true; task?.cancel(); layer?.cancel(); };
  }, [pdf, page, zoom, width]);
  return <><ZoomToolbar percent={percent} onZoom={delta => setZoom(value => Math.max(.4, Math.min(3, value + delta)))} onFit={() => setZoom(1)}><div className="preview-pagination"><button className="icon-button" title="上一页" disabled={!pdf || page === 1} onClick={() => setPage(value => value - 1)}><ChevronLeft size={16}/></button><label><input aria-label="PDF 页码" type="number" min={1} max={pdf?.numPages || 1} value={page} onChange={event => { const value = Number(event.target.value); if (Number.isInteger(value) && value >= 1 && value <= (pdf?.numPages || 1)) setPage(value); }}/><span>/ {pdf?.numPages || "–"}</span></label><button className="icon-button" title="下一页" disabled={!pdf || page >= pdf.numPages} onClick={() => setPage(value => value + 1)}><ChevronRight size={16}/></button></div></ZoomToolbar>
    <div ref={viewport} className="preview-pdf-viewport" tabIndex={0} aria-label="PDF 阅读区域" data-render-key={`${width}:${zoom}:${page}`} data-rendered-key={renderedKey} data-rendered-width={reading.current.width} onScroll={event => {
      if (updating.current || event.currentTarget.clientWidth !== reading.current.width || !event.currentTarget.clientWidth || !event.currentTarget.clientHeight) return;
      reading.current.top = event.currentTarget.scrollTop / reading.current.scale;
      reading.current.left = event.currentTarget.scrollLeft / reading.current.scale;
    }}>{error ? <PreviewError message={error}/> : <>{rendering && <div className="preview-pdf-loading" role="status">正在渲染 PDF…</div>}<div className="preview-pdf-page"><canvas ref={canvas} aria-label={`PDF 第 ${page} 页`}/><div ref={text} className="textLayer"/></div></>}</div>
  </>;
}
