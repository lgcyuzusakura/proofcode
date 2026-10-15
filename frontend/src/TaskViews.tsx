import { FormEvent, useEffect, useRef, useState } from "react";
import { Activity, ArrowUpRight, Bot, Check, CheckCircle2, CircleDot, Code2, Database, FileCode2, GitBranch, Loader2, MessageSquare, MoreHorizontal, Play, Plus, RefreshCw, ShieldCheck, Sparkles, Square, TerminalSquare, Workflow, Wrench, X, XCircle } from "lucide-react";
import { api, desktopApp } from "./api";
import type { Conversation, ConversationMessage, ExecutionMode, Project, Task, TaskApproval, TaskArtifact, TaskArtifactDetail, TaskEvent, ModelConfig } from "./types";
import { MessageContent } from "./MessageContent";
export type PanelKind = "运行统计" | "账户" | "代码证据" | "验证中心" | "最近活动" | "Agent 设置";

export function AgentWorkspace({ project, conversation, messages, task, tasks, events, artifacts, approvals, onCreated, onSelectTask, onApprove, onCancel, onRetry, onNavigate, onNotice }: { project: Project; conversation: Conversation | null; messages: ConversationMessage[]; task: Task | null; tasks: Task[]; events: TaskEvent[]; artifacts: TaskArtifact[]; approvals: TaskApproval[]; onCreated: (prompt: string, model: string, mode: ExecutionMode) => Promise<boolean>; onSelectTask: (task: Task) => void; onApprove: (approvalId: string, approved: boolean) => void; onCancel: (taskId?: string) => void; onRetry: () => void; onNavigate: (label: string, taskId?: string) => void; onNotice: (notice: string) => void }) {
  const [prompt, setPrompt] = useState("");
  const [model, setModel] = useState(localStorage.getItem("proofcode.model") || "gpt-4.1-mini");
  const [mode, setMode] = useState<ExecutionMode>((localStorage.getItem("proofcode.execution-mode") as ExecutionMode) === "CHAT" ? "CHAT" : "CODE");
  const [submitting, setSubmitting] = useState(false);
  useEffect(() => {
    if (localStorage.getItem("proofcode.model")) return;
    void desktopApp()?.GetModelConfig?.().then((value) => { if (value.model) setModel(value.model); }).catch(() => undefined);
  }, []);
  const activeTask = tasks.find(item => ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(item.status));
  const streamEnd = useRef<HTMLDivElement>(null);
  const follow = useRef(true);
  const [liveStep, liveContent] = events.reduce<[number, string]>((current, item) => {
    if (item.type === "agent.message.completed") return [Number(item.payload?.step || current[0]), ""];
    if (item.type !== "agent.message.delta") return current;
    const step = Number(item.payload?.step || 0); return [step, (step === current[0] ? current[1] : "") + String(item.payload?.delta || "")];
  }, [0, ""]);
  useEffect(() => { if (follow.current) streamEnd.current?.scrollIntoView({block:"end"}); }, [messages, liveContent]);
  const running = tasks.some((item) => ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(item.status));
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (!prompt.trim() || !model.trim() || submitting || running) return;
    setSubmitting(true);
    try { if (await onCreated(prompt.trim(), model.trim(), mode)) setPrompt(""); } finally { setSubmitting(false); }
  };
  const renderedMessages = messages.filter((item) => item.content || item.status !== "COMPLETED");
  const streamingMessage = renderedMessages.find(item => item.role === "assistant" && item.taskId === task?.id && !item.content);
  return <div className="agent-page agent-chat-page">
    <div className="agent-page-header"><div><span className="section-kicker">CODING AGENT</span><h1>{conversation?.title === "Legacy" ? "默认对话" : conversation?.title || "开始新对话"}</h1><p>{project.name} · {conversation ? `${renderedMessages.length} 条消息` : "首次发送会自动创建独立工程"}</p></div></div>
    <section className="conversation-panel"><div className="conversation-stream" onWheel={event => { follow.current = event.deltaY >= 0; }}>
      {renderedMessages.map((message) => { const selected = message.taskId ? tasks.find((item) => item.id === message.taskId) : undefined; return <div key={message.id} className={`message-row ${message.role}`}><div className={`message-avatar ${message.role === "user" ? "user-avatar" : "agent-avatar"}`}>{message.role === "user" ? "你" : <Bot size={15}/>}</div><div className="message-bubble"><strong>{message.role === "user" ? "你" : "ProofCode"}</strong><MessageContent content={message.content || (streamingMessage?.id === message.id && liveContent) || (message.status === "FAILED" ? "此回合失败，可在审查页查看详情。" : "正在等待模型响应…")}/><span className="message-meta">{new Date(message.createdAt).toLocaleString()}</span>{selected && message.role !== "user" && selected.executionMode !== "CHAT" && <button className="text-button message-run-link" onClick={() => onNavigate("审查", selected.id)}><ShieldCheck size={12}/>查看修改</button>}</div></div>; })}
      {!renderedMessages.length && task && <div className="message-row user"><div className="message-bubble"><strong>你</strong><p>{task.prompt}</p><span className="message-meta">{task.model}</span><button className="text-button message-run-link" onClick={() => onNavigate("审查", task.id)}><ShieldCheck size={12}/>查看修改</button></div></div>}
      {!task && !renderedMessages.length && <div className="conversation-empty"><Sparkles size={28}/><strong>直接开始聊天或写代码</strong><span>不需要先导入文件夹。首次发送会创建独立工程和可恢复对话。</span></div>}
      {task && liveContent && !streamingMessage && activeTask?.id === task.id && <div className="message-row assistant" key={`stream:${task.id}:${liveStep}`}><div className="message-avatar agent-avatar"><Bot size={15}/></div><div className="message-bubble"><strong>ProofCode</strong><MessageContent content={liveContent}/></div></div>}
      {task && <div className="chat-run-status"><StatusBadge status={task.status}/>{approvals.some((value) => value.status === "PENDING") && <button className="text-button" onClick={() => onNavigate("审查", task.id)}><ShieldCheck size={12}/>需要审批 · 打开审查</button>}{["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(task.status) && <button className="text-button" onClick={() => onCancel(task.id)}> <Square size={12}/>停止</button>}{["FAILED", "CANCELLED"].includes(task.status) && <button className="text-button" onClick={onRetry}><RefreshCw size={12}/>重试</button>}</div>}
      {activeTask && activeTask.id !== task?.id && <div className="chat-run-status"><StatusBadge status={activeTask.status}/><button className="text-button" onClick={() => onNavigate("审查", activeTask.id)}>查看运行中的回合</button><button className="text-button" onClick={() => onCancel(activeTask.id)}>停止</button></div>}
      <div ref={streamEnd}/></div><form className="agent-composer" onSubmit={(event) => void submit(event)}><textarea id="task-composer" value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder={mode === "CHAT" ? "直接提问、讨论设计或解释代码…" : "描述要完成的代码修改、排查或验证目标…"} rows={3} onKeyDown={event => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }}/><div className="agent-composer-footer"><div className="composer-mode"><button type="button" className={mode === "CHAT" ? "active" : ""} onClick={() => { setMode("CHAT"); localStorage.setItem("proofcode.execution-mode", "CHAT"); }}><MessageSquare size={13}/>普通聊天</button><button type="button" className={mode === "CODE" ? "active" : ""} onClick={() => { setMode("CODE"); localStorage.setItem("proofcode.execution-mode", "CODE"); }}><Code2 size={13}/>代码任务</button></div><div className="agent-composer-run"><input className="model-input" aria-label="模型 ID" list="proofcode-models" required maxLength={200} value={model} onChange={(event) => { setModel(event.target.value); localStorage.setItem("proofcode.model", event.target.value); }} placeholder="模型 ID"/><datalist id="proofcode-models"><option value="gpt-4.1-mini"/><option value="gpt-4o"/><option value="deepseek-chat"/></datalist><button className="run-button" type="submit" disabled={submitting || running || !prompt.trim() || !model.trim()}>{submitting ? <Loader2 size={14}/> : <Play size={14} fill="currentColor"/>}{submitting ? "创建中" : running ? "当前回合运行中" : mode === "CHAT" ? "发送" : "运行任务"}</button></div></div><p className="composer-scope-help">{mode === "CHAT" ? "普通聊天保留本对话历史，使用模型回答。" : "代码任务从当前源码快照运行，变更可审查后应用到工程。"}</p></form></section>
  </div>;
}

export function EventContent({ item }: { item: TaskEvent }) {
  const payload = item.payload || {};
  const message = item.type === "agent.message.completed" ? payload.content : item.type === "verification.completed" ? payload.report : item.type === "task.failed" ? payload.error : item.type.startsWith("tool.") ? payload.result : item.type === "task.completed" ? payload.result : item.type === "decision.tool_routed" || item.type === "decision.tool_route_fallback" ? JSON.stringify(payload, null, 2) : undefined;
  const tool = typeof payload.tool === "string" ? payload.tool : "";
  return <div className="event-entry-content">{tool && <span className="event-tool">{tool} · {String(payload.callId || "")}</span>}{item.type.startsWith("decision.") && <span className="event-tool">决策层 · {String(payload.choice || payload.reason || "回退")}</span>}{message != null && <pre>{typeof message === "string" ? message : JSON.stringify(message, null, 2)}</pre>}{(item.type === "checkpoint.created" || item.type === "recovery.created") && <span>{item.type === "recovery.created" ? "恢复补丁" : "Checkpoint"} · {String(payload.commit || "").slice(0, 12)} · {String(payload.branch || "")}</span>}</div>;
}

export function ToolchainPage({ task, events }: { task: Task | null; events: TaskEvent[] }) {
  const [selectedSequence, setSelectedSequence] = useState<number | null>(null);
  const selected = events.find((item) => item.sequence === selectedSequence) || events[events.length - 1];
  return <div className="toolchain-page"><div className="toolchain-heading"><div><span className="section-kicker">EXECUTION LOG</span><h1>执行链</h1></div>{task && <StatusBadge status={task.status}/>}</div><div className="chain-summary"><div><span>当前任务</span><strong>{task?.id || "等待新任务"}</strong></div><div><span>持久化事件</span><strong>{events.length}</strong></div><div><span>工具调用</span><strong>{events.filter((item) => item.type === "tool.requested").length}</strong></div><div><span>执行状态</span><strong>{task?.status || "未开始"}</strong></div></div><div className="toolchain-layout"><section className="chain-visual"><div className="chain-visual-head"><div><strong>事件时间线</strong><span>按服务端序号排序</span></div></div><div className="chain-event-list">{events.map((item) => <button key={item.sequence} className={`chain-event ${selected?.sequence === item.sequence ? "selected" : ""}`} onClick={() => setSelectedSequence(item.sequence)}><span className="chain-event-sequence">#{item.sequence}</span><span className="chain-event-type">{item.type}</span><span className="chain-event-tool">{typeof item.payload?.tool === "string" ? item.payload.tool : ""}</span><time>{new Date(item.timestamp).toLocaleTimeString()}</time></button>)}{events.length === 0 && <div className="event-empty">暂无执行事件</div>}</div></section><aside className="node-inspector"><div className="inspector-head"><div><span className="section-kicker">EVENT DETAIL</span><h2>{selected?.type || "选择事件"}</h2></div></div>{selected && <><div className="chain-event-info"><span>序号 #{selected.sequence}</span><time>{new Date(selected.timestamp).toLocaleString()}</time></div><pre className="chain-event-payload">{JSON.stringify(selected.payload, null, 2)}</pre></>}</aside></div></div>;
}

export function ToolUsagePage({ task, events }: { task: Task | null; events: TaskEvent[] }) {
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
export function TaskCapsule({ project, task, events, artifacts, approvals, onApprove, onCancel, onRetry, onNotice }: { project?: Project; task: Task | null; events: TaskEvent[]; artifacts: TaskArtifact[]; approvals: TaskApproval[]; onApprove: (approvalId: string, approved: boolean) => void; onCancel: () => void; onRetry: () => void; onNotice?: (notice: string) => void }) {
  const [detail, setDetail] = useState<TaskArtifactDetail | null>(null);
  const [patchLoading, setPatchLoading] = useState(false);
  const [patchError, setPatchError] = useState("");
  const [selectedArtifact, setSelectedArtifact] = useState<TaskArtifact | null>(null);
  const [applying, setApplying] = useState(false);
  const [applied, setApplied] = useState(false);
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
  const applyPatch = async (artifact: TaskArtifact) => {
    if (!task?.sourceSnapshotId || !project?.localHandle || !desktopApp()?.ApplyProjectPatch || applying) return;
    setApplying(true); setPatchError("");
    try {
      const value = await loadPatch(artifact); if (!value) return;
      const source = await api<{ manifestHash: string }>(`/api/projects/${project.id}/sources/${task.sourceSnapshotId}`);
      const result = await desktopApp()!.ApplyProjectPatch!(project.localHandle, source.manifestHash, value.patch);
      if (!result.applied) throw new Error("工程文件未应用，请检查冲突状态");
      setApplied(true); onNotice?.(`已应用到 ${result.path}（${result.fileCount} 个文件）`);
    } catch (cause) { setPatchError(`无法应用变更：${String(cause)}`); }
    finally { setApplying(false); }
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
    {selectedArtifact && <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) setSelectedArtifact(null); }}><section className="diff-dialog" role="dialog" aria-modal="true" aria-label="变更审查"><div className="diff-dialog-head"><div><span className="section-kicker">{selectedArtifact.kind.toUpperCase()}</span><h2>变更审查</h2><small>{selectedArtifact.commitHash} · {selectedArtifact.branch}</small></div><button className="icon-button" title="关闭" onClick={() => setSelectedArtifact(null)}><X size={18}/></button></div>{patchLoading && <div className="diff-loading">正在读取变更...</div>}{patchError && <div className="capsule-error">{patchError}</div>}{detail?.id === selectedArtifact.id && <pre className="diff-content">{detail.patch || "无文件变更"}</pre>}<div className="diff-dialog-actions">{project?.localHandle && task?.sourceSnapshotId && desktopApp()?.ApplyProjectPatch && <button className="primary-button" onClick={() => void applyPatch(selectedArtifact)} disabled={detail?.id !== selectedArtifact.id || patchLoading || applying || applied}>{applying ? "应用中" : applied ? "已应用到工程" : "批准应用到工程"}</button>}<button className="secondary-button" onClick={() => void downloadPatch(selectedArtifact)} disabled={detail?.id !== selectedArtifact.id || patchLoading}>下载 patch</button><button className="primary-button" onClick={() => setSelectedArtifact(null)}>关闭</button></div></section></div>}
  </section>;
}
function ActivityRow({ icon, title, detail, time, tone }: { icon: React.ReactNode; title: string; detail: string; time: string; tone: string }) { return <div className="activity-row"><span className={`activity-icon ${tone}`}>{icon}</span><div><strong>{title}</strong><small>{detail}</small></div><time>{time}</time></div>; }
function StatusIcon({ status }: { status: string }) { return status === "SUCCEEDED" ? <CheckCircle2 size={16}/> : status === "FAILED" || status === "CANCELLED" ? <XCircle size={16}/> : <CircleDot size={16}/>; }
export function StatusBadge({ status }: { status: string }) { const labels: Record<string, string> = { SUCCEEDED: "已完成", VERIFYING: "验证中", RUNNING: "运行中", QUEUED: "排队中", WAITING_APPROVAL: "待审批", FAILED: "失败", CANCELLED: "已取消" }; return <span className={`status-badge ${status.toLowerCase()}`}>{labels[status] || status}</span>; }
export function ActionPanel({ kind, tasks, events, artifacts, onClose }: { kind: PanelKind; tasks: Task[]; events: TaskEvent[]; artifacts: TaskArtifact[]; onClose: () => void }) {
  const [modelConfig, setModelConfig] = useState<ModelConfig | null>(null);
  const [loadingConfig, setLoadingConfig] = useState(false);
  const [tokenValue, setTokenValue] = useState(() => localStorage.getItem("proofcode.token") || "");
  const [tokenSaved, setTokenSaved] = useState(false);
  const refreshModelConfig = async () => {
    setLoadingConfig(true);
    try {
      const value = await (desktopApp()?.GetModelConfig || (window as unknown as { GetModelConfig?: () => Promise<ModelConfig> }).GetModelConfig)?.();
      if (value) setModelConfig(value); else throw new Error("桌面绑定不可用，请从桌面端打开");
    } catch (error) { setModelConfig({ configured: false, provider: "", model: "", baseUrl: "", source: "Codex config.toml", hasKey: false, keyLast4: "", maxContextTokens: 183616, maxOutputTokens: 16384, maxTotalTokens: 200000, error: String(error) }); }
    finally { setLoadingConfig(false); }
  };
  useEffect(() => { if (kind === "Agent 设置") void refreshModelConfig(); }, [kind]);
  useEffect(() => { if (kind === "账户") { setTokenValue(localStorage.getItem("proofcode.token") || ""); setTokenSaved(false); } }, [kind]);
  const saveToken = () => { const value = tokenValue.trim(); if (value) localStorage.setItem("proofcode.token", value); else localStorage.removeItem("proofcode.token"); setTokenValue(value); setTokenSaved(true); };
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
  return <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}><section className="action-panel"><div className="panel-head"><div><span className="section-kicker">{current.eyebrow}</span><h2>{current.title}</h2></div><button className="icon-button" title="关闭" onClick={onClose}><X size={17}/></button></div><p className="panel-description">{current.description}</p>{kind === "账户" && <div className="account-settings"><label>Control Plane Token<input type="password" autoComplete="off" value={tokenValue} onChange={(event) => { setTokenValue(event.target.value); setTokenSaved(false); }} placeholder="输入当前环境的访问 Token"/></label><p className="form-help">Token 仅保存在当前桌面 WebView 或浏览器的本地存储中，发送请求时作为 Bearer 凭据。</p>{tokenSaved && <div className="capsule-success"><CheckCircle2 size={15}/>Token 已保存，刷新或重新发送请求即可生效。</div>}<div className="panel-actions"><button className="primary-button" onClick={saveToken}><Check size={14}/>保存 Token</button><button className="secondary-button" onClick={() => { setTokenValue(""); localStorage.removeItem("proofcode.token"); setTokenSaved(true); }}>清除</button></div></div>}{kind === "运行统计" && <div className="panel-stats"><div><span>任务</span><strong>{tasks.length}</strong></div><div><span>已结束</span><strong>{finished.length}</strong></div><div><span>成功率</span><strong>{finished.length ? `${Math.round(succeeded / finished.length * 100)}%` : "--"}</strong></div><div><span>事件</span><strong>{events.length}</strong></div><div><span>Artifacts</span><strong>{artifacts.length}</strong></div></div>}{kind === "Agent 设置" && <div className="model-config"><div className="panel-stats"><div><span>配置来源</span><strong>{modelConfig?.source || "读取中"}</strong></div><div><span>Provider</span><strong>{modelConfig?.provider || "未读取"}</strong></div><div><span>模型</span><strong>{modelConfig?.model || "未配置"}</strong></div><div><span>API Key</span><strong>{modelConfig?.hasKey ? `已配置（${modelConfig.keyLast4}）` : "未配置"}</strong></div><div><span>输入预算</span><strong>{(modelConfig?.maxContextTokens || 0).toLocaleString()} token</strong></div><div><span>输出预算</span><strong>{(modelConfig?.maxOutputTokens || 0).toLocaleString()} token</strong></div></div>{modelConfig?.error && <div className="capsule-error"><XCircle size={15}/>{modelConfig.error}</div>}<button className="secondary-button" onClick={() => void refreshModelConfig()} disabled={loadingConfig}><RefreshCw size={14}/>{loadingConfig ? "读取中" : "刷新 Codex 配置"}</button></div>}{kind === "代码证据" && <div className="panel-stats"><div><span>工具调用</span><strong>{events.filter((item) => item.type.startsWith("tool.")).length}</strong></div><div><span>验证事件</span><strong>{events.filter((item) => item.type === "verification.completed").length}</strong></div><div><span>变更 artifact</span><strong>{artifacts.length}</strong></div></div>}{kind === "验证中心" && <div className="panel-stats"><div><span>验证事件</span><strong>{events.filter((item) => item.type === "verification.completed").length}</strong></div><div><span>失败事件</span><strong>{events.filter((item) => item.type.endsWith("failed")).length}</strong></div><div><span>当前任务</span><strong>{tasks.find((task) => task.status === "VERIFYING")?.status || "无"}</strong></div></div>}<div className="panel-actions"><button className="secondary-button" onClick={onClose}>关闭</button></div></section></div>;
}
