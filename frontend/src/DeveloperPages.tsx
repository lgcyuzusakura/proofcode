import type { SourceAttachment } from "./types";
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { Check, Code2, FileCode2, GitBranch, Loader2, Play, Plus, PanelRightOpen, RefreshCw, Save, Search, ShieldCheck, Square, TerminalSquare, X } from "lucide-react";
import { TaskHistory } from "./TaskHistory";
import { verifiedSourceText, sourceExcerpt } from "./source-attachment-model";
import { api, desktopApp } from "./api";
import type { CommandState, EditorFile, GitState, Project, ProjectFile, Task, TaskApproval, TaskArtifact, TaskArtifactDetail, TaskEvent, SourceCapture } from "./types";
import { EventContent, StatusBadge } from "./TaskViews";
import { riskLabel, taskLabel, toolLabel } from "./presentation";
import { MergeWorkbench } from "./MergeWorkbench";
import { DebugTools, IDEProtocolContext, LanguageTools } from "./ProtocolTools";
import { reviewPatch, selectedPatch, selectionIds, type ReviewFile, type ReviewPatch } from "./patch-selection";
import { parseEditorDraft, patchDraftBlock } from "./editor-draft";
import { latestStreamingReview } from "./streaming-review";
import { latestWorkspacePreview } from "./workspace-preview";
export { ProjectBrowserPage as BrowserPage } from "./ProjectBrowserPage";
import type { Breakpoints, LanguageSession } from "./ProtocolTools";
const CodeEditor = lazy(() => import("./CodeEditor"));
const active = (task: Task | null) => !!task && ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(task.status);
function Empty({ title, text }: { title: string; text: string }) { return <div className="developer-empty"><Code2 size={26}/><h2>{title}</h2><p>{text}</p></div>; }
function restoreDraft(project: Project): { file: EditorFile; draft: string } | null {
  if (!project.localHandle) return null;
  try {
    const value = parseEditorDraft(sessionStorage.getItem(`proofcode.editor:${project.id}`));
    if (value.kind === "dirty" || value.kind === "saved") return value;
  } catch { /* An invalid draft never replaces project files. */ }
  return null;
}

export function IDEPage({ project, task, workspaceId, onAttach, onPreview }: { project: Project; task: Task | null; workspaceId?: string; onPreview?: (file: File) => void; onAttach?: (attachment: SourceAttachment) => void }) {
  const [session,setSession]=useState<LanguageSession|null>(null),[breakpoints,setBreakpoints]=useState<Breakpoints>({}),[line,setLine]=useState<number|undefined>();
  const [recovered] = useState(() => restoreDraft(project));
  const [files, setFiles] = useState<ProjectFile[]>([]), [file, setFile] = useState<EditorFile | null>(recovered?.file || null), [draft, setDraft] = useState(recovered?.draft || "");
  const [query, setQuery] = useState(""), [error, setError] = useState(""), [busy, setBusy] = useState(false), [newPath, setNewPath] = useState("");
  const [command, setCommand] = useState<CommandState | null>(null), [program, setProgram] = useState("npm"), [args, setArgs] = useState('["run","dev"]');
  const generation = useRef(0), handle = project.localHandle, native = !!handle && !!desktopApp()?.ReadProjectFile;
  const archive = useRef<Map<string, {path:string;sha256:string;content:string}>>(new Map());
  const sourceId = task?.resultSourceSnapshotId || task?.sourceSnapshotId;
  const dirty = !!file && draft !== file.content;
  const dirtyRef = useRef(dirty); dirtyRef.current = dirty;
  const persistenceFailed = useRef(false);
  useEffect(() => {
    try {
      if (native && file && dirty) sessionStorage.setItem(`proofcode.editor:${project.id}`, JSON.stringify({file,draft}));
      else sessionStorage.removeItem(`proofcode.editor:${project.id}`);
      persistenceFailed.current = false;
    } catch { persistenceFailed.current = true; setError("设备草稿空间不足，请先保存文件再切换页面。"); }
  }, [file,draft,project.id,native,dirty]);
  useEffect(() => {
    const guard = (event: Event) => { if (dirtyRef.current && persistenceFailed.current && !window.confirm("草稿无法暂存，放弃未保存修改并离开？")) event.preventDefault(); };
    window.addEventListener("proofcode:before-navigation", guard); return () => window.removeEventListener("proofcode:before-navigation", guard);
  }, []);
  const load = async () => {
    const request = ++generation.current; setBusy(true); setError("");
    try {
      let values: ProjectFile[];
      if (native) values = await desktopApp()!.ListProjectFiles!(handle!);
      else if (task && sourceId) {
        const capture = await api<{files:{path:string;sha256:string;content:string}[]}>(`/api/projects/${project.id}/workspaces/${task.workspaceId}/sources/${sourceId}/files`);
        if (request === generation.current) archive.current = new Map(capture.files.map(entry => [entry.path, entry]));
        values = capture.files.map(entry => ({path:entry.path,size:atob(entry.content).length,hash:entry.sha256}));
      } else values = [];
      if (request === generation.current) setFiles(values.sort((a,b)=>a.path.localeCompare(b.path)));
    } catch (cause) { if (request === generation.current) setError(String(cause)); }
    finally { if (request === generation.current) setBusy(false); }
  };
  useEffect(() => { if (!native) { setFile(null); setDraft(""); } void load(); return () => { generation.current++; }; }, [project.id, sourceId]);
  useEffect(() => { const warn = (event: BeforeUnloadEvent) => { if (dirtyRef.current) { event.preventDefault(); event.returnValue = ""; } }; window.addEventListener("beforeunload",warn);return()=>window.removeEventListener("beforeunload",warn); },[]);
  const open = async (path: string) => {
    if (dirty && !window.confirm("当前文件有未保存修改，放弃后打开其他文件？")) return;
    const request = ++generation.current; setBusy(true); setError("");
    try { const entry = archive.current.get(path); const value = native ? await desktopApp()!.ReadProjectFile!(handle!,path) : entry ? {path:entry.path,hash:entry.sha256,content:await verifiedSourceText(entry)} : undefined; if (!value) throw Error("文件不在当前源码中"); if(value.content.includes("\0")) throw Error("二进制文件无法编辑"); if(request===generation.current){setFile(value);setDraft(value.content);} }
    catch(cause){if(request===generation.current)setError(String(cause));}finally{if(request===generation.current)setBusy(false);}
  };
  const save = async () => {
    if (!native || !file || busy) return;
    setBusy(true);setError("");const request=generation.current;
    try{const value=await desktopApp()!.SaveProjectFile!(handle!,file.path,file.hash,draft);if(request===generation.current){setFile(value);setDraft(value.content);}await load();}
    catch(cause){if(request===generation.current)setError(String(cause));}finally{if(request===generation.current)setBusy(false);}
  };
  const attach = async (startLine?:number,endLine?:number) => {
    if(!file||busy||!workspaceId||!onAttach)return;
    if(dirty){setError("请先保存当前文件，再附加到聊天；未保存草稿不会覆盖任务源码。");return;}
    const selected=file; const request=generation.current;
    setError("");setBusy(true);
    try{
      sourceExcerpt(selected.content, {startLine,endLine});
      let attachment:SourceAttachment;
      if(native&&handle){
        const capture=await desktopApp()!.CaptureProjectSource!(handle) as SourceCapture;
        const entry=capture.files.find(value=>value.path===selected.path);
        if(!entry||entry.sha256!==selected.hash)throw Error("文件已在外部修改，请刷新文件后重新选择。");
        const source=await api<{id:string}>(`/api/projects/${project.id}/sources`,{method:"POST",body:JSON.stringify({workspaceId,...capture})});
        attachment={sourceSnapshotId:source.id,path:selected.path,sha256:entry.sha256,startLine,endLine};
      }else{
        if(!sourceId||task?.workspaceId!==workspaceId)throw Error("此源码快照不属于当前工作区。");
        attachment={sourceSnapshotId:sourceId,path:selected.path,sha256:selected.hash,startLine,endLine};
      }
      if(request!==generation.current)return;
      onAttach(attachment);
    }catch(cause){if(request===generation.current)setError(String(cause));}finally{if(request===generation.current)setBusy(false);}
  };
  useEffect(()=>{if(!native)return;let live=true;const poll=()=>desktopApp()!.GetProjectCommand!(handle!).then(value=>{if(live)setCommand(value);}).catch(()=>undefined);void poll();const timer=setInterval(()=>void poll(),1500);return()=>{live=false;clearInterval(timer);};},[handle,native]);
  const run = async () => { setError("");try{const parsed:unknown=JSON.parse(args);if(!Array.isArray(parsed)||parsed.some(value=>typeof value!=="string"))throw Error("参数需要 JSON 字符串数组");setCommand(await desktopApp()!.StartProjectCommand!(handle!,program,parsed));}catch(cause){setError(String(cause));} };
  return <IDEProtocolContext.Provider value={{session,breakpoints,line,toggleBreakpoint:(path,line)=>setBreakpoints(old=>({...old,[path]:(old[path]||[]).includes(line)?old[path].filter(value=>value!==line):[...(old[path]||[]),line].sort((a,b)=>a-b)})),onNavigate:(path,nextLine)=>{setLine(nextLine);void open(path);}}}><section className="developer-page ide-page"><div className="developer-heading"><div><h1>代码</h1><p>{project.name} · {native ? "当前本机文件" : "任务源码快照（只读）"}</p></div><button className="secondary-button" disabled={busy} onClick={()=>void load()}><RefreshCw size={14}/>刷新文件</button></div>{recovered&&dirty&&<p className="draft-recovered">已恢复此工程未保存的编辑草稿；保存时仍会核对原文件版本。</p>}{error&&<div role="alert" className="data-error">{error}</div>}
    <div className="ide-layout"><aside className="file-explorer"><label className="file-search"><Search size={13}/><input aria-label="搜索文件" value={query} onChange={event=>setQuery(event.target.value)} placeholder="搜索文件"/></label>{native&&<form className="new-file-form" onSubmit={event=>{event.preventDefault();if(dirty&&!window.confirm("放弃当前未保存修改？"))return;generation.current++;setFile({path:newPath.trim(),hash:"",content:""});setDraft("");setNewPath("");}}><input aria-label="新文件路径" value={newPath} onChange={event=>setNewPath(event.target.value)} placeholder="src/new-file.ts"/><button className="icon-button" type="submit" title="新建文件" disabled={busy||!newPath.trim()}><Plus size={14}/></button></form>}<div className="file-list">{files.filter(value=>value.path.toLowerCase().includes(query.toLowerCase())).map(value=><button disabled={busy} key={value.path} className={file?.path===value.path?"current":""} onClick={()=>void open(value.path)}><FileCode2 size={13}/><span title={value.path}>{value.path}</span></button>)}</div></aside>
    <div className="editor-area"><div className="editor-toolbar"><span>{file?.path||"选择文件"}{dirty&&" •"}</span>{file&&<small>{native?"可编辑":"只读快照"}</small>}{file && onPreview && /\.(md|markdown|txt)$/i.test(file.path) && <button className="text-button" onClick={() => onPreview(new File([draft], file.path.split(/[\\/]/).pop() || file.path, { type: "text/plain" }))}><PanelRightOpen size={13}/>预览</button>}{native&&<button className="text-button" disabled={!file||busy||(!dirty&&file.hash!=="")} onClick={()=>void save()}><Save size={13}/>保存 · Ctrl+S</button>}</div>{file?<Suspense fallback={<div className="developer-empty">加载编辑器…</div>}><CodeEditor fileId={`${project.id}:${file.path}:${file.hash}`} path={file.path} initial={draft} readOnly={!native||busy} onChange={setDraft} onSave={()=>void save()} onAttach={onAttach && workspaceId && !busy ? (start,end)=>void attach(start,end):undefined}/></Suspense>:<Empty title={busy?"读取源码…":"打开文件开始工作"} text={native?"文件保存会检查原版本；外部修改时保留编辑草稿并拒绝覆盖。":"本机编辑请使用桌面端绑定工程目录；Web 可查看任务保存的源码。"}/>}</div></div>
    {native&&<div className="command-console"><div className="developer-heading"><h2><TerminalSquare size={15}/>命令控制台</h2><span>{command?.running?"运行中":command?.id?`退出码 ${command.exitCode}`:"就绪"}</span></div><form onSubmit={event=>{event.preventDefault();void run();}}><label>程序<input aria-label="程序" value={program} onChange={event=>setProgram(event.target.value)} placeholder="npm / python / go"/></label><label className="command-args">参数（JSON 数组）<input aria-label="程序参数" value={args} onChange={event=>setArgs(event.target.value)}/></label><button className="primary-button" disabled={command?.running||!program} type="submit"><Play size={13}/>运行</button>{command?.running&&<button className="secondary-button" type="button" onClick={()=>void desktopApp()!.StopProjectCommand!(handle!,command.id).catch(cause=>setError(String(cause)))}><Square size={13}/>停止</button>}</form><pre>{command?.output||"输出将显示在这里。程序直接运行在当前工程目录中。"}{command?.truncated&&"\n[输出达到 1 MiB 限制]"}</pre></div>}
    {native&&desktopApp()?.StartProjectProtocol&&<><LanguageTools project={project} onSession={setSession}/><DebugTools project={project} path={file?.path} breakpoints={breakpoints} onNavigate={(path,nextLine)=>{setLine(nextLine);void open(path);}}/></>}
  </section></IDEProtocolContext.Provider>;
}

export function GitPage({ project }: { project: Project }) {
  const [state,setState]=useState<GitState|null>(null),[reload,setReload]=useState(0),[busy,setBusy]=useState(true);
  return <><GitControls key={reload} project={project} onState={setState} onBusy={setBusy}/>{state?.initialized&&project.localHandle&&<MergeWorkbench key={project.id} project={project} state={state} blocked={busy} onApplied={value=>{setState(value);setReload(old=>old+1);}}/>}</>;
}
function GitControls({ project,onState,onBusy }: { project: Project;onState:(state:GitState)=>void;onBusy:(busy:boolean)=>void }) {
  const [state,setInternalState]=useState<GitState|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState("");
  useEffect(()=>onBusy(busy),[busy,onBusy]);
  const setState=(value:GitState)=>{setInternalState(value);onState(value);};
  const [message,setMessage]=useState(""),[branch,setBranch]=useState(""),[diffTab,setDiffTab]=useState("working"),[remote,setRemote]=useState("");
  const handle=project.localHandle,native=!!handle&&!!desktopApp()?.GetProjectGit; const live=useRef(true);
  useEffect(()=>{live.current=true;void load();return()=>{live.current=false;};},[project.id]);
  const load=async()=>{if(!native)return;setBusy(true);try{const value=await desktopApp()!.GetProjectGit!(handle!);if(live.current)setState(value);}catch(cause){if(live.current)setError(String(cause));}finally{if(live.current)setBusy(false);}};
  const action=async(name:string,value="",paths:string[]=[])=>{if(!state||busy)return;if(["push","pull","set_origin"].includes(name)&&!window.confirm(name==="set_origin"?`设置 origin 为 ${value}？`:name==="pull"?`以 fast-forward 模式拉取 ${state.branch}？`:`推送 ${state.branch} 到 origin？`))return;setBusy(true);setError("");try{const next=await desktopApp()!.ProjectGitAction!(handle!,name,state.revision,value,paths);if(live.current){setState(next);if(name==="commit")setMessage("");}}catch(cause){if(live.current)setError(String(cause));}finally{if(live.current)setBusy(false);}};
  return <section className="developer-page"><div className="developer-heading"><div><h1>Git</h1><p>{project.name} · {state?.branch||"版本控制"}</p></div><button className="secondary-button" disabled={!native||busy} onClick={()=>void load()}><RefreshCw size={14}/>刷新</button></div>{error&&<div className="data-error" role="alert">{error}</div>}{!native?<Empty title="连接本机 Git 工程" text="桌面端绑定目录后可暂存、提交、切换分支与推送；任务补丁可以在代码审查页查看和下载。"/>:!state?<Empty title="读取 Git 状态" text="正在检查工程仓库。"/>:!state.initialized?<Empty title="此工程尚未初始化 Git" text="新工程可创建 main 分支，原有文件保持在工程中。"/>:<div className="git-layout"><aside className="git-sidebar"><div className="branch-controls"><GitBranch size={15}/><select aria-label="当前 Git 分支" disabled={busy} value={state.branch} onChange={event=>void action("switch_branch",event.target.value)}>{!state.branches.includes(state.branch)&&<option>{state.branch}</option>}{state.branches.map(value=><option key={value}>{value}</option>)}</select><button className="secondary-button" disabled={busy} onClick={()=>void action("push")}>推送</button></div><form onSubmit={event=>{event.preventDefault();void action("create_branch",branch);}}><input aria-label="新分支名称" value={branch} onChange={event=>setBranch(event.target.value)} placeholder="feature/my-change"/><button className="icon-button" title="创建分支" disabled={!branch||busy}><Plus size={14}/></button></form><form onSubmit={event=>{event.preventDefault();void action("set_origin",remote);}}><input aria-label="远程仓库地址" value={remote} onChange={event=>setRemote(event.target.value)} placeholder={state.origin||"https://github.com/user/repo.git"}/><button className="text-button" disabled={busy||!remote}>设置</button></form><small className="git-origin">{state.origin||"尚未设置 origin"}</small><div className="git-sync"><button className="secondary-button" disabled={busy||!state.origin} onClick={()=>void action("fetch")}>获取远程</button><button className="secondary-button" disabled={busy||!state.origin||state.files.length>0} onClick={()=>void action("pull")}>拉取（仅快进）</button></div><h2>变更 · {state.files.length}</h2>{state.files.map(file=><div className="git-file" key={file.path}><span><code>{file.index}{file.working}</code>{file.path}</span><div>{!file.operable&&<small>受保护路径 · 外部管理</small>}{file.operable&&file.working!==" "&&<button className="text-button" disabled={busy} onClick={()=>void action("stage","",[file.path])}>暂存</button>}{file.operable&&file.index!==" "&&file.index!=="?"&&<button className="text-button" disabled={busy} onClick={()=>void action("unstage","",[file.path])}>取消暂存</button>}</div></div>)}<textarea aria-label="提交说明" value={message} onChange={event=>setMessage(event.target.value)} placeholder="提交说明" rows={3}/><button className="primary-button" disabled={busy||!message.trim()||!state.files.some(file=>file.index!==" "&&file.index!=="?")} onClick={()=>void action("commit",message)}><Check size={14}/>提交暂存修改</button></aside><div className="git-content"><div className="data-tabs"><button className={diffTab==="working"?"active":""} onClick={()=>setDiffTab("working")}>工作区 diff</button><button className={diffTab==="staged"?"active":""} onClick={()=>setDiffTab("staged")}>暂存 diff</button><button className={diffTab==="history"?"active":""} onClick={()=>setDiffTab("history")}>提交历史</button></div>{diffTab==="history"?<div className="commit-history">{state.commits.map(commit=><div key={commit.hash}><strong>{commit.subject}</strong><small>{commit.hash.slice(0,10)} · {commit.author} · {new Date(commit.date).toLocaleString()}</small></div>)}</div>:<PatchView patch={diffTab==="staged"?state.stagedDiff:state.diff}/>}</div></div>}{native&&state&&!state.initialized&&<button className="primary-button" disabled={busy} onClick={()=>void action("init")}>初始化 Git 仓库</button>}</section>;
}


export function PatchView({patch}:{patch:string}) { return <pre className="review-patch">{patch?patch.split("\n").map((line,index)=><span key={index} className={line.startsWith("+")&&!line.startsWith("+++")?"diff-added":line.startsWith("-")&&!line.startsWith("---")?"diff-removed":line.startsWith("@@")?"diff-hunk":""}>{line||" "}{"\n"}</span>):"暂无文件变更"}</pre>; }
export function ReviewPage({ project, task, tasks, artifacts, events, approvals, onSelect, onApprove, onCancel, onRetry, onOpenEditor, retryDisabled = false }: {project:Project;task:Task|null;tasks:Task[];artifacts:TaskArtifact[];events:TaskEvent[];approvals:TaskApproval[];onSelect:(task:Task)=>void;onApprove:(id:string,approved:boolean)=>void;onCancel:()=>void;onRetry:()=>void;onOpenEditor:()=>void;retryDisabled?:boolean}) {
  const [detail,setDetail]=useState<TaskArtifactDetail|null>(null),[kind,setKind]=useState("checkpoint"),[error,setError]=useState(""),[busy,setBusy]=useState(false),[applied,setApplied]=useState(false);
  const [tab,setTab]=useState("changes"),[note,setNote]=useState("");
  const [historical,setHistorical]=useState<TaskArtifact|null>(null);
  const [draftBlocked,setDraftBlocked]=useState(false);
  const [selectedReview,setSelectedReview]=useState<Set<string>>(new Set());
  const pending=approvals.filter(value=>value.status==="PENDING");
  const streamingReview=useMemo(()=>latestStreamingReview(events,task?.attempt),[events,task?.attempt]);
  const workspacePreview=useMemo(()=>latestWorkspacePreview(events,task?.attempt),[events,task?.attempt]);
  useEffect(()=>{if(pending.length)setTab("approvals");},[pending.length]);
  const currentArtifact=artifacts.find(value=>value.kind===kind&&(value.attempt||1)===(task?.attempt||1));
  const selectedArtifact=historical?.taskId===task?.id?historical:currentArtifact;
  useEffect(()=>{
    let live=true;setApplied(false);setDetail(null);setError("");setDraftBlocked(false);setBusy(false);setSelectedReview(new Set());
    if(!task||!selectedArtifact)return;
    setBusy(true);const url=historical?.taskId===task.id?`/api/tasks/${task.id}/artifacts/by-id/${historical.id}`:`/api/tasks/${task.id}/artifacts/${kind}`;
    void api<TaskArtifactDetail>(url).then(value=>{if(live&&value.id===selectedArtifact.id&&value.taskId===task.id){setDetail(value);setSelectedReview(new Set(selectionIds(reviewPatch(value.patch||""))));}}).catch(cause=>{if(live)setError(String(cause));}).finally(()=>{if(live)setBusy(false);});return()=>{live=false;};
  },[task?.id,task?.attempt,kind,selectedArtifact?.id]);
  const validDetail=detail && detail.id===selectedArtifact?.id ? detail : null;
  const parsedReview = useMemo(() => reviewPatch(validDetail?.patch || ""), [validDetail?.id, validDetail?.patch]);
  const selectedReviewPatch = useMemo(() => { if(!validDetail||parsedReview.error)return "";try{return selectedPatch(parsedReview,selectedReview);}catch{return "";} }, [validDetail,parsedReview,selectedReview]);
  const noteKey=`proofcode.review:${project.id}:${task?.id}:${detail?.id||""}`;
  useEffect(()=>{setNote(localStorage.getItem(noteKey)||"");},[noteKey]);
  const download=()=>{if(!validDetail||!selectedReviewPatch)return;const url=URL.createObjectURL(new Blob([selectedReviewPatch],{type:"text/x-diff"}));const link=document.createElement("a");link.href=url;link.download=`${task?.id}-attempt-${validDetail.attempt||1}-${validDetail.kind}-selected.patch`;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);};
  const apply=async()=>{
    const bridge=desktopApp();
    if(historical||!project.localHandle||!task?.sourceSnapshotId||!validDetail||!selectedReviewPatch||busy||applied)return;
    setBusy(true);setError("");setDraftBlocked(false);
    const checkDraft=()=>{
      let reason:string;
      try { const selected=reviewPatch(selectedReviewPatch);reason=selected.error||patchDraftBlock(sessionStorage.getItem(`proofcode.editor:${project.id}`),selected.files.flatMap(file=>file.previousPath?[file.path,file.previousPath]:[file.path])); }
      catch { reason="无法读取编辑器草稿，已停止应用。请打开代码页检查草稿。"; }
      if(reason){setDraftBlocked(true);throw Error(reason);}
    };
    try{
      checkDraft();
      if(!bridge?.CaptureProjectSource||!bridge.ApplyProjectPatch)throw Error("桌面接口不支持当前文件校验，请更新桌面端后重试。");
      const source=await bridge.CaptureProjectSource(project.localHandle);
      checkDraft();
      const result=await bridge.ApplyProjectPatch(project.localHandle,source.manifestHash,selectedReviewPatch);
      setApplied(result.applied);
    }catch(cause){setError(String(cause));}finally{setBusy(false);}
  };
  const toggleReview=(id:string,checked:boolean)=>setSelectedReview(old=>{const next=new Set(old);if(checked)next.add(id);else next.delete(id);return next;});
  const selectAllReview=()=>setSelectedReview(new Set(selectionIds(parsedReview)));
  return <section className="developer-page review-page"><div className="developer-heading"><div><h1>代码审查</h1><p>{task?.prompt||"选择需要审查的代码回合"}</p></div>{task&&<StatusBadge status={task.status}/>}</div><div className="review-task-picker"><select aria-label="审查回合" value={task?.id||""} onChange={event=>{const next=tasks.find(value=>value.id===event.target.value);if(next)onSelect(next);}}>{tasks.map(value=><option key={value.id} value={value.id}>{value.prompt.slice(0,80)} · {taskLabel(value.status)}</option>)}</select>{active(task)&&<button className="secondary-button" onClick={onCancel}>停止</button>}{task&&["FAILED","CANCELLED"].includes(task.status)&&!retryDisabled&&<button className="secondary-button" onClick={onRetry}>重试</button>}</div><div className="data-tabs"><button className={tab==="changes"?"active":""} onClick={()=>setTab("changes")}>文件变更</button><button className={tab==="approvals"?"active":""} onClick={()=>setTab("approvals")}>工具审批 {pending.length>0&&`· ${pending.length}`}</button><button className={tab==="verification"?"active":""} onClick={()=>setTab("verification")}>验证结果</button><button className={tab==="events"?"active":""} onClick={()=>setTab("events")}>执行证据</button><button className={tab==="history"?"active":""} onClick={()=>setTab("history")}>恢复记录</button></div>{error&&<div className="data-error" role="alert">{error}{draftBlocked&&<button type="button" className="secondary-button" onClick={onOpenEditor}>打开代码页</button>}</div>}{task?.error&&<div className="data-error">{task.error}</div>}
    {tab==="changes"&&!historical&&workspacePreview&&<section className="workspace-preview" aria-label="实时工作区变更"><header><strong>工作区变更预览</strong><time dateTime={workspacePreview.timestamp}>{new Date(workspacePreview.timestamp).toLocaleTimeString()}</time></header><p>{workspacePreview.state==="ready"?workspacePreview.patch?"已写入工作区的累计修改":"当前工作区没有代码变更":workspacePreview.state==="oversized"?"修改超过实时预览上限，请在执行结束后查看完整审查产物。":"暂时无法生成实时预览，任务可继续执行。"}</p>{workspacePreview.state==="ready"&&workspacePreview.patch&&<div tabIndex={0} role="region" aria-label="实时变更代码"><PatchView patch={workspacePreview.patch}/></div>}<small>预览随工具完成更新；应用修改请使用正式审查产物。</small></section>}
    {tab==="changes"&&<>{historical&&<div className="historical-artifact-note"><span>历史产物 · 尝试 {historical.attempt||1} · 仅预览与下载</span><button type="button" className="secondary-button" onClick={()=>{setHistorical(null);setApplied(false);}}>返回当前产物</button></div>}{!historical&&streamingReview?.text&&<section className="review-streaming" aria-label="实时模型回复" aria-live="polite"><div><strong>模型实时回复</strong><span>{active(task)?"正在生成":task?.status==="FAILED"?"任务失败前已收到":"本次响应"} · 步骤 {streamingReview.step}</span></div><pre>{streamingReview.text}</pre></section>}<div className="review-toolbar"><select aria-label="补丁类型" disabled={!!historical} value={historical?.kind||kind} onChange={event=>setKind(event.target.value)}><option value="checkpoint">正常结果</option><option value="recovery">恢复补丁</option></select><small>{detail?.branch} {detail?.commitHash?.slice(0,12)}</small><button className="secondary-button" disabled={!validDetail||busy||!selectedReviewPatch} onClick={download}>下载所选 patch</button>{!historical&&project.localHandle&&task?.sourceSnapshotId&&desktopApp()?.ApplyProjectPatch&&<button className="primary-button" disabled={!validDetail||busy||applied||!selectedReviewPatch} onClick={()=>void apply()}>{applied?"已应用到工程":"批准应用所选变更"}</button>}</div>{busy&&!detail?<Empty title="读取变更…" text=""/>:parsedReview.error?<div className="data-error" role="alert">{parsedReview.error}</div>:<ReviewPatchView review={parsedReview} selected={selectedReview} onToggle={toggleReview} onSelectAll={selectAllReview} onClear={()=>setSelectedReview(new Set())}/>}<label className="review-notes">审查笔记（保存在当前设备）<textarea aria-label="审查笔记" rows={3} value={note} onChange={event=>{setNote(event.target.value);localStorage.setItem(noteKey,event.target.value);}} placeholder="记录文件、行号和需要跟进的问题…"/></label></>}
    {tab==="history"&&<TaskHistory task={task} onReview={artifact=>{setHistorical(artifact);setTab("changes");}}/>}
    {tab==="approvals"&&<div className="review-approvals">{approvals.map(value=><article key={value.id}><div className="approval-heading"><span className="approval-tool-icon"><ShieldCheck size={17}/></span><h3>{toolLabel(value.tool)}<small>{value.status === "PENDING" ? "等待你的确认" : value.status === "APPROVED" ? "已批准" : "已拒绝"}</small></h3><span className={`approval-risk ${String(value.risk).toLowerCase()}`}>{riskLabel(value.risk)}</span></div><ApprovalVisual arguments={value.arguments}/>{value.status==="PENDING"&&<div className="approval-decision"><button className="primary-button" onClick={()=>onApprove(value.id,true)}>批准此次调用</button><button className="secondary-button" onClick={()=>onApprove(value.id,false)}>拒绝</button></div>}</article>)}{!approvals.length&&<Empty title="无需审批" text="本回合没有待处理的工具调用。"/>}</div>}
    {(tab==="events"||tab==="verification")&&<div className="review-events">{events.filter(value=>tab==="verification"?value.type==="verification.completed"||value.type==="task.failed":!["agent.message.delta","usage.updated"].includes(value.type)).map(value=><article key={`${value.attempt}-${value.sequence}`}><strong>{reviewEventLabels[value.type] || "执行事件"}</strong><small>第 {value.sequence} 个节点 · {new Date(value.timestamp).toLocaleTimeString()}</small><EventContent item={value}/></article>)}</div>}
  </section>;
}
const reviewEventLabels: Record<string, string> = { "verification.completed": "验证完成", "workspace.updated": "工作区变更预览", "task.failed": "任务失败", "task.completed": "任务完成", "tool.requested": "请求工具", "tool.started": "工具开始", "tool.completed": "工具完成", "tool.failed": "工具失败", "checkpoint.created": "创建检查点", "recovery.created": "创建恢复补丁" };
function ReviewPatchView({review,selected,onToggle,onSelectAll,onClear}:{review:ReviewPatch;selected:Set<string>;onToggle:(id:string,checked:boolean)=>void;onSelectAll:()=>void;onClear:()=>void}) {
  if(!review.files.length)return <Empty title="暂无文件变更" text="此任务没有可审查的代码块。"/>;
  const total=selectionIds(review).length, chosen=selected.size;
  return <div className="review-selection" aria-label="逐文件和代码块审查">
    <div className="review-selection-toolbar"><strong>{chosen} / {total} 项已选</strong><div><button type="button" className="text-button" onClick={onSelectAll}>全选</button><button type="button" className="text-button" onClick={onClear}>清空</button></div></div>
    {review.files.map(file=><ReviewFileView key={file.id} file={file} selected={selected} onToggle={onToggle}/>)}
  </div>;
}
function ReviewFileView({file,selected,onToggle}:{file:ReviewFile;selected:Set<string>;onToggle:(id:string,checked:boolean)=>void}) {
  const ids=file.atomicReason?[file.id]:file.hunks.map(hunk=>hunk.id),count=ids.filter(id=>selected.has(id)).length;
  return <section className="review-file" aria-label={`审查文件 ${file.path}`}>
    <header><label><input type="checkbox" aria-label={`选择文件 ${file.path}`} checked={count===ids.length&&ids.length>0} ref={node=>{if(node)node.indeterminate=count>0&&count<ids.length;}} onChange={event=>ids.forEach(id=>onToggle(id,event.currentTarget.checked))}/><strong>{file.path}</strong></label>{file.previousPath&&<small>来自 {file.previousPath}</small>}{file.atomicReason&&<span>{file.atomicReason}</span>}</header>
    {file.atomicReason?<pre className="review-atomic-patch">{file.raw}</pre>:file.hunks.map(hunk=><details className="review-hunk" key={hunk.id} open><summary><input type="checkbox" aria-label={`选择代码块 ${file.path} ${hunk.header}`} checked={selected.has(hunk.id)} onChange={event=>onToggle(hunk.id,event.currentTarget.checked)}/><code>{hunk.header}</code></summary><pre>{hunk.lines.map((line,index)=>{const marker=line[0],content=line.startsWith("\\ No newline")?line.slice(1):line.slice(1);return <span key={index} className={marker==="+"?"diff-added":marker==="-"?"diff-removed":marker==="\\"?"diff-no-newline":""}><i>{marker==="\\"?"":marker}</i>{content||" "}{"\n"}</span>;})}</pre></details>)}
  </section>;
}
function ApprovalVisual({ arguments: value }: { arguments: unknown }) {
  const entries = value && typeof value === "object" && !Array.isArray(value) ? Object.entries(value as Record<string, unknown>) : [];
  const labels: Record<string, string> = { path: "文件路径", command: "命令", query: "查询", reason: "原因", pattern: "搜索内容", oldText: "原内容", newText: "新内容", timeoutMs: "超时时间", schema: "数据分区", table: "数据表", key: "缓存键", value: "缓存值", ttlSeconds: "有效时间" };
  return <div className="approval-visual"><div className="approval-visual-title"><span>即将执行的内容</span><small>执行前仍需你的批准</small></div><div className="approval-fields">{entries.map(([key, item]) => <div key={key}><span>{labels[key] || key}</span><strong>{typeof item === "string" ? item : JSON.stringify(item)}</strong></div>)}</div><details><summary>查看完整参数</summary><pre>{JSON.stringify(value, null, 2)}</pre></details></div>;
}
