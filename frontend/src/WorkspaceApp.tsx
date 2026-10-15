import { FormEvent, useEffect, useRef, useState } from "react";
import { Activity, Bot, CheckCircle2, ChevronDown, CircleDot, Code2, Globe, Database, FolderGit2, Gauge, GitBranch, Layers3, Loader2, Menu, MessageSquare, Plus, RefreshCw, Search, Settings2, ShieldCheck, Sparkles, Workflow, Wrench, X, XCircle } from "lucide-react";
import { api, createScratchProject, desktopApp, pendingBootstrap, post, registerProject, scopedPath, token } from "./api";
import { ActionPanel, AgentWorkspace, StatusBadge, ToolchainPage, ToolUsagePage } from "./TaskViews";
import type { PanelKind } from "./TaskViews";
import type { Conversation, ConversationMessage, ExecutionMode, LocalProject, Project, Scope, Task, TaskApproval, TaskArtifact, TaskEvent, Workspace } from "./types";
import { DataWorkbench } from "./DataWorkbench";
import { IDEPage, GitPage, BrowserPage, ReviewPage } from "./DeveloperPages";

const blankProject: Project = { id: "", name: "新工程对话", defaultBranch: "main", sourceKind: "SCRATCH" };
const savedScopeKey = "proofcode.selected-scope";
function readSavedScope(): Partial<Scope> {
  try { return JSON.parse(localStorage.getItem(savedScopeKey) || "{}"); } catch { return {}; }
}
function scopeEquals(left: Partial<Scope>, right: Partial<Scope>): boolean {
  return left.projectId === right.projectId && left.workspaceId === right.workspaceId && left.conversationId === right.conversationId;
}
const isActiveTask = (task: Task) => ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(task.status);

export function WorkspaceApp() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [project, setProject] = useState<Project>(blankProject);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [conversationTree, setConversationTree] = useState<Record<string, Conversation[]>>({});
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [conversation, setConversation] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<ConversationMessage[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [task, setTask] = useState<Task | null>(null);
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [runDataTaskId, setRunDataTaskId] = useState("");
  const [artifacts, setArtifacts] = useState<TaskArtifact[]>([]);
  const [approvals, setApprovals] = useState<TaskApproval[]>([]);
  const [localProjects, setLocalProjects] = useState<LocalProject[]>([]);
  const [showProjectForm, setShowProjectForm] = useState(false);
  const [showWorkspaceForm, setShowWorkspaceForm] = useState(false);
  const [notice, setNotice] = useState("");
  const [activeNav, setActiveNav] = useState("工作台");
  const [query, setQuery] = useState("");
  const [backendOnline, setBackendOnline] = useState(false);
  const [collapsed, setCollapsed] = useState(() => window.matchMedia("(max-width:760px)").matches);
  const [openPanel, setOpenPanel] = useState<PanelKind | null>(null);
  const [scopeLoading, setScopeLoading] = useState(false);
  const [creatingConversation, setCreatingConversation] = useState(false);
  const [treeRevision, setTreeRevision] = useState(0);
  const scopeRef = useRef<Partial<Scope>>({});
  const projectRef = useRef<Project>(blankProject);
  const scopeGeneration = useRef(0);
  const projectRequest = useRef(0);
  const taskRequest = useRef(0);
  const messageRequest = useRef(0);
  const submission = useRef<{ identity: string; key: string; sourceSnapshotId?: string } | null>(null);
  const retainedBootstrap = pendingBootstrap();

  const clearRun = () => { setTasks([]); setTask(null); setEvents([]); setArtifacts([]); setApprovals([]); setMessages([]); };
  const beforeNavigation = () => window.dispatchEvent(new Event("proofcode:before-navigation", {cancelable:true}));
  const chooseProject = (next: Project) => {
    if (!beforeNavigation()) return;
    scopeGeneration.current++;
    projectRef.current = next;
    scopeRef.current = { projectId: next.id };
    setProject(next); setWorkspace(null); setConversation(null); setWorkspaces([]); setConversationTree({}); clearRun();
    if (next.id) localStorage.setItem(savedScopeKey, JSON.stringify({ projectId: next.id }));
    else localStorage.removeItem(savedScopeKey);
  };
  const chooseConversation = (nextWorkspace: Workspace, nextConversation: Conversation) => {
    if (!beforeNavigation()) return;
    if (nextConversation.projectId !== projectRef.current.id || nextConversation.workspaceId !== nextWorkspace.id) return;
    scopeGeneration.current++;
    scopeRef.current = { projectId: projectRef.current.id, workspaceId: nextWorkspace.id, conversationId: nextConversation.id };
    setWorkspace(nextWorkspace); setConversation(nextConversation); clearRun();
    localStorage.setItem(savedScopeKey, JSON.stringify(scopeRef.current)); setActiveNav("工作台");
  };
  const refreshLocals = async () => {
    try { const values = await desktopApp()?.ListLocalProjects?.(); if (values) setLocalProjects(values); } catch { /* Remote projects remain available. */ }
  };
  const loadProjects = async () => {
    const generation = scopeGeneration.current, request = ++projectRequest.current;
    const currentRequest = () => generation === scopeGeneration.current && request === projectRequest.current;
    try {
      const values = await api<Project[]>("/api/projects");
      if (!currentRequest()) return;
      setProjects(values); setBackendOnline(true);
      const saved = readSavedScope();
      const current = projectRef.current;
      const next = values.find((value) => value.id === (current.id || saved.projectId));
      if (next && current.id !== next.id) {
        chooseProject(next);
        if (saved.projectId === next.id) localStorage.setItem(savedScopeKey, JSON.stringify(saved));
      } else if (next) { projectRef.current = next; setProject(next); }
      else if (current.id) chooseProject(blankProject);
    } catch (cause) { if (currentRequest()) { setBackendOnline(false); setNotice(`无法加载项目：${String(cause)}`); } }
  };
  const loadTasks = async (scope = scopeRef.current) => {
    if (!scope.projectId || !scope.conversationId) return;
    const generation = scopeGeneration.current, request = ++taskRequest.current;
    const currentRequest = () => generation === scopeGeneration.current && request === taskRequest.current && scopeEquals(scopeRef.current, scope);
    try {
      const values = await api<Task[]>(`/api/tasks?projectId=${scope.projectId}&conversationId=${scope.conversationId}`);
      if (!currentRequest()) return;
      setTasks(values); setTask((old) => values.find((value) => value.id === old?.id) || values[0] || null); setBackendOnline(true);
    } catch { if (currentRequest()) setBackendOnline(false); }
  };
  const loadMessages = async (scope = scopeRef.current) => {
    if (!scope.projectId || !scope.workspaceId || !scope.conversationId) return;
    const generation = scopeGeneration.current, request = ++messageRequest.current;
    const currentRequest = () => generation === scopeGeneration.current && request === messageRequest.current && scopeEquals(scopeRef.current, scope);
    try {
      const values: ConversationMessage[] = [];
      let cursor = 0, page: ConversationMessage[];
      do {
        page = await api<ConversationMessage[]>(`${scopedPath(scope.projectId)}/workspaces/${scope.workspaceId}/conversations/${scope.conversationId}/messages?afterSequence=${cursor}`);
        if (!currentRequest()) return;
        if (page.some((item) => item.sequence <= cursor)) throw new Error("消息分页未前进");
        values.push(...page);
        if (page.length) cursor = Math.max(...page.map((item) => item.sequence));
      } while (page.length === 500);
      if (currentRequest()) setMessages(values.sort((a, b) => a.sequence - b.sequence));
    } catch (cause) { if (currentRequest()) setNotice(`会话恢复失败：${String(cause)}`); }
  };
  useEffect(() => { void loadProjects(); void refreshLocals(); }, []);
  useEffect(() => {
    if (!project.id) { setScopeLoading(false); return; }
    let active = true;
    const generation = scopeGeneration.current;
    const projectId = project.id;
    setScopeLoading(true);
    const load = async () => {
      try {
        const defaultScope = await post<Scope>(`${scopedPath(projectId)}/default-scope`, {});
        const values = await api<Workspace[]>(`${scopedPath(projectId)}/workspaces`);
        const children = await Promise.all(values.map(async (value) => [value.id, await api<Conversation[]>(`${scopedPath(projectId)}/workspaces/${value.id}/conversations`)] as const));
        if (!active || generation !== scopeGeneration.current || projectRef.current.id !== projectId) return;
        const tree = Object.fromEntries(children);
        setWorkspaces(values); setConversationTree(tree);
        const saved = readSavedScope();
        const requested = saved.projectId === projectId ? saved : defaultScope;
        const nextWorkspace = values.find((value) => value.id === requested.workspaceId) || values.find((value) => value.id === defaultScope.workspaceId);
        const nextConversation = nextWorkspace && (tree[nextWorkspace.id].find((value) => value.id === requested.conversationId) || tree[nextWorkspace.id].find((value) => value.id === defaultScope.conversationId) || tree[nextWorkspace.id][0]);
        if (nextWorkspace && nextConversation) {
          scopeRef.current = { projectId, workspaceId: nextWorkspace.id, conversationId: nextConversation.id };
          setWorkspace(nextWorkspace); setConversation(nextConversation); localStorage.setItem(savedScopeKey, JSON.stringify(scopeRef.current));
        }
        setBackendOnline(true);
      } catch (cause) { if (active && generation === scopeGeneration.current) setNotice(`工作区加载失败：${String(cause)}`); }
      finally { if (active) setScopeLoading(false); }
    };
    void load(); return () => { active = false; };
  }, [project.id, treeRevision]);
  useEffect(() => {
    if (!conversation?.id) return;
    const scope = { ...scopeRef.current };
    void loadTasks(scope); void loadMessages(scope);
    const poll = setInterval(() => { void loadMessages(scope); }, 4000);
    return () => clearInterval(poll);
  }, [conversation?.id]);
  useEffect(() => {
    if (!task) { setEvents([]); setArtifacts([]); setApprovals([]); return; }
    let active = true;
    const taskId = task.id;
    const scope = { ...scopeRef.current };
    const generation = scopeGeneration.current;
    const current = () => active && generation === scopeGeneration.current && scopeEquals(scopeRef.current, scope);
    const known = new Map<number, TaskEvent>();
    let cursor = 0, syncing = false, needsSync = false, retryDelay = 1000;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
    let socket: WebSocket | undefined;
    setEvents([]); setArtifacts([]); setApprovals([]); setRunDataTaskId(taskId);
    const merge = (batch: TaskEvent[]) => {
      if (!current()) return;
      for (const item of batch) known.set(item.sequence, item);
      while (known.has(cursor + 1)) cursor++;
      setEvents([...known.values()].sort((a, b) => a.sequence - b.sequence));
    };
    const refreshRelated = () => {
      void api<TaskArtifact[]>(`/api/tasks/${taskId}/artifacts`).then((values) => { if (current()) setArtifacts(values); }).catch(() => undefined);
      void api<TaskApproval[]>(`/api/tasks/${taskId}/approvals`).then((values) => { if (current()) setApprovals(values); }).catch(() => undefined);
    };
    const sync = async () => {
      needsSync = true; if (syncing) return; syncing = true;
      try { while (current() && needsSync) {
        needsSync = false; let page: TaskEvent[];
        do { page = await api<TaskEvent[]>(`/api/tasks/${taskId}/events?afterSequence=${cursor}`); merge(page); } while (current() && page.length === 500);
      } } catch { /* Reconnection reloads durable events. */ } finally { syncing = false; }
    };
    const connect = () => {
      if (!current()) return;
      socket = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws/tasks/${taskId}?token=${encodeURIComponent(token())}`);
      socket.onopen = () => { retryDelay = 1000; void sync(); refreshRelated(); void loadTasks(scope); void loadMessages(scope); };
      socket.onmessage = (message) => {
        if (!current()) return;
        try {
          const value = JSON.parse(message.data) as TaskEvent;
          if (value.sequence > cursor + 1) void sync(); merge([value]);
          if (["task.started", "task.completed", "task.failed", "task.cancelled", "tool.approval_required", "checkpoint.created", "recovery.created", "verification.completed", "agent.message.completed"].includes(value.type)) {
            refreshRelated(); void loadTasks(scope); void loadMessages(scope);
          }
        } catch { return; }
      };
      socket.onclose = () => { if (current()) { reconnectTimer = setTimeout(connect, retryDelay); retryDelay = Math.min(retryDelay * 2, 15000); } };
    };
    refreshRelated(); void sync(); if (!desktopApp()?.ProxyRequest) connect();
    const poll = setInterval(() => { void sync(); refreshRelated(); if (!socket || socket.readyState !== WebSocket.OPEN) void loadTasks(scope); }, desktopApp()?.ProxyRequest ? 3000 : 5000);
    return () => { active = false; clearInterval(poll); if (reconnectTimer) clearTimeout(reconnectTimer); socket?.close(); };
  }, [task?.id]);

  const addProject = (value: Project) => { setProjects((old) => [value, ...old.filter((item) => item.id !== value.id)]); chooseProject(value); setActiveNav("工作台"); void refreshLocals(); };
  const ensureProject = async (title: string) => {
    if (projectRef.current.id) return projectRef.current;
    const generation = scopeGeneration.current;
    const value = await createScratchProject(title.slice(0, 48));
    if (generation === scopeGeneration.current) addProject(value);
    else setProjects((old) => [value, ...old.filter((item) => item.id !== value.id)]);
    return value;
  };
  const ensureScope = async (value: Project): Promise<Scope> => {
    const current = scopeRef.current;
    if (current.projectId === value.id && current.workspaceId && current.conversationId) return current as Scope;
    const generation = scopeGeneration.current;
    const resolved = await post<Scope>(`${scopedPath(value.id)}/default-scope`, {});
    if (generation === scopeGeneration.current && projectRef.current.id === value.id) { scopeRef.current = resolved; localStorage.setItem(savedScopeKey, JSON.stringify(resolved)); }
    return resolved;
  };
  const createTask = async (prompt: string, model: string, executionMode: ExecutionMode = "CODE") => {
    try {
      const value = await ensureProject(prompt);
      const resolved = await ensureScope(value);
      const generation = scopeGeneration.current;
      const identity = JSON.stringify([resolved, prompt, model, executionMode]);
      if (submission.current?.identity !== identity) submission.current = { identity, key: crypto.randomUUID() };
      const pending = submission.current;
      if (executionMode === "CODE" && value.sourceKind === "LOCAL_FOLDER" && !pending.sourceSnapshotId) {
        if (!value.localHandle || !desktopApp()?.CaptureProjectSource) throw new Error("本机源码只能在已绑定此工程目录的桌面端执行");
        const capture = await desktopApp()!.CaptureProjectSource!(value.localHandle);
        const uploaded = await post<{ id: string }>(`${scopedPath(value.id)}/sources`, { workspaceId: resolved.workspaceId, ...capture });
        pending.sourceSnapshotId = uploaded.id;
      }
      const created = await post<Task>("/api/tasks", { ...resolved, prompt, model, executionMode, sourceSnapshotId: pending.sourceSnapshotId || null }, pending.key);
      submission.current = null;
      if (generation === scopeGeneration.current && scopeEquals(scopeRef.current, resolved)) { setTasks((old) => [created, ...old.filter((item) => item.id !== created.id)]); setTask(created); void loadMessages(resolved); setBackendOnline(true); setNotice(""); }
      return true;
    } catch (cause) { setNotice(`发送失败，可重试同一请求：${String(cause)}`); return false; }
  };
  const newConversation = async () => {
    if (creatingConversation) return; setCreatingConversation(true);
    try {
      const value = await ensureProject("未命名工程"); const resolved = await ensureScope(value);
      const generation = scopeGeneration.current;
      const created = await post<Conversation>(`${scopedPath(value.id)}/workspaces/${resolved.workspaceId}/conversations`, { title: `新对话 ${new Date().toLocaleString()}` });
      const currentWorkspace = workspace?.id === resolved.workspaceId ? workspace : await api<Workspace>(`${scopedPath(value.id)}/workspaces/${resolved.workspaceId}`);
      if (generation !== scopeGeneration.current || projectRef.current.id !== value.id) return;
      setConversationTree((old) => ({ ...old, [currentWorkspace.id]: [...(old[currentWorkspace.id] || []), created] }));
      chooseConversation(currentWorkspace, created);
    } catch (cause) { setNotice(`新建对话失败：${String(cause)}`); } finally { setCreatingConversation(false); }
  };
  const mutateTask = async (action: "retry" | "cancel", selectedId = task?.id) => {
    if (!selectedId) return; const scope = { ...scopeRef.current }, generation = scopeGeneration.current;
    try { await api(`/api/tasks/${selectedId}/${action}`, { method: "POST" }); if (generation !== scopeGeneration.current) return; await loadTasks(scope); await loadMessages(scope); } catch (cause) { if (generation === scopeGeneration.current && scopeEquals(scopeRef.current, scope)) setNotice(String(cause)); }
  };
  const decideApproval = async (approvalId: string, approved: boolean) => {
    if (!task) return; const scope = { ...scopeRef.current }, generation = scopeGeneration.current;
    try {
      const value = await post<TaskApproval>(`/api/tasks/${task.id}/approvals/${approvalId}`, { approved });
      if (generation !== scopeGeneration.current || !scopeEquals(scopeRef.current, scope)) return;
      setApprovals((old) => old.map((item) => item.id === value.id ? value : item)); await loadTasks(scope);
    } catch (cause) { if (generation === scopeGeneration.current && scopeEquals(scopeRef.current, scope)) setNotice(String(cause)); }
  };
  const navigate = (label: string, taskId?: string) => { if (!beforeNavigation()) return; if (taskId) { const selected = tasks.find(value => value.id === taskId); if (selected) setTask(selected); } setActiveNav(label); setOpenPanel(null); if (window.matchMedia("(max-width:760px)").matches) setCollapsed(true); };
  const local = localProjects.find((value) => value.localHandle === project.localHandle);
  const filteredTasks = tasks.filter((value) => value.prompt.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
  const totals = { active: tasks.filter(isActiveTask).length, succeeded: tasks.filter((value) => value.status === "SUCCEEDED").length, completed: tasks.filter((value) => ["SUCCEEDED", "FAILED", "CANCELLED"].includes(value.status)).length };

  return <div className={`app-shell has-route ${collapsed ? "tree-collapsed" : ""}`}>
    <header className="topbar"><div className="top-left"><button className="rail-toggle" title={collapsed ? "展开导航" : "收起导航"} onClick={() => setCollapsed((value) => !value)}><Menu size={17}/></button><div className="brand-mark">P</div><div className="brand-copy"><strong>ProofCode</strong><span>可验证编程工作台</span></div><div className="workspace-picker"><span className="workspace-dot"/>{project.name}<ChevronDown size={14}/></div></div><div className="top-center"><div className="command-search"><Search size={15}/><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索当前对话的任务"/></div></div><div className="top-right"><span className={`connection ${backendOnline ? "online" : "local"}`}><span/>{backendOnline ? "Control Plane 在线" : "Control Plane 离线"}</span><button className="icon-button" title="刷新项目与会话" onClick={() => { void loadProjects(); void loadTasks(); void loadMessages(); setTreeRevision((value) => value + 1); }}><RefreshCw size={16}/></button><button className="avatar" title="账户" onClick={() => setOpenPanel("账户")}>A</button></div></header>
    <div className={`app-body ${collapsed ? "rail-collapsed" : ""}`}><aside className={`nav-rail ${collapsed ? "collapsed" : ""}`}>
      <div className="nav-group"><NavButton icon={<Bot size={16}/>} label="编码与聊天" active={activeNav === "工作台"} onClick={() => navigate("工作台")}/><NavButton icon={<Code2 size={16}/>} label="代码 IDE" active={activeNav === "代码"} onClick={() => navigate("代码")}/><NavButton icon={<Globe size={16}/>} label="浏览器" active={activeNav === "浏览器"} onClick={() => navigate("浏览器")}/><NavButton icon={<GitBranch size={16}/>} label="Git" active={activeNav === "Git"} onClick={() => navigate("Git")}/><NavButton icon={<ShieldCheck size={16}/>} label="代码审查" active={activeNav === "审查"} onClick={() => navigate("审查")}/><NavButton icon={<Layers3 size={16}/>} label="运行概览" active={activeNav === "概览"} onClick={() => navigate("概览")}/><NavButton icon={<Database size={16}/>} label="数据库与缓存" active={activeNav === "数据"} onClick={() => navigate("数据")}/><NavButton icon={<Workflow size={16}/>} label="执行链" active={activeNav === "执行链"} onClick={() => navigate("执行链")}/><NavButton icon={<Wrench size={16}/>} label="可用工具" active={activeNav === "插件"} onClick={() => navigate("插件")}/></div>
      <div className="scope-tree"><div className="nav-label project-label">工程 / 对话<button className="tiny-button" title="连接或新建工程" onClick={() => setShowProjectForm(true)}><Plus size={14}/></button></div><button className={`tree-blank ${!project.id ? "selected" : ""}`} onClick={() => { chooseProject(blankProject); navigate("工作台"); }}><Plus size={14}/><span>新建独立工程对话</span></button>{projects.map((value) => <div className="project-tree" key={value.id}><button className={`mini-project ${project.id === value.id ? "selected" : ""}`} onClick={() => chooseProject(value)}><FolderGit2 size={14}/><span>{value.name}</span></button>{value.id === project.id && <div className="workspace-tree">{scopeLoading && <small className="scope-loading"><Loader2 size={12}/>加载工作区</small>}{workspaces.map((item) => <div key={item.id}><div className={`tree-workspace ${workspace?.id === item.id ? "current" : ""}`}><GitBranch size={12}/><span>{item.name}</span></div>{(conversationTree[item.id] || []).map((chat) => <button className={`tree-conversation ${conversation?.id === chat.id ? "selected" : ""}`} key={chat.id} title={chat.title} onClick={() => chooseConversation(item, chat)}><MessageSquare size={12}/><span>{chat.title === "Legacy" ? "默认对话" : chat.title}</span></button>)}</div>)}<div className="tree-create-actions"><button onClick={() => void newConversation()} disabled={creatingConversation}><Plus size={12}/>新对话</button><button onClick={() => setShowWorkspaceForm(true)}><GitBranch size={12}/>工作区</button></div></div>}</div>)}</div>
      <div className="nav-bottom"><NavButton icon={<Settings2 size={16}/>} label="设置" onClick={() => setOpenPanel("Agent 设置")}/><div className="privacy-note"><ShieldCheck size={14}/><span>工程隔离 · 数据审批<br/>持久会话与操作审计</span></div></div>
    </aside></div>
    <div className={`route-overlay ${activeNav === "工作台" ? "chat-route" : ""}`}><div className="scope-banner"><span><FolderGit2 size={13}/>{project.name}</span><span>/ {workspace?.name || "自动工程"}</span><span>/ {conversation?.title === "Legacy" ? "默认对话" : conversation?.title || "首次发送时创建"}</span>{local && <span className="local-project-path" title={local.path}>{local.path}</span>}{project.id && <button className="text-button" onClick={() => void newConversation()} disabled={creatingConversation}><Plus size={12}/>新对话</button>}</div>
      {activeNav === "工作台" && <AgentWorkspace project={project} conversation={conversation} messages={messages} task={task} tasks={tasks} events={runDataTaskId === task?.id ? events : []} artifacts={runDataTaskId === task?.id ? artifacts : []} approvals={runDataTaskId === task?.id ? approvals : []} onCreated={createTask} onSelectTask={setTask} onApprove={decideApproval} onCancel={(id?: string) => void mutateTask("cancel", id)} onRetry={() => void mutateTask("retry")} onNavigate={navigate} onNotice={setNotice}/>}
      {activeNav === "代码" && <IDEPage key={project.id} project={project} task={task}/>}
      {activeNav === "Git" && <GitPage key={project.id} project={project}/>}
      {activeNav === "浏览器" && <BrowserPage key={project.id} project={project}/>}
      {activeNav === "审查" && <ReviewPage key={`${project.id}:${task?.id || "empty"}`} project={project} task={task} tasks={tasks} artifacts={runDataTaskId === task?.id ? artifacts : []} events={runDataTaskId === task?.id ? events : []} approvals={runDataTaskId === task?.id ? approvals : []} onSelect={setTask} onApprove={decideApproval} onCancel={(id?: string) => void mutateTask("cancel", id)} onRetry={() => void mutateTask("retry")}/>}
      {activeNav === "数据" && (project.id ? <DataWorkbench key={project.id} project={project} onNotice={setNotice}/> : <section className="data-empty-page"><Database size={35}/><h1>为工程连接数据库与缓存</h1><p>先选择工程，或直接开始新对话自动创建独立工程。</p><button className="primary-button" onClick={() => setShowProjectForm(true)}><Plus size={15}/>新建工程</button></section>)}
      {activeNav === "执行链" && <ToolchainPage task={task} events={runDataTaskId === task?.id ? events : []}/>}{activeNav === "插件" && <ToolUsagePage task={task} events={runDataTaskId === task?.id ? events : []}/>}
      {activeNav === "概览" && <section className="overview-page"><div className="page-heading"><div><div className="breadcrumb">{workspace?.name} / {conversation?.title}</div><h1>运行概览</h1><p>当前对话的持久任务与执行记录。</p></div><button className="secondary-button" onClick={() => setOpenPanel("运行统计")}><Gauge size={15}/>运行统计</button></div><div className="metric-grid"><Metric icon={<Activity size={17}/>} label="活跃任务" value={totals.active}/><Metric icon={<CheckCircle2 size={17}/>} label="已完成" value={totals.succeeded}/><Metric icon={<ShieldCheck size={17}/>} label="成功率" value={totals.completed ? `${Math.round(totals.succeeded / totals.completed * 100)}%` : "--"}/><Metric icon={<Sparkles size={17}/>} label="持久消息" value={messages.length}/></div><div className="overview-tasks">{filteredTasks.map((value) => <button key={value.id} className="overview-task" onClick={() => { setTask(value); navigate("工作台"); }}><span><strong>{value.prompt}</strong><small>{value.executionMode === "CHAT" ? "普通聊天" : "代码任务"} · {value.model} · {new Date(value.createdAt).toLocaleString()}</small></span><StatusBadge status={value.status}/></button>)}{!filteredTasks.length && <div className="empty-state">当前对话还没有任务</div>}</div></section>}
    </div>
    {showProjectForm && <ProjectForm onClose={() => setShowProjectForm(false)} onCreated={(value) => { addProject(value); setShowProjectForm(false); }}/>} {showWorkspaceForm && project.id && <WorkspaceForm project={project} onClose={() => setShowWorkspaceForm(false)} onCreated={() => { setShowWorkspaceForm(false); setTreeRevision((value) => value + 1); }}/>} {openPanel && <ActionPanel kind={openPanel} tasks={tasks} events={runDataTaskId === task?.id ? events : []} artifacts={runDataTaskId === task?.id ? artifacts : []} onClose={() => setOpenPanel(null)}/>}
    {retainedBootstrap && !project.id && <div className="bootstrap-retry"><CircleDot size={14}/>上次工程注册未完成；再次发送会继续同一工程。</div>}{notice && <div className="toast" role="status"><XCircle size={16}/><span>{notice}</span><button aria-label="关闭提示" onClick={() => setNotice("")}><X size={14}/></button></div>}
  </div>;
}

function NavButton({ icon, label, active, onClick }: { icon: React.ReactNode; label: string; active?: boolean; onClick: () => void }) { return <button className={`nav-item ${active ? "active" : ""}`} onClick={onClick} title={label}>{icon}<span>{label}</span></button>; }
function Metric({ icon, label, value }: { icon: React.ReactNode; label: string; value: number | string }) { return <div className="metric-card"><div className="metric-icon blue">{icon}</div><div><span>{label}</span><strong>{value}</strong><small>当前对话</small></div></div>; }
function ProjectForm({ onClose, onCreated }: { onClose: () => void; onCreated: (project: Project) => void }) {
  const [name, setName] = useState(""); const [url, setUrl] = useState(""); const [branch, setBranch] = useState("main");
  const [sourceKind, setSourceKind] = useState<"REMOTE_REPOSITORY" | "LOCAL_FOLDER" | "SCRATCH">("SCRATCH");
  const [error, setError] = useState(""); const [busy, setBusy] = useState(false); const [local, setLocal] = useState<LocalProject | null>(null);
  const chooseFolder = async () => { try { const value = await desktopApp()?.RegisterLocalProject?.(); if (!value) throw new Error("请从桌面端导入本机目录"); setLocal(value); if (!name) setName(value.name); } catch (cause) { setError(String(cause)); } };
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (busy) return; setBusy(true); setError("");
    try {
      if (sourceKind === "SCRATCH") onCreated(await createScratchProject(name || "未命名工程"));
      else if (sourceKind === "LOCAL_FOLDER") { if (!local) throw new Error("请先选择工程目录"); onCreated(await registerProject(local, name || local.name, local.bootstrapId)); }
      else onCreated(await post<Project>("/api/projects", { name, repositoryUrl: url, defaultBranch: branch, sourceKind }, crypto.randomUUID()));
    } catch (cause) { setError(String(cause)); } finally { setBusy(false); }
  };
  return <div className="modal-backdrop"><form className="modal" onSubmit={(event) => void submit(event)}><div className="modal-head"><div><span className="section-kicker">NEW PROJECT</span><h2>新建或连接工程</h2></div><button type="button" className="icon-button" onClick={onClose} title="关闭"><X size={17}/></button></div><label>项目名称<input required maxLength={120} value={name} onChange={(event) => setName(event.target.value)} placeholder="例如 订单管理工具"/></label><label>项目来源<select value={sourceKind} onChange={(event) => setSourceKind(event.target.value as typeof sourceKind)}><option value="SCRATCH">空白工程</option><option value="REMOTE_REPOSITORY">远程 Git 仓库</option>{desktopApp()?.RegisterLocalProject && <option value="LOCAL_FOLDER">本机工程目录</option>}</select></label>{sourceKind === "SCRATCH" && <p className="form-help">{desktopApp()?.BootstrapScratchProject ? "创建桌面上的独立工程目录，并建立持久对话。" : "创建服务端空白工程，可直接聊天和生成代码。桌面端会自动创建桌面目录。"}</p>}{sourceKind === "LOCAL_FOLDER" && <div className="folder-picker"><button type="button" className="secondary-button" onClick={() => void chooseFolder()}><FolderGit2 size={15}/>选择目录</button>{local && <small>{local.path}</small>}</div>}{sourceKind === "REMOTE_REPOSITORY" && <><label>Git 仓库 URL<input required type="url" value={url} onChange={(event) => setUrl(event.target.value)} placeholder="https://github.com/org/repo.git"/></label><label>默认分支<input required value={branch} onChange={(event) => setBranch(event.target.value)}/></label></>}{error && <div className="capsule-error"><XCircle size={15}/>{error}</div>}<div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>取消</button><button className="primary-button" disabled={busy} type="submit">{busy ? <Loader2 size={15}/> : <Plus size={15}/>}创建工程</button></div></form></div>;
}
function WorkspaceForm({ project, onClose, onCreated }: { project: Project; onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState(""); const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent) => { event.preventDefault(); setBusy(true); try { const value = await post<Workspace>(`${scopedPath(project.id)}/workspaces`, { name, kind: project.sourceKind || "REMOTE_REPOSITORY" }); await post(`${scopedPath(project.id)}/workspaces/${value.id}/conversations`, { title: "新对话" }); onCreated(); } catch (cause) { setError(String(cause)); } finally { setBusy(false); } };
  return <div className="modal-backdrop"><form className="modal" onSubmit={(event) => void submit(event)}><div className="modal-head"><h2>新建工作区</h2><button type="button" className="icon-button" onClick={onClose} title="关闭"><X size={17}/></button></div><label>工作区名称<input required maxLength={120} value={name} onChange={(event) => setName(event.target.value)} placeholder="例如 功能开发"/></label><p className="form-help">工作区归属于 {project.name}，对话与执行历史单独保存。</p>{error && <div className="capsule-error">{error}</div>}<div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>取消</button><button className="primary-button" type="submit" disabled={busy}>创建工作区</button></div></form></div>;
}
