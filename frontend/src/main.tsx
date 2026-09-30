import { FormEvent, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity, Archive, ArrowUpRight, Bot, Check, CheckCircle2, ChevronDown, CircleDot, Clock3,
  Code2, Command, FileCode2, FolderGit2, Gauge, GitBranch, Layers3, Menu, MessageSquare,
  MoreHorizontal, Paperclip, Play, Plus, RefreshCw, Rocket, Search, Settings2, ShieldCheck,
  Sparkles, Square, TerminalSquare, Wrench, X, XCircle, Loader2, Puzzle, Database, Globe2, SlidersHorizontal, Workflow, Zap
} from "lucide-react";
import "./styles.css";

type Project = { id: string; name: string; repositoryUrl: string; defaultBranch: string };
type Task = { id: string; projectId: string; prompt: string; model: string; status: string; result?: string; error?: string; createdAt: string };
type TaskEvent = { sequence: number; type: string; timestamp: string; payload: Record<string, unknown>; attempt?: number; runId?: string };
type TaskArtifact = { id: string; taskId: string; attempt?: number; kind: string; commitHash?: string; branch?: string; createdAt: string };
type TaskArtifactDetail = TaskArtifact & { patch: string; metadata?: Record<string, unknown> };
type TaskApproval = { id: string; taskId: string; callId: string; tool: string; risk: string; arguments: Record<string, unknown>; status: string; decision?: string; createdAt: string };
type ModelConfig = { configured: boolean; provider: string; model: string; baseUrl: string; source: string; hasKey: boolean; keyLast4: string; maxContextTokens: number; maxOutputTokens: number; maxTotalTokens: number; error?: string };
type PanelKind = "运行统计" | "账户" | "代码证据" | "验证中心" | "最近活动" | "Agent 设置";

const token = () => localStorage.getItem("proofcode.token") || "change-me";
const emptyProject: Project = { id: "", name: "未连接项目", repositoryUrl: "", defaultBranch: "main" };

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...init, headers: { "Content-Type": "application/json", Authorization: `Bearer ${token()}`, ...(init?.headers || {}) } });
  if (!response.ok) throw new Error(`${response.status} ${await response.text()}`);
  return response.status === 204 ? (undefined as T) : response.json();
}

function App() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProject, setSelectedProject] = useState<Project>(emptyProject);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [artifacts, setArtifacts] = useState<TaskArtifact[]>([]);
  const [approvals, setApprovals] = useState<TaskApproval[]>([]);
  const [showProjectForm, setShowProjectForm] = useState(false);
  const [error, setError] = useState("");
  const [activeNav, setActiveNav] = useState("工作台");
  const [query, setQuery] = useState("");
  const [backendOnline, setBackendOnline] = useState(false);
  const [collapsed, setCollapsed] = useState(false);
  const [openPanel, setOpenPanel] = useState<PanelKind | null>(null);

  const loadProjects = async () => { try { const values = await api<Project[]>("/api/projects"); setProjects(values); setSelectedProject((old) => values.find((item) => item.id === old.id) || values[0] || emptyProject); setBackendOnline(true); } catch { setBackendOnline(false); } };
  const loadTasks = async (project = selectedProject) => { if (!project.id) { setTasks([]); setSelectedTask(null); return; } try { const values = await api<Task[]>(`/api/tasks?projectId=${project.id}`); setTasks(values); setSelectedTask((old) => values.find((item) => item.id === old?.id) || values[0] || null); setBackendOnline(true); } catch { setBackendOnline(false); } };
  useEffect(() => { void loadProjects(); }, []);
  useEffect(() => { void loadTasks(selectedProject); }, [selectedProject.id]);
  useEffect(() => {
    if (!selectedTask) { setEvents([]); setArtifacts([]); setApprovals([]); return; }
    let active = true;
    const taskId = selectedTask.id;
    const known = new Map<number, TaskEvent>();
    let cursor = 0;
    let syncing = false;
    let needsSync = false;
    let retryDelay = 1000;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
    let socket: WebSocket | undefined;
    setEvents([]);
    setArtifacts([]);
    setApprovals([]);
    const merge = (batch: TaskEvent[]) => {
      if (!active) return;
      for (const item of batch) known.set(item.sequence, item);
      while (known.has(cursor + 1)) cursor++;
      setEvents([...known.values()].sort((left, right) => left.sequence - right.sequence));
    };
    const refreshRelated = () => {
      void api<TaskArtifact[]>(`/api/tasks/${taskId}/artifacts`).then((values) => { if (active) setArtifacts(values); }).catch(() => undefined);
      void api<TaskApproval[]>(`/api/tasks/${taskId}/approvals`).then((values) => { if (active) setApprovals(values); }).catch(() => undefined);
    };
    const syncEvents = async () => {
      needsSync = true;
      if (syncing) return;
      syncing = true;
      try {
        while (active && needsSync) {
          needsSync = false;
          let page: TaskEvent[];
          do {
            page = await api<TaskEvent[]>(`/api/tasks/${taskId}/events?afterSequence=${cursor}`);
            merge(page);
          } while (active && page.length === 500);
        }
      } catch { /* A reconnect or the next event retries the durable history. */ }
      finally { syncing = false; }
    };
    const protocol = location.protocol === "https:" ? "wss" : "ws";
    const connect = () => {
      if (!active) return;
      socket = new WebSocket(`${protocol}://${location.host}/ws/tasks/${taskId}?token=${encodeURIComponent(token())}`);
      socket.onopen = () => { retryDelay = 1000; void syncEvents(); refreshRelated(); void loadTasks(); };
      socket.onmessage = (message) => {
        if (!active) return;
        let value: TaskEvent;
        try {
          value = JSON.parse(message.data) as TaskEvent;
          const gap = value.sequence > cursor + 1;
          merge([value]);
          if (gap) void syncEvents();
        } catch { return; }
        if (["task.started", "task.completed", "task.failed", "task.cancelled", "tool.approval_required", "checkpoint.created", "recovery.created", "verification.completed"].includes(value.type)) {
          refreshRelated();
          void loadTasks();
        }
      };
      socket.onclose = () => {
        if (!active) return;
        reconnectTimer = setTimeout(connect, retryDelay);
        retryDelay = Math.min(retryDelay * 2, 15000);
      };
    };
    refreshRelated();
    connect();
    void syncEvents();
    const poll = setInterval(() => { void syncEvents(); refreshRelated(); }, 15000);
    return () => { active = false; clearInterval(poll); if (reconnectTimer) clearTimeout(reconnectTimer); socket?.close(); };
  }, [selectedTask?.id]);

  const filteredTasks = useMemo(() => tasks.filter((task) => task.prompt.toLowerCase().includes(query.toLowerCase())), [tasks, query]);
  const activeCount = tasks.filter((task) => ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(task.status)).length;
  const succeededCount = tasks.filter((task) => task.status === "SUCCEEDED").length;
  const completedCount = tasks.filter((task) => ["SUCCEEDED", "FAILED", "CANCELLED"].includes(task.status)).length;
  const successRate = completedCount ? `${Math.round(succeededCount / completedCount * 100)}%` : "--";
  const tokensUsed = events.filter((item) => item.type === "usage.updated").reduce((value, item) => Math.max(value, Number(item.payload.totalTokens || 0)), 0);
  const createTask = async (prompt: string, model: string) => {
    if (!selectedProject.id) { setError("请先连接一个 Git 项目。"); setShowProjectForm(true); return false; }
    try { const task = await api<Task>("/api/tasks", { method: "POST", headers: { "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify({ projectId: selectedProject.id, prompt, model }) }); setTasks((old) => [task, ...old.filter((value) => value.id !== task.id)]); setSelectedTask(task); setBackendOnline(true); setError(""); return true; }
    catch (cause) { if (cause instanceof TypeError) { setBackendOnline(false); setError("控制面未连接，任务未创建。请检查服务状态。"); } else setError(`任务未创建：${String(cause)}`); return false; }
  };
  const retrySelected = async () => {
    if (!selectedTask) return;
    try { const task = await api<Task>(`/api/tasks/${selectedTask.id}/retry`, { method: "POST" }); setTasks((old) => old.map((value) => value.id === task.id ? task : value)); setSelectedTask(task); setError("任务已重新排队"); } catch (e) { setError(String(e)); }
  };
  const navigate = (label: string) => { setActiveNav(label); setOpenPanel(null); };
  const cancelSelected = async () => {
    if (!selectedTask) return;
    try { await api(`/api/tasks/${selectedTask.id}/cancel`, { method: "POST" }); await loadTasks(); setError("任务已停止"); }
    catch (e) { setError(String(e)); }
  };
  const decideApproval = async (approvalId: string, approved: boolean) => {
    if (!selectedTask) return;
    try { const value = await api<TaskApproval>(`/api/tasks/${selectedTask.id}/approvals/${approvalId}`, { method: "POST", body: JSON.stringify({ approved }) }); setApprovals((old) => old.map((approval) => approval.id === value.id ? value : approval)); await loadTasks(); setError(approved ? "已批准，任务重新排队" : "已拒绝，任务重新排队"); } catch (e) { setError(String(e)); }
  };

  return <div className={`app-shell ${["工作台", "执行链", "插件"].includes(activeNav) ? "has-route" : ""}`}>
    <header className="topbar"><div className="top-left"><button className="rail-toggle" title={collapsed ? "展开导航" : "收起导航"} onClick={() => setCollapsed((value) => !value)}><Menu size={17}/></button><div className="brand-mark">P</div><div className="brand-copy"><strong>ProofCode</strong><span>可验证编程工作台</span></div><div className="workspace-picker"><span className="workspace-dot"/>{selectedProject.name}<ChevronDown size={14}/></div></div><div className="top-center"><div className="command-search"><Search size={15}/><input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索任务、文件或运行记录"/><kbd><Command size={11}/>K</kbd></div></div><div className="top-right"><span className={`connection ${backendOnline ? "online" : "local"}`}><span/> {backendOnline ? "Control Plane 在线" : "Control Plane 离线"}</span><button className="icon-button" title="刷新" onClick={() => { void loadProjects(); void loadTasks(); setError("已刷新项目和任务"); }}><RefreshCw size={16}/></button><button className="avatar" title="账户" onClick={() => setOpenPanel("账户")}>A</button></div></header>
    <div className={`app-body ${collapsed ? "rail-collapsed" : ""}`}><aside className={`nav-rail ${collapsed ? "collapsed" : ""}`}><div className="nav-group"><NavItem icon={<Bot size={16}/>} label="编码 Agent" active={activeNav === "工作台"} onClick={() => navigate("工作台")}/><NavItem icon={<Layers3 size={16}/>} label="概览" active={activeNav === "概览"} onClick={() => navigate("概览")}/><NavItem icon={<MessageSquare size={16}/>} label="会话" active={activeNav === "会话"} onClick={() => navigate("会话")} count={tasks.length}/><NavItem icon={<FolderGit2 size={16}/>} label="项目" active={activeNav === "项目"} onClick={() => navigate("项目")}/></div><div className="nav-label">Agent 工具</div><div className="nav-group"><NavItem icon={<Workflow size={16}/>} label="执行链" active={activeNav === "执行链"} onClick={() => navigate("执行链")}/><NavItem icon={<Wrench size={16}/>} label="可用工具" active={activeNav === "插件"} onClick={() => navigate("插件")}/><NavItem icon={<Code2 size={16}/>} label="代码证据" onClick={() => setOpenPanel("代码证据")}/><NavItem icon={<ShieldCheck size={16}/>} label="验证中心" onClick={() => setOpenPanel("验证中心")}/></div><div className="projects-nav"><div className="nav-label project-label">项目 <button className="tiny-button" title="新建项目" onClick={() => setShowProjectForm(true)}><Plus size={14}/></button></div>{projects.map((project) => <button key={project.id} className={`mini-project ${selectedProject.id === project.id ? "selected" : ""}`} onClick={() => setSelectedProject(project)}><span className="project-avatar">{project.name.slice(0, 1).toUpperCase()}</span><span>{project.name}</span><span className="mini-branch">{project.defaultBranch}</span></button>)}</div><div className="nav-bottom"><NavItem icon={<Settings2 size={16}/>} label="设置" onClick={() => setOpenPanel("Agent 设置")}/><div className="privacy-note"><ShieldCheck size={14}/><span>Worktree 隔离<br/>审计记录已开启</span></div></div></aside>
      <main className="main-content"><div className="page-heading"><div><div className="breadcrumb">WORKSPACE / {selectedProject.defaultBranch}</div><h1>工作台概览</h1><p>把想法交给 Agent，所有修改都会留下可验证的证据。</p></div><div className="heading-actions"><button className="secondary-button" onClick={() => setOpenPanel("运行统计")}><Gauge size={15}/>运行统计</button><button className="primary-button" onClick={() => document.getElementById("task-composer")?.focus()}><Plus size={16}/>新建任务</button></div></div><section className="metric-grid"><Metric icon={<Activity size={17}/>} label="活跃任务" value={String(activeCount).padStart(2, "0")} detail="当前项目" tone="blue"/><Metric icon={<CheckCircle2 size={17}/>} label="已完成" value={String(succeededCount).padStart(2, "0")} detail="当前项目" tone="green"/><Metric icon={<ShieldCheck size={17}/>} label="成功率" value={successRate} detail={`${completedCount} 个已结束任务`} tone="violet"/><Metric icon={<Sparkles size={17}/>} label="Token 消耗" value={tokensUsed.toLocaleString()} detail="当前任务" tone="amber"/></section><div className="workspace-grid"><div className="primary-column"><TaskComposer onCreated={createTask}/><section className="section-block"><div className="section-heading"><div><span className="section-kicker">LIVE SESSIONS</span><h2>最近会话</h2></div><button className="text-button" onClick={() => { setQuery(""); setError("已显示全部会话"); }}>查看全部 <ArrowUpRight size={14}/></button></div><div className="session-list">{filteredTasks.map((task) => <SessionRow key={task.id} task={task} selected={selectedTask?.id === task.id} onClick={() => setSelectedTask(task)}/>)}{filteredTasks.length === 0 && <div className="empty-state"><Search size={22}/><p>没有匹配的会话</p></div>}</div></section><section className="section-block activity-block"><div className="section-heading"><div><span className="section-kicker">RECENT ACTIVITY</span><h2>验证与变更</h2></div><button className="icon-button" title="更多" onClick={() => setOpenPanel("最近活动")}><MoreHorizontal size={17}/></button></div><div className="activity-list">{events.slice(-8).reverse().map((item) => <ActivityRow key={item.sequence} icon={item.type.startsWith("tool.") ? <Wrench size={14}/> : <CircleDot size={14}/>} title={item.type} detail={String(item.payload.tool || item.payload.result || "")} time={new Date(item.timestamp).toLocaleTimeString()} tone={item.type.endsWith("failed") ? "violet" : "blue"}/>)}{events.length === 0 && <div className="empty-state">暂无运行记录</div>}</div></section></div><aside className="right-column"><TaskCapsule key={selectedTask?.id || "empty"} task={selectedTask} events={events} artifacts={artifacts} approvals={approvals} onApprove={decideApproval} onCancel={cancelSelected} onRetry={retrySelected}/></aside></div></main></div>
    {showProjectForm && <ProjectForm onClose={() => setShowProjectForm(false)} onCreated={(project) => { setProjects((old) => [project, ...old]); setSelectedProject(project); setShowProjectForm(false); }}/>} {openPanel && <ActionPanel kind={openPanel} tasks={tasks} events={events} artifacts={artifacts} onClose={() => setOpenPanel(null)}/>} {error && <div className="toast"><XCircle size={16}/>{error}<button onClick={() => setError("")}><X size={14}/></button></div>}
    {activeNav === "工作台" && <div className="route-overlay"><AgentWorkspace project={selectedProject} task={selectedTask} tasks={tasks} events={events} artifacts={artifacts} approvals={approvals} onCreated={createTask} onSelectTask={setSelectedTask} onApprove={decideApproval} onCancel={cancelSelected} onRetry={retrySelected} onNavigate={navigate}/></div>}
    {activeNav === "执行链" && <div className="route-overlay"><ToolchainPage task={selectedTask} events={events}/></div>}
    {activeNav === "插件" && <div className="route-overlay"><ToolUsagePage task={selectedTask} events={events}/></div>}
  </div>;
}

function AgentWorkspace({ project, task, tasks, events, artifacts, approvals, onCreated, onSelectTask, onApprove, onCancel, onRetry, onNavigate }: { project: Project; task: Task | null; tasks: Task[]; events: TaskEvent[]; artifacts: TaskArtifact[]; approvals: TaskApproval[]; onCreated: (prompt: string, model: string) => Promise<boolean>; onSelectTask: (task: Task) => void; onApprove: (approvalId: string, approved: boolean) => void; onCancel: () => void; onRetry: () => void; onNavigate: (label: string) => void }) {
  const [prompt, setPrompt] = useState("");
  const [model, setModel] = useState("gpt-4.1-mini");
  const [submitting, setSubmitting] = useState(false);
  const submit = async (event: FormEvent) => { event.preventDefault(); if (!prompt.trim() || submitting) return; setSubmitting(true); try { if (await onCreated(prompt.trim(), model)) setPrompt(""); } finally { setSubmitting(false); } };
  const visibleEvents = events.filter((item) => !["agent.message.delta", "usage.updated"].includes(item.type));
  return <div className="agent-page">
    <div className="agent-page-header"><div><span className="section-kicker">CODING AGENT</span><h1>编码 Agent</h1></div><div className="agent-header-actions"><span className="workspace-chip"><span className="workspace-dot"/>{project.name}</span><button className="secondary-button" onClick={() => onNavigate("执行链")}><Workflow size={15}/>查看执行链</button></div></div>
    {tasks.length > 0 && <div className="agent-task-picker"><label htmlFor="agent-task-select">任务</label><select id="agent-task-select" value={task?.id || ""} onChange={(event) => { const selected = tasks.find((item) => item.id === event.target.value); if (selected) onSelectTask(selected); }}>{tasks.map((item) => <option key={item.id} value={item.id}>{item.prompt} · {item.status}</option>)}</select></div>}
    <div className="agent-layout"><section className="conversation-panel"><div className="conversation-head"><div><strong>当前任务</strong><span>{task ? task.id : "等待新任务"}</span></div>{task && <StatusBadge status={task.status}/>}</div><div className="conversation-stream">
      {task && <div className="message-row user"><div className="message-bubble"><strong>你</strong><p>{task.prompt}</p><span className="message-meta">{task.model} · {new Date(task.createdAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span></div><div className="message-avatar user-avatar">A</div></div>}
      {visibleEvents.map((item) => <div className="event-entry" key={item.sequence}><span className="event-entry-icon">{item.type.startsWith("tool.") ? <Wrench size={14}/> : <CircleDot size={14}/>}</span><div><div className="event-entry-head"><strong>{item.type}</strong><time>{new Date(item.timestamp).toLocaleTimeString()}</time></div><EventContent item={item}/></div></div>)}
      {task && visibleEvents.length === 0 && <div className="event-empty">暂无执行事件</div>}
      {!task && <div className="conversation-empty"><Sparkles size={28}/><strong>从一个目标开始</strong><span>描述代码修改或排查目标。</span></div>}
    </div><form className="agent-composer" onSubmit={(event) => void submit(event)}><textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder="描述你要完成的编码任务..." rows={3}/><div className="agent-composer-footer"><span className="composer-hint">每次发送会创建一个新任务</span><div className="agent-composer-run"><select value={model} onChange={(e) => setModel(e.target.value)}><option>gpt-4.1-mini</option><option>gpt-4o</option><option>deepseek-chat</option></select><button className="run-button" type="submit" disabled={submitting || !prompt.trim()}>{submitting ? <Loader2 size={14}/> : <Play size={14} fill="currentColor"/>}{submitting ? "创建中" : "运行任务"}</button></div></div></form></section>
      <aside className="agent-context"><TaskCapsule key={task?.id || "empty"} task={task} events={events} artifacts={artifacts} approvals={approvals} onApprove={onApprove} onCancel={onCancel} onRetry={onRetry}/></aside>
    </div>
  </div>;
}

function EventContent({ item }: { item: TaskEvent }) {
  const payload = item.payload || {};
  const message = item.type === "agent.message.completed" ? payload.content : item.type === "verification.completed" ? payload.report : item.type === "task.failed" ? payload.error : item.type.startsWith("tool.") ? payload.result : item.type === "task.completed" ? payload.result : item.type === "decision.tool_routed" || item.type === "decision.tool_route_fallback" ? JSON.stringify(payload, null, 2) : undefined;
  const tool = typeof payload.tool === "string" ? payload.tool : "";
  return <div className="event-entry-content">{tool && <span className="event-tool">{tool} · {String(payload.callId || "")}</span>}{item.type.startsWith("decision.") && <span className="event-tool">决策层 · {String(payload.choice || payload.reason || "回退")}</span>}{message != null && <pre>{typeof message === "string" ? message : JSON.stringify(message, null, 2)}</pre>}{(item.type === "checkpoint.created" || item.type === "recovery.created") && <span>{item.type === "recovery.created" ? "恢复补丁" : "Checkpoint"} · {String(payload.commit || "").slice(0, 12)} · {String(payload.branch || "")}</span>}</div>;
}

function ToolchainPage({ task, events }: { task: Task | null; events: TaskEvent[] }) {
  const [selectedSequence, setSelectedSequence] = useState<number | null>(null);
  const selected = events.find((item) => item.sequence === selectedSequence) || events[events.length - 1];
  return <div className="toolchain-page"><div className="toolchain-heading"><div><span className="section-kicker">EXECUTION LOG</span><h1>执行链</h1></div>{task && <StatusBadge status={task.status}/>}</div><div className="chain-summary"><div><span>当前任务</span><strong>{task?.id || "等待新任务"}</strong></div><div><span>持久化事件</span><strong>{events.length}</strong></div><div><span>工具调用</span><strong>{events.filter((item) => item.type === "tool.requested").length}</strong></div><div><span>执行状态</span><strong>{task?.status || "未开始"}</strong></div></div><div className="toolchain-layout"><section className="chain-visual"><div className="chain-visual-head"><div><strong>事件时间线</strong><span>按服务端序号排序</span></div></div><div className="chain-event-list">{events.map((item) => <button key={item.sequence} className={`chain-event ${selected?.sequence === item.sequence ? "selected" : ""}`} onClick={() => setSelectedSequence(item.sequence)}><span className="chain-event-sequence">#{item.sequence}</span><span className="chain-event-type">{item.type}</span><span className="chain-event-tool">{typeof item.payload?.tool === "string" ? item.payload.tool : ""}</span><time>{new Date(item.timestamp).toLocaleTimeString()}</time></button>)}{events.length === 0 && <div className="event-empty">暂无执行事件</div>}</div></section><aside className="node-inspector"><div className="inspector-head"><div><span className="section-kicker">EVENT DETAIL</span><h2>{selected?.type || "选择事件"}</h2></div></div>{selected && <><div className="chain-event-info"><span>序号 #{selected.sequence}</span><time>{new Date(selected.timestamp).toLocaleString()}</time></div><pre className="chain-event-payload">{JSON.stringify(selected.payload, null, 2)}</pre></>}</aside></div></div>;
}

function ToolUsagePage({ task, events }: { task: Task | null; events: TaskEvent[] }) {
  const catalog = [
    { name: "read_file", description: "读取仓库文本文件和行范围", permission: "只读" },
    { name: "list_files", description: "列出仓库目录中的文件", permission: "只读" },
    { name: "search_code", description: "搜索仓库内容", permission: "只读" },
    { name: "git_diff", description: "检查 Git 工作区变更", permission: "只读" },
    { name: "apply_patch", description: "修改仓库文件", permission: "写入审批" },
    { name: "run_command", description: "在隔离工作区运行受限命令", permission: "执行审批" }
  ];
  const calls = events.filter((item) => item.type === "tool.requested");
  return <div className="plugin-page"><div className="plugin-heading"><div><span className="section-kicker">BUILT-IN TOOLS</span><h1>可用工具</h1></div>{task && <StatusBadge status={task.status}/>}</div><div className="tool-catalog">{catalog.map((tool) => <div className="tool-catalog-row" key={tool.name}><span className="tool-catalog-icon"><Wrench size={16}/></span><div><strong>{tool.name}</strong><small>{tool.description}</small></div><span className="tool-catalog-permission">{tool.permission}</span><span className="tool-catalog-count">{calls.filter((item) => item.payload?.tool === tool.name).length} 次调用</span></div>)}</div></div>;
}

function NavItem({ icon, label, active, count, onClick }: { icon: React.ReactNode; label: string; active?: boolean; count?: number; onClick?: () => void }) { return <button className={`nav-item ${active ? "active" : ""}`} onClick={onClick}>{icon}<span>{label}</span>{count !== undefined && <small>{count}</small>}</button>; }
function Metric({ icon, label, value, detail, tone }: { icon: React.ReactNode; label: string; value: string; detail: string; tone: string }) { return <div className="metric-card"><div className={`metric-icon ${tone}`}>{icon}</div><div><span>{label}</span><strong>{value}</strong><small>{detail}</small></div></div>; }
function TaskComposer({ onCreated }: { onCreated: (prompt: string, model: string) => Promise<boolean> }) { const [prompt, setPrompt] = useState(""); const [model, setModel] = useState("gpt-4.1-mini"); const [submitting, setSubmitting] = useState(false); const submit = async (event: FormEvent) => { event.preventDefault(); if (!prompt.trim() || submitting) return; setSubmitting(true); try { if (await onCreated(prompt.trim(), model)) setPrompt(""); } finally { setSubmitting(false); } }; return <form className="task-composer" onSubmit={(event) => void submit(event)}><div className="composer-top"><span className="composer-badge"><Sparkles size={14}/>Agent 任务</span></div><textarea id="task-composer" value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder="描述需要修改或排查的代码问题..." rows={3}/><div className="composer-footer"><div className="composer-run"><select value={model} onChange={(e) => setModel(e.target.value)}><option>gpt-4.1-mini</option><option>gpt-4o</option><option>deepseek-chat</option></select><button className="run-button" type="submit" disabled={submitting || !prompt.trim()}>{submitting ? <Loader2 size={14}/> : <Play size={14} fill="currentColor"/>}{submitting ? "创建中" : "运行任务"}</button></div></div></form>; }
function SessionRow({ task, selected, onClick }: { task: Task; selected: boolean; onClick: () => void }) { return <button className={`session-row ${selected ? "selected" : ""}`} onClick={onClick}><div className={`session-status ${task.status.toLowerCase()}`}><StatusIcon status={task.status}/></div><div className="session-main"><strong>{task.prompt}</strong><div><span>{task.model}</span><span className="session-separator">·</span><span>{new Date(task.createdAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span></div></div><StatusBadge status={task.status}/><MoreHorizontal className="row-more" size={16}/></button>; }
function TaskCapsule({ task, events, artifacts, approvals, onApprove, onCancel, onRetry }: { task: Task | null; events: TaskEvent[]; artifacts: TaskArtifact[]; approvals: TaskApproval[]; onApprove: (approvalId: string, approved: boolean) => void; onCancel: () => void; onRetry: () => void }) {
  const [detail, setDetail] = useState<TaskArtifactDetail | null>(null);
  const [patchLoading, setPatchLoading] = useState(false);
  const [patchError, setPatchError] = useState("");
  const [selectedArtifact, setSelectedArtifact] = useState<TaskArtifact | null>(null);
  const checkpoint = artifacts.find((artifact) => artifact.kind === "checkpoint");
  const pending = approvals.filter((approval) => approval.status === "PENDING");
  const evidence = events.filter((item) => !["agent.message.delta", "usage.updated"].includes(item.type)).slice(-5).reverse();
  const loadPatch = async (artifact: TaskArtifact) => {
    if (!task) return null;
    if (detail?.id === artifact.id) return detail;
    setPatchLoading(true);
    setPatchError("");
    try {
      const value = await api<TaskArtifactDetail>(`/api/tasks/${task.id}/artifacts/${artifact.kind}`);
      setDetail(value);
      return value;
    } catch (cause) { setPatchError(`无法读取 ${artifact.kind}：${String(cause)}`); return null; }
    finally { setPatchLoading(false); }
  };
  const downloadPatch = async (artifact: TaskArtifact) => {
    const value = await loadPatch(artifact);
    if (!value) return;
    const url = URL.createObjectURL(new Blob([value.patch], { type: "text/x-diff" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `${task?.id || "proofcode"}-${artifact.kind}.patch`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  };
  return <section className="capsule-card">
    <div className="capsule-head"><div><span className="section-kicker">TASK CAPSULE</span><h2>{task?.prompt || "选择一个会话"}</h2></div><div className="capsule-actions">{task && <StatusBadge status={task.status} />}{task && ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(task.status) && <button className="cancel-button" onClick={onCancel}><Square size={13}/>停止</button>}{task && ["FAILED", "CANCELLED"].includes(task.status) && <button className="retry-button" onClick={onRetry}><RefreshCw size={13}/>重试</button>}</div></div>
    {task ? <>
      <div className="capsule-meta"><div><span>MODEL</span><strong>{task.model}</strong></div><div><span>BRANCH</span><strong><GitBranch size={13}/> {checkpoint?.branch || "待生成"}</strong></div><div><span>EVENTS</span><strong>{events.length}</strong></div></div>
      {pending.length > 0 && <div className="approval-box"><div className="evidence-title"><span>需要你的审批</span><small>{pending.length} 个请求</small></div>{pending.map((approval) => <div className="approval-item" key={approval.id}><div className="approval-item-head"><strong>{approval.tool}</strong><small>{approval.risk} · {approval.callId}</small></div><pre className="approval-arguments">{JSON.stringify(approval.arguments, null, 2)}</pre><div className="approval-buttons"><button className="primary-button" onClick={() => onApprove(approval.id, true)}>批准</button><button className="secondary-button" onClick={() => onApprove(approval.id, false)}>拒绝</button></div></div>)}</div>}
      <div className="evidence-title"><span>执行事件</span><small>{events.length} 个事件</small></div>
      <div className="evidence-list">{evidence.map((item) => <div className="capsule-event" key={item.sequence}><span>{item.type}</span><time>{new Date(item.timestamp).toLocaleTimeString()}</time></div>)}{events.length === 0 && <div className="event-empty">暂无执行事件</div>}</div>
      {artifacts.filter((artifact) => artifact.kind === "checkpoint" || artifact.kind === "recovery").map((artifact) => <div className={`capsule-result ${artifact.kind === "recovery" ? "recovery" : ""}`} key={artifact.id}><FileCode2 size={15}/><span>{artifact.kind === "recovery" ? "恢复补丁" : "Checkpoint"} {artifact.commitHash?.slice(0, 8) || ""}</span><button className="text-button" onClick={() => { setSelectedArtifact(artifact); void loadPatch(artifact); }}>审查变更</button><button className="text-button" onClick={() => void downloadPatch(artifact)} disabled={patchLoading}>下载 patch</button></div>)}
      {patchError && <div className="capsule-error"><XCircle size={15}/>{patchError}</div>}
      {task.error && <div className="capsule-error"><XCircle size={15}/>{task.error}</div>}
      {task.result && <div className="capsule-result"><CheckCircle2 size={15}/>{task.result}</div>}
    </> : <div className="capsule-empty"><CircleDot size={22}/><p>选择任务后查看事件、审批与变更</p></div>}
    {selectedArtifact && <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) setSelectedArtifact(null); }}><section className="diff-dialog" role="dialog" aria-modal="true" aria-label="变更审查"><div className="diff-dialog-head"><div><span className="section-kicker">{selectedArtifact.kind.toUpperCase()}</span><h2>变更审查</h2><small>{selectedArtifact.commitHash} · {selectedArtifact.branch}</small></div><button className="icon-button" title="关闭" onClick={() => setSelectedArtifact(null)}><X size={18}/></button></div>{patchLoading && <div className="diff-loading">正在读取变更...</div>}{patchError && <div className="capsule-error">{patchError}</div>}{detail?.id === selectedArtifact.id && <pre className="diff-content">{detail.patch || "无文件变更"}</pre>}<div className="diff-dialog-actions"><button className="secondary-button" onClick={() => void downloadPatch(selectedArtifact)} disabled={detail?.id !== selectedArtifact.id || patchLoading}>下载 patch</button><button className="primary-button" onClick={() => setSelectedArtifact(null)}>关闭</button></div></section></div>}
  </section>;
}
function ActivityRow({ icon, title, detail, time, tone }: { icon: React.ReactNode; title: string; detail: string; time: string; tone: string }) { return <div className="activity-row"><span className={`activity-icon ${tone}`}>{icon}</span><div><strong>{title}</strong><small>{detail}</small></div><time>{time}</time></div>; }
function StatusIcon({ status }: { status: string }) { return status === "SUCCEEDED" ? <CheckCircle2 size={16}/> : status === "FAILED" || status === "CANCELLED" ? <XCircle size={16}/> : <CircleDot size={16}/>; }
function StatusBadge({ status }: { status: string }) { const labels: Record<string, string> = { SUCCEEDED: "已完成", VERIFYING: "验证中", RUNNING: "运行中", QUEUED: "排队中", WAITING_APPROVAL: "待审批", FAILED: "失败", CANCELLED: "已取消" }; return <span className={`status-badge ${status.toLowerCase()}`}>{labels[status] || status}</span>; }
function ActionPanel({ kind, tasks, events, artifacts, onClose }: { kind: PanelKind; tasks: Task[]; events: TaskEvent[]; artifacts: TaskArtifact[]; onClose: () => void }) {
  const [modelConfig, setModelConfig] = useState<ModelConfig | null>(null);
  const [loadingConfig, setLoadingConfig] = useState(false);
  const refreshModelConfig = async () => {
    setLoadingConfig(true);
    try {
      const value = await (window as unknown as { GetModelConfig?: () => Promise<ModelConfig> }).GetModelConfig?.();
      if (value) setModelConfig(value); else throw new Error("桌面绑定不可用，请从桌面端打开");
    } catch (error) { setModelConfig({ configured: false, provider: "", model: "", baseUrl: "", source: "Codex config.toml", hasKey: false, keyLast4: "", maxContextTokens: 183616, maxOutputTokens: 16384, maxTotalTokens: 200000, error: String(error) }); }
    finally { setLoadingConfig(false); }
  };
  useEffect(() => { if (kind === "Agent 设置") void refreshModelConfig(); }, [kind]);
  const finished = tasks.filter((task) => ["SUCCEEDED", "FAILED", "CANCELLED"].includes(task.status));
  const succeeded = tasks.filter((task) => task.status === "SUCCEEDED").length;
  const content: Record<PanelKind, { eyebrow: string; title: string; description: string }> = {
    "运行统计": { eyebrow: "RUN ANALYTICS", title: "运行统计", description: "统计来自当前项目任务和已持久化事件。" },
    "账户": { eyebrow: "ACCOUNT", title: "本地账户", description: "当前使用本地工作区身份。API Token 保存在当前浏览器的本地存储中。" },
    "代码证据": { eyebrow: "CODE EVIDENCE", title: "代码证据", description: "当前任务的工具调用、验证报告与变更 artifact。" },
    "验证中心": { eyebrow: "VERIFICATION", title: "验证中心", description: "当前任务通过事件记录的验证结果。" },
    "最近活动": { eyebrow: "ACTIVITY LOG", title: "最近活动", description: "当前任务最近的持久化事件。" },
    "Agent 设置": { eyebrow: "AGENT CONFIG", title: "Agent 设置", description: "读取桌面端提供的模型配置。执行权限由 Runner 环境策略控制。" }
  };
  const current = content[kind];
  return <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}><section className="action-panel"><div className="panel-head"><div><span className="section-kicker">{current.eyebrow}</span><h2>{current.title}</h2></div><button className="icon-button" title="关闭" onClick={onClose}><X size={17}/></button></div><p className="panel-description">{current.description}</p>{kind === "运行统计" && <div className="panel-stats"><div><span>任务</span><strong>{tasks.length}</strong></div><div><span>已结束</span><strong>{finished.length}</strong></div><div><span>成功率</span><strong>{finished.length ? `${Math.round(succeeded / finished.length * 100)}%` : "--"}</strong></div><div><span>事件</span><strong>{events.length}</strong></div><div><span>Artifacts</span><strong>{artifacts.length}</strong></div></div>}{kind === "Agent 设置" && <div className="model-config"><div className="panel-stats"><div><span>配置来源</span><strong>{modelConfig?.source || "读取中"}</strong></div><div><span>Provider</span><strong>{modelConfig?.provider || "未读取"}</strong></div><div><span>模型</span><strong>{modelConfig?.model || "未配置"}</strong></div><div><span>API Key</span><strong>{modelConfig?.hasKey ? `已配置（${modelConfig.keyLast4}）` : "未配置"}</strong></div><div><span>输入预算</span><strong>{(modelConfig?.maxContextTokens || 0).toLocaleString()} token</strong></div><div><span>输出预算</span><strong>{(modelConfig?.maxOutputTokens || 0).toLocaleString()} token</strong></div></div>{modelConfig?.error && <div className="capsule-error"><XCircle size={15}/>{modelConfig.error}</div>}<button className="secondary-button" onClick={() => void refreshModelConfig()} disabled={loadingConfig}><RefreshCw size={14}/>{loadingConfig ? "读取中" : "刷新 Codex 配置"}</button></div>}{kind === "代码证据" && <div className="panel-stats"><div><span>工具调用</span><strong>{events.filter((item) => item.type.startsWith("tool.")).length}</strong></div><div><span>验证事件</span><strong>{events.filter((item) => item.type === "verification.completed").length}</strong></div><div><span>变更 artifact</span><strong>{artifacts.length}</strong></div></div>}{kind === "验证中心" && <div className="panel-stats"><div><span>验证事件</span><strong>{events.filter((item) => item.type === "verification.completed").length}</strong></div><div><span>失败事件</span><strong>{events.filter((item) => item.type.endsWith("failed")).length}</strong></div><div><span>当前任务</span><strong>{tasks.find((task) => task.status === "VERIFYING")?.status || "无"}</strong></div></div>}<div className="panel-actions"><button className="secondary-button" onClick={onClose}>关闭</button></div></section></div>;
}
function ProjectForm({ onClose, onCreated }: { onClose: () => void; onCreated: (project: Project) => void }) { const [name, setName] = useState(""); const [url, setUrl] = useState(""); const [branch, setBranch] = useState("main"); const [error, setError] = useState(""); const submit = async (event: FormEvent) => { event.preventDefault(); try { onCreated(await api<Project>("/api/projects", { method: "POST", body: JSON.stringify({ name, repositoryUrl: url, defaultBranch: branch }) })); } catch (cause) { setError(String(cause)); } }; return <div className="modal-backdrop"><form className="modal" onSubmit={submit}><div className="modal-head"><div><span className="section-kicker">NEW PROJECT</span><h2>连接一个代码库</h2></div><button type="button" className="icon-button" onClick={onClose} title="关闭"><X size={17}/></button></div><label>项目名称<input required value={name} onChange={(e) => setName(e.target.value)} placeholder="例如 proofcode"/></label><label>Git 仓库 URL<input required type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://github.com/org/repo.git"/></label><label>默认分支<input required value={branch} onChange={(e) => setBranch(e.target.value)}/></label>{error && <div className="capsule-error"><XCircle size={15}/>{error}</div>}<div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>取消</button><button className="primary-button" type="submit"><Plus size={15}/>连接项目</button></div></form></div>; }

const root = document.getElementById("root");
if (!root) throw new Error("ProofCode root element was not found");
createRoot(root).render(<App />);
