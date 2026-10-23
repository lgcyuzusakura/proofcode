import { FormEvent, Fragment, useEffect, useMemo, useState } from "react";
import { Activity, ArrowUpRight, Bot, Check, CheckCircle2, CircleDot, Code2, Database, FileCode2, GitBranch, Loader2, MessageSquare, MoreHorizontal, Play, Plus, RefreshCw, ShieldCheck, Sparkles, Square, TerminalSquare, Workflow, Wrench, X, XCircle } from "lucide-react";
import { api, desktopApp, logout, type AccountUser } from "./api";
import type { Conversation, ConversationMessage, ExecutionMode, Project, Task, TaskApproval, TaskArtifact, TaskArtifactDetail, TaskEvent, ModelProvider, SourceAttachment } from "./types";
import { MessageContent } from "./MessageContent";
import { CurrentActivity, TaskActivity } from "./ExecutionActivity";
import { Transcript, type MessageLocation } from "./Transcript";
import { MessageActions } from "./MessageActions";
import { taskLabel, toolLabel } from "./presentation";
import { ContextUsage, RunUsageSummary, AgentTeam } from "./RunResources";
import { ProviderSettings } from "./ProviderSettings";
import { TaskProgress } from "./TaskProgress";
import { AgentPlatform, AgentConfiguration, type AgentModelChoice } from "./AgentPlatform";
import { ConversationContext, useConversationContext } from "./ConversationContext";
import { SourceAttachments, readSourceAttachments, saveSourceAttachments } from "./SourceAttachments";
import { ToolCatalog } from "./ToolCatalog";
import { latestStreamingReview } from "./streaming-review";
export type PanelKind = "运行统计" | "账户" | "代码证据" | "验证中心" | "最近活动" | "Agent 设置";

export function AgentWorkspace({ project, conversation, messages, task, tasks, events, artifacts, approvals, onCreated, onSelectTask, onApprove, onCancel, onRetry, onNavigate, onNotice, onContextFork, historyLocation }: { project: Project; conversation: Conversation | null; messages: ConversationMessage[]; task: Task | null; tasks: Task[]; events: TaskEvent[]; artifacts: TaskArtifact[]; approvals: TaskApproval[]; onCreated: (prompt: string, model: string, mode: ExecutionMode, providerId: string, attachments: SourceAttachment[]) => Promise<boolean>; onSelectTask: (task: Task) => void; onApprove: (approvalId: string, approved: boolean) => void; onCancel: (taskId?: string) => void; onRetry: () => void; onNavigate: (label: string, taskId?: string) => void; onNotice: (notice: string) => void; onContextFork: (conversation: Conversation) => void; historyLocation?: MessageLocation }) {
  const [showContext, setShowContext] = useState(false);
  const [agentConfigBusy, setAgentConfigBusy] = useState(false);
  const contextPath = conversation ? `/api/projects/${project.id}/workspaces/${conversation.workspaceId}/conversations/${conversation.id}/context` : "";
  const context = useConversationContext(contextPath, messages.map(item => `${item.id}:${item.status}:${item.content.length}`).join("|"));
  const closeContext = () => { setShowContext(false); document.getElementById("open-conversation-context")?.focus(); };
  useEffect(() => { if (conversation?.archivedAt) setShowContext(false); }, [conversation?.id, conversation?.archivedAt]);
  const draftKey = `proofcode.chat-draft.${project.id || "scratch"}.${conversation?.id || "new"}`;
  const [prompt, setPrompt] = useState(() => { try { return localStorage.getItem(draftKey) || ""; } catch { return ""; } });
  const updatePrompt = (value: string) => { setPrompt(value); try { if (value) localStorage.setItem(draftKey, value); else localStorage.removeItem(draftKey); } catch { /* The editor works even when storage is unavailable. */ } };
  const [attachmentDraft] = useState(() => readSourceAttachments(project.id, conversation?.id));
  const [sourceAttachments, setSourceAttachments] = useState<SourceAttachment[]>(attachmentDraft.items);
  useEffect(() => { if (attachmentDraft.error) onNotice(attachmentDraft.error); }, []);
  const removeAttachment = (index: number) => {
    const next = sourceAttachments.filter((_, i) => i !== index);
    try { saveSourceAttachments(project.id, conversation?.id, next); setSourceAttachments(next); }
    catch (cause) { onNotice(String(cause)); }
  };
  const [showDetails, setShowDetails] = useState(false);
  useEffect(() => { const toggle = () => { setShowDetails(value => !value); setShowContext(false); }; window.addEventListener("proofcode:toggle-run-details", toggle); return () => window.removeEventListener("proofcode:toggle-run-details", toggle); }, []);
  const [model, setModel] = useState(localStorage.getItem("proofcode.model") || "");
  const [providers, setProviders] = useState<ModelProvider[]>([]);
  const [providerId, setProviderId] = useState(localStorage.getItem("proofcode.provider") || "");
  const [agentModel, setAgentModel] = useState<AgentModelChoice | null>(null);
  const effectiveProviderId = agentModel?.providerId || providerId;
  const effectiveModel = agentModel?.model || model;
  const models = providers.find(value => value.id === effectiveProviderId)?.models || [];
  const modelAvailable = !!effectiveProviderId && models.some(value => value.id === effectiveModel);
  const [mode, setMode] = useState<ExecutionMode>(["CHAT", "PLAN"].includes(localStorage.getItem("proofcode.execution-mode") || "") ? localStorage.getItem("proofcode.execution-mode") as ExecutionMode : "CODE");
  const [submitting, setSubmitting] = useState(false);
  useEffect(() => {
    let live = true;
    const load = () => { void api<ModelProvider[]>("/api/model-providers").then(values => { if (!live) return; setProviders(values); setProviderId(old => values.some(value => value.id === old) ? old : values[0]?.id || ""); }).catch(error => { if (live) onNotice(String(error)); }); };
    load(); window.addEventListener("proofcode:providers-changed", load);
    return () => { live = false; window.removeEventListener("proofcode:providers-changed", load); };
  }, []);
  useEffect(() => { localStorage.setItem("proofcode.provider", providerId); }, [providerId]);
  useEffect(() => { if (!agentModel && !models.some(value => value.id === model)) setModel(models[0]?.id || ""); }, [providerId, providers, agentModel]);
  const activeTask = tasks.find(item => ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(item.status));
  const liveResponse = useMemo(() => latestStreamingReview(events, task?.attempt), [events, task?.attempt]);
  const liveStep = liveResponse?.step ?? 0, liveContent = liveResponse?.text ?? "";
  const running = tasks.some((item) => ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(item.status));
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (!prompt.trim() || !modelAvailable || submitting || running || context.busy || agentConfigBusy || conversation?.archivedAt) return;
    setSubmitting(true);
    try { if (await onCreated(prompt.trim(), effectiveModel, mode, effectiveProviderId, sourceAttachments)) { updatePrompt(""); setSourceAttachments([]); try { saveSourceAttachments(project.id, conversation?.id, []); } catch (cause) { onNotice(String(cause)); } } } finally { setSubmitting(false); }
  };
  const renderedMessages = messages.filter((item) => item.content || item.status !== "COMPLETED");
  const lastAssistant = [...renderedMessages].reverse().find(value => value.role === "assistant" && value.taskId === task?.id)?.id;
  const streamingMessage = renderedMessages.find(item => item.role === "assistant" && item.taskId === task?.id && !item.content);
  return <div className={`agent-page agent-chat-page ${!task && !renderedMessages.length ? "is-empty" : ""}`}>
    <div className="agent-layout"><section className="conversation-panel"><div className="context-chat-toolbar"><span>{conversation?.title || "新对话"}</span><AgentPlatform projectId={project.id} conversationId={conversation?.id} query={prompt} disabled={running || submitting || !!conversation?.archivedAt} onBusy={setAgentConfigBusy} providers={providers} onModelChoice={setAgentModel}/><button id="open-conversation-context" type="button" disabled={!conversation || !!conversation.archivedAt} aria-expanded={showContext} onClick={() => { setShowContext(value => !value); setShowDetails(false); }}><Workflow size={14}/>上下文{context.preview && context.preview.excludedTurns > 0 && <small>{context.preview.excludedTurns} 段已排除</small>}</button></div>{conversation?.archivedAt && <div className="archived-conversation-banner" role="status">此对话已归档，只能查看历史。恢复后可以继续聊天和修改上下文。</div>}<Transcript location={historyLocation} sessionKey={`${project.id}:${conversation?.id || "new"}`}>
      {renderedMessages.map((message) => {
        const selected = message.taskId ? tasks.find(item => item.id === message.taskId) : undefined;
        const turn = messages.find(item => item.role === "user" && item.taskId === message.taskId && item.attempt === message.attempt);
        const contextState = turn ? context.state(turn.id) : "INCLUDE";
        const finalAssistant = message.role === "assistant" && !renderedMessages.some(item => item.role === "assistant" && item.taskId === message.taskId && item.attempt === message.attempt && item.sequence > message.sequence);
        const content = message.content || (streamingMessage?.id === message.id && liveContent);
        return <Fragment key={message.id}>
          {finalAssistant && selected && <TaskActivity key={`${selected.id}:${message.attempt || 1}`} task={{ ...selected, attempt: message.attempt ?? selected.attempt, status: message.attempt != null && selected.attempt != null && message.attempt !== selected.attempt ? message.status : selected.status }} selected={selected.id === task?.id} events={events}/>}
          <div className={`message-row ${message.role}`} data-message-id={message.id}><div className={`message-avatar ${message.role === "user" ? "user-avatar" : "agent-avatar"}`}>{message.role === "user" ? "你" : <Bot size={15}/>}</div><div className="message-bubble"><strong>{message.role === "user" ? "你" : "ProofCode"}</strong><MessageContent content={content || (message.status === "FAILED" ? "本次任务未完成，可展开执行记录查看详情。" : message.status === "CANCELLED" ? "本次任务已停止。" : "正在等待模型响应…")}/><span className="message-meta">{new Date(message.createdAt).toLocaleString()}</span>{message.content && <MessageActions content={message.content} reuse={message.role === "user" ? () => { updatePrompt(message.content); document.getElementById("task-composer")?.focus(); } : undefined}/>} {turn && message.role === "assistant" && <button className="text-button message-context-action" disabled={!!conversation?.archivedAt || context.busy || !context.preview} onClick={() => context.edit([turn.id], contextState === "EXCLUDE" ? "INCLUDE" : "EXCLUDE", undefined, messages.filter(item => item.role === "user").map(item => item.id))}>{contextState === "EXCLUDE" ? "已从上下文排除 · 恢复问答" : contextState === "REPLACE" ? "已用摘要替换 · 排除问答" : "从上下文排除问答"}</button>} {selected && message.role !== "user" && selected.executionMode !== "CHAT" && <button className="text-button message-run-link" onClick={() => onNavigate("审查", selected.id)}><ShieldCheck size={12}/>查看修改</button>}</div></div>
        </Fragment>;
      })}
      {!renderedMessages.length && task && <div className="message-row user"><div className="message-bubble"><strong>你</strong><p>{task.prompt}</p><span className="message-meta">{task.model}</span><button className="text-button message-run-link" onClick={() => onNavigate("审查", task.id)}><ShieldCheck size={12}/>查看修改</button></div></div>}
      {!task && !renderedMessages.length && <div className="conversation-empty"><div className="empty-brand"><Code2 size={30}/></div><strong>从一个想法，开始构建。</strong><span>{project.id ? `与 ProofCode 一起探索 ${project.name}` : "打开项目，或直接描述你想完成的工作。"}</span><div className="starter-prompts">{["梳理项目结构，说明入口和核心模块", "检查当前改动，找出潜在问题", "定位失败的测试并提出修复方案"].map(value => <button key={value} type="button" onClick={() => { updatePrompt(value); document.getElementById("task-composer")?.focus(); }}><span>{value}</span><ArrowUpRight size={15}/></button>)}</div></div>}
      {task && liveContent && !streamingMessage && activeTask?.id === task.id && <div className="message-row assistant" key={`stream:${task.id}:${liveStep}`}><div className="message-avatar agent-avatar"><Bot size={15}/></div><div className="message-bubble"><strong>ProofCode</strong><MessageContent content={liveContent}/></div></div>}
      {!lastAssistant && task && <TaskActivity task={task} selected events={events}/>}
      {task?.status === "FAILED" && task.error && <div className="chat-error-banner" role="alert"><strong>{/403|Forbidden|内容安全|拦截/i.test(task.error) ? "模型服务拒绝了本次请求" : "本次任务未完成"}</strong><p>{/403|Forbidden|内容安全|拦截/i.test(task.error) ? "服务返回了拒绝响应。可以修改问题或检查模型服务设置，再手动发送。" : "查看错误详情，修改问题后可以重新发送。"}</p><details><summary>错误详情</summary><pre>{task.error}</pre></details><div className="chat-error-actions"><button type="button" className="secondary-button" onClick={() => { updatePrompt(task.prompt); document.getElementById("task-composer")?.focus(); }}>修改问题</button><button type="button" className="secondary-button" onClick={() => window.dispatchEvent(new Event("proofcode:open-provider-settings"))}>模型设置</button></div></div>}
      {task && <div className="chat-run-status"><StatusBadge status={task.status}/>{approvals.some((value) => value.status === "PENDING") && <button className="text-button" onClick={() => onNavigate("审查", task.id)}><ShieldCheck size={12}/>需要审批 · 打开审查</button>}{["FAILED", "CANCELLED"].includes(task.status) && !conversation?.archivedAt && <button className="text-button" onClick={onRetry}><RefreshCw size={12}/>重试</button>}</div>}
      {task && <TaskProgress key={`${task.id}:${task.attempt}`} task={task} events={events} onNotice={onNotice} onUsePlan={plan => { setMode("CODE"); localStorage.setItem("proofcode.execution-mode", "CODE"); updatePrompt(prompt.trim() ? `${prompt.trim()}\n\n${plan}` : plan); document.getElementById("task-composer")?.focus(); }}/>}
      {task && <RunUsageSummary key={task.id} task={task} events={events}/> }
      {activeTask && activeTask.id !== task?.id && <div className="chat-run-status"><StatusBadge status={activeTask.status}/><button className="text-button" onClick={() => onNavigate("审查", activeTask.id)}>查看运行中的回合</button><button className="text-button" onClick={() => onCancel(activeTask.id)}>停止</button></div>}
      </Transcript><div className="composer-dock">{(activeTask || task) && <CurrentActivity task={(activeTask || task)!} events={activeTask && activeTask.id !== task?.id ? [] : events} onShow={() => { if (activeTask && activeTask.id !== task?.id) onSelectTask(activeTask); setShowDetails(true); setShowContext(false); }} onStop={() => onCancel((activeTask || task)!.id)}/>}<form className="agent-composer" onSubmit={(event) => void submit(event)}><SourceAttachments conversationId={conversation?.id} projectId={project.id} workspaceId={conversation?.workspaceId} items={sourceAttachments} onRemove={removeAttachment}/><textarea id="task-composer" aria-label="消息内容" disabled={!!conversation?.archivedAt} value={prompt} onChange={(event) => updatePrompt(event.target.value)} placeholder={conversation?.archivedAt ? "此对话已归档；恢复后可继续" : mode === "CHAT" ? "直接提问、讨论设计或解释代码…" : mode === "PLAN" ? "描述需要调查的问题，先制定方案…" : "描述要完成的代码修改、排查或验证目标…"} rows={2} onKeyDown={event => { if (event.key === "Enter" && (event.ctrlKey || event.metaKey) && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }}/><div className="agent-composer-footer"><div className="composer-mode"><button type="button" className={mode === "CHAT" ? "active" : ""} onClick={() => { setMode("CHAT"); localStorage.setItem("proofcode.execution-mode", "CHAT"); }}><MessageSquare size={13}/>普通聊天</button><button type="button" className={mode === "CODE" ? "active" : ""} onClick={() => { setMode("CODE"); localStorage.setItem("proofcode.execution-mode", "CODE"); }}><Code2 size={13}/>代码任务</button><button type="button" className={mode === "PLAN" ? "active" : ""} onClick={() => { setMode("PLAN"); localStorage.setItem("proofcode.execution-mode", "PLAN"); }}><Workflow size={13}/>只读规划</button></div><div className="agent-composer-run"><select aria-label="模型服务" disabled={!!agentModel} title={agentModel ? "由当前智能体指定" : undefined} value={effectiveProviderId} onChange={event => setProviderId(event.target.value)}><option value="">选择模型服务</option>{agentModel && !providers.some(p => p.id === effectiveProviderId) && <option value={effectiveProviderId}>智能体模型服务已不可用</option>}{providers.map(value => <option key={value.id} value={value.id}>{value.name}</option>)}</select><select aria-label="模型" disabled={!!agentModel} title={agentModel ? "由当前智能体指定" : undefined} required value={effectiveModel} onChange={event => { setModel(event.target.value); localStorage.setItem("proofcode.model", event.target.value); }}><option value="">选择可用模型</option>{agentModel && !modelAvailable && <option value={effectiveModel}>智能体模型已不可用</option>}{models.map(value => <option key={value.id} value={value.id}>{value.name}</option>)}</select><button className="run-button" type="submit" disabled={submitting || running || context.busy || agentConfigBusy || !!conversation?.archivedAt || !prompt.trim() || !modelAvailable}>{submitting ? <Loader2 size={14}/> : <ArrowUpRight size={18}/>}<span>{submitting ? "创建中" : running ? "当前回合运行中" : mode === "CHAT" ? "发送" : mode === "PLAN" ? "制定方案" : "运行任务"}</span></button></div></div></form><div className="composer-caption"><span>{agentModel ? modelAvailable ? "当前模型由智能体指定" : "智能体模型不可用，请在“智能体与记忆”重新配置" : "Ctrl + Enter 发送 · Enter 换行"}</span><span>{mode === "CHAT" ? "对话历史自动保存" : mode === "PLAN" ? "仅调查与规划，禁止修改及执行命令" : "修改可审查后应用"}</span></div></div></section>
      {showContext && conversation && <ConversationContext manager={context} path={contextPath} messages={messages} prompt={prompt} running={running} onClose={closeContext} onFork={onContextFork}/>}
      {showDetails && <aside className="agent-context"><div className="evidence-panel-head"><strong>运行详情</strong><button className="icon-button" title="关闭运行详情" onClick={() => setShowDetails(false)}><X size={16}/></button></div><div className="chat-task-list"><strong>当前对话的任务</strong>{tasks.map(value => <button key={value.id} className={task?.id === value.id ? "active" : ""} onClick={() => onSelectTask(value)}><span>{value.prompt}</span><StatusBadge status={value.status}/></button>)}{!tasks.length && <p>还没有运行任务</p>}</div><TaskCapsule project={project} task={task} events={events} artifacts={artifacts} approvals={approvals} onApprove={onApprove} onCancel={() => onCancel(task?.id)} onRetry={onRetry} onNotice={onNotice} retryDisabled={!!conversation?.archivedAt}/><AgentConfiguration events={events}/><ContextUsage events={events} attempt={task?.attempt}/><AgentTeam events={events} attempt={task?.attempt}/></aside>}
    </div>
  </div>;
}

const eventLabels: Record<string, string> = { "task.started": "任务开始", "agent.delegated": "子任务协作", "agent.message.completed": "Agent 回复完成", "verification.completed": "验证完成", "task.failed": "任务失败", "task.completed": "任务完成", "tool.requested": "请求工具", "tool.started": "工具开始执行", "tool.completed": "工具执行完成", "tool.failed": "工具执行失败", "checkpoint.created": "创建检查点", "recovery.created": "创建恢复补丁", "workspace.updated": "工作区变更预览", "decision.tool_routed": "决策层选择工具", "decision.tool_route_fallback": "决策层回退" };
function eventLabel(type: string) { return eventLabels[type] || type.split(".").join(" · "); }
function eventSummary(item: TaskEvent) {
  const payload = item.payload || {};
  if (item.type.startsWith("tool.")) return `${payload.tool ? toolLabel(String(payload.tool)) : "工具"}${payload.result ? " · 已返回结果" : ""}`;
  if (item.type === "verification.completed") return "测试与验证结果已写入当前回合";
  if (item.type === "task.completed") return "当前回合已完成，可以打开代码审查";
  if (item.type === "workspace.updated") return `工作区变更预览：${payload.state === "ready" ? "已记录累计修改" : payload.state === "oversized" ? "修改超过预览上限" : "预览暂不可用"}`;
  if (item.type.startsWith("decision.")) return String(payload.choice || payload.reason || "已完成工具路由");
  if (item.type === "task.failed") return String(payload.error || "执行过程中出现错误");
  return "已记录到执行证据";
}
export function EventContent({ item }: { item: TaskEvent }) {
  const payload = item.payload || {};
  if (item.type === "workspace.updated") return <div className="event-entry-content"><p className="event-readable-summary">工作区变更预览：{payload.state === "ready" ? "已记录累计修改" : payload.state === "oversized" ? "修改超过预览上限" : "预览暂不可用"}</p><small>请在“文件变更”页查看预览或最终审查产物。</small></div>;
  const message = item.type === "agent.message.completed" ? payload.content : item.type === "verification.completed" ? payload.report : item.type === "task.failed" ? payload.error : item.type.startsWith("tool.") ? payload.result : item.type === "task.completed" ? payload.result : undefined;
  const tool = typeof payload.tool === "string" ? payload.tool : "";
  return <div className="event-entry-content">{tool && <span className="event-tool">{toolLabel(tool)} · {String(payload.callId || "")}</span>}{item.type.startsWith("decision.") && <span className="event-tool">决策层 · {String(payload.choice || payload.reason || "回退")}</span>}{message == null && <p className="event-readable-summary">{eventSummary(item)}</p>}{message != null && <p className="event-readable-summary">{typeof message === "string" ? message : eventSummary(item)}</p>}{(item.type === "checkpoint.created" || item.type === "recovery.created") && <span>{item.type === "recovery.created" ? "恢复补丁" : "检查点"} · {String(payload.commit || "").slice(0, 12)} · {String(payload.branch || "")}</span>}{message != null && <details className="event-raw"><summary>查看原始结果</summary><pre>{typeof message === "string" ? message : JSON.stringify(message, null, 2)}</pre></details>}</div>;
}

export function ToolchainPage({ task, events }: { task: Task | null; events: TaskEvent[] }) {
  const [selectedSequence, setSelectedSequence] = useState<number | null>(null);
  const timeline = events.filter(item => !["agent.message.delta", "usage.updated"].includes(item.type));
  const selected = timeline.find((item) => item.sequence === selectedSequence) || timeline[timeline.length - 1];
  const inspectorPayload = selected?.type === "workspace.updated" ? Object.fromEntries(Object.entries(selected.payload).filter(([key]) => key !== "patch")) : selected?.payload;
  return <div className="toolchain-page"><div className="toolchain-heading"><div><span className="section-kicker">执行过程</span><h1>执行链</h1><p className="page-subtitle">按时间查看 Agent 如何选择工具、执行验证并交付结果。</p></div>{task && <StatusBadge status={task.status}/>}</div><div className="chain-summary"><div><span>当前任务</span><strong>{task?.id ? task.id.slice(0, 12) : "等待新任务"}</strong></div><div><span>持久化事件</span><strong>{events.length}</strong></div><div><span>工具调用</span><strong>{events.filter((item) => item.type === "tool.requested").length}</strong></div><div><span>执行状态</span><strong>{task?.status ? taskLabel(task.status) : "未开始"}</strong></div></div><ContextUsage events={events} attempt={task?.attempt}/><AgentTeam events={events} attempt={task?.attempt}/><div className="toolchain-layout"><section className="chain-visual"><div className="chain-visual-head"><div><strong>回合时间线</strong><span>点击任意节点查看证据</span></div></div><div className="chain-event-list">{timeline.map((item) => <button key={item.sequence} className={`chain-event ${selected?.sequence === item.sequence ? "selected" : ""}`} onClick={() => setSelectedSequence(item.sequence)}><span className="chain-event-sequence">#{item.sequence}</span><span className="chain-event-type">{eventLabel(item.type)}</span><span className="chain-event-tool">{typeof item.payload?.tool === "string" ? toolLabel(item.payload.tool) : eventSummary(item)}</span><time>{new Date(item.timestamp).toLocaleTimeString()}</time></button>)}{timeline.length === 0 && <div className="event-empty">暂无执行事件</div>}</div></section><aside className="node-inspector"><div className="inspector-head"><div><span className="section-kicker">节点详情</span><h2>{selected ? eventLabel(selected.type) : "选择时间线节点"}</h2></div></div>{selected && <><div className="chain-event-info"><span>第 {selected.sequence} 个节点</span><time>{new Date(selected.timestamp).toLocaleString()}</time></div><p className="selected-event-summary">{eventSummary(selected)}</p><details className="chain-raw-details"><summary>查看原始事件数据</summary><pre className="chain-event-payload">{JSON.stringify(inspectorPayload, null, 2)}</pre></details></>}</aside></div></div>;
}

export function ToolUsagePage({ task, events }: { task: Task | null; events: TaskEvent[] }) {
  return <div className="plugin-page"><div className="plugin-heading"><div><h1>可用工具</h1></div>{task && <StatusBadge status={task.status}/>}</div><ToolCatalog events={events} attempt={task?.attempt}/><AgentTeam events={events} attempt={task?.attempt}/></div>;
}

function NavItem({ icon, label, active, count, onClick }: { icon: React.ReactNode; label: string; active?: boolean; count?: number; onClick?: () => void }) { return <button className={`nav-item ${active ? "active" : ""}`} onClick={onClick}>{icon}<span>{label}</span>{count !== undefined && <small>{count}</small>}</button>; }
function Metric({ icon, label, value, detail, tone }: { icon: React.ReactNode; label: string; value: string; detail: string; tone: string }) { return <div className="metric-card"><div className={`metric-icon ${tone}`}>{icon}</div><div><span>{label}</span><strong>{value}</strong><small>{detail}</small></div></div>; }
function TaskComposer({ onCreated }: { onCreated: (prompt: string, model: string) => Promise<boolean> }) { const [prompt, setPrompt] = useState(""); const [model, setModel] = useState("gpt-4.1-mini"); const [submitting, setSubmitting] = useState(false); const submit = async (event: FormEvent) => { event.preventDefault(); if (!prompt.trim() || submitting) return; setSubmitting(true); try { if (await onCreated(prompt.trim(), model)) setPrompt(""); } finally { setSubmitting(false); } }; return <form className="task-composer" onSubmit={(event) => void submit(event)}><div className="composer-top"><span className="composer-badge"><Sparkles size={14}/>Agent 任务</span></div><textarea id="task-composer" value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder="描述需要修改或排查的代码问题..." rows={3}/><div className="composer-footer"><div className="composer-run"><select value={model} onChange={(e) => setModel(e.target.value)}><option>gpt-4.1-mini</option><option>gpt-4o</option><option>deepseek-chat</option></select><button className="run-button" type="submit" disabled={submitting || !prompt.trim()}>{submitting ? <Loader2 size={14}/> : <Play size={14} fill="currentColor"/>}{submitting ? "创建中" : "运行任务"}</button></div></div></form>; }
function SessionRow({ task, selected, onClick }: { task: Task; selected: boolean; onClick: () => void }) { return <button className={`session-row ${selected ? "selected" : ""}`} onClick={onClick}><div className={`session-status ${task.status.toLowerCase()}`}><StatusIcon status={task.status}/></div><div className="session-main"><strong>{task.prompt}</strong><div><span>{task.model}</span><span className="session-separator">·</span><span>{new Date(task.createdAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span></div></div><StatusBadge status={task.status}/><MoreHorizontal className="row-more" size={16}/></button>; }
export function TaskCapsule({ project, task, events, artifacts, approvals, onApprove, onCancel, onRetry, onNotice, retryDisabled = false }: { project?: Project; task: Task | null; events: TaskEvent[]; artifacts: TaskArtifact[]; approvals: TaskApproval[]; onApprove: (approvalId: string, approved: boolean) => void; onCancel: () => void; onRetry: () => void; onNotice?: (notice: string) => void; retryDisabled?: boolean }) {
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
    <div className="capsule-head"><div><span className="section-kicker">TASK CAPSULE</span><h2>{task?.prompt || "选择一个会话"}</h2></div><div className="capsule-actions">{task && <StatusBadge status={task.status} />}{task && ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(task.status) && <button className="cancel-button" onClick={onCancel}><Square size={13}/>停止</button>}{task && ["FAILED", "CANCELLED"].includes(task.status) && !retryDisabled && <button className="retry-button" onClick={onRetry}><RefreshCw size={13}/>重试</button>}</div></div>
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
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [onClose]);
  const finished = tasks.filter((task) => ["SUCCEEDED", "FAILED", "CANCELLED"].includes(task.status));
  const succeeded = tasks.filter((task) => task.status === "SUCCEEDED").length;
  const content: Record<PanelKind, { eyebrow: string; title: string; description: string }> = {
    "运行统计": { eyebrow: "RUN ANALYTICS", title: "运行统计", description: "统计来自当前项目任务和已持久化事件。" },
    "账户": { eyebrow: "ACCOUNT", title: "本地账户", description: "账户、工程和模型服务配置按登录账户隔离。" },
    "代码证据": { eyebrow: "CODE EVIDENCE", title: "代码证据", description: "当前任务的工具调用、验证报告与变更 artifact。" },
    "验证中心": { eyebrow: "VERIFICATION", title: "验证中心", description: "当前任务通过事件记录的验证结果。" },
    "最近活动": { eyebrow: "ACTIVITY LOG", title: "最近活动", description: "当前任务最近的持久化事件。" },
    "Agent 设置": { eyebrow: "AGENT CONFIG", title: "Agent 设置", description: "连接模型服务，刷新可用模型，选择协议和显示名称。" }
  };
  const current = content[kind];
  return <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}><section className={`action-panel ${kind === "Agent 设置" ? "provider-panel" : ""}`} role="dialog" aria-modal="true" aria-label={current.title}><div className="panel-head"><div><span className="section-kicker">{current.eyebrow}</span><h2>{current.title}</h2></div><button className="icon-button" title="关闭" onClick={onClose}><X size={17}/></button></div><p className="panel-description">{current.description}</p>{kind === "账户" && <AccountSettings onClose={onClose}/>}{kind === "运行统计" && <div className="panel-stats"><div><span>任务</span><strong>{tasks.length}</strong></div><div><span>已结束</span><strong>{finished.length}</strong></div><div><span>成功率</span><strong>{finished.length ? `${Math.round(succeeded / finished.length * 100)}%` : "--"}</strong></div><div><span>事件</span><strong>{events.length}</strong></div><div><span>Artifacts</span><strong>{artifacts.length}</strong></div></div>}{kind === "Agent 设置" && <ProviderSettings />}{kind === "代码证据" && <div className="panel-stats"><div><span>工具调用</span><strong>{events.filter((item) => item.type.startsWith("tool.")).length}</strong></div><div><span>验证事件</span><strong>{events.filter((item) => item.type === "verification.completed").length}</strong></div><div><span>变更 artifact</span><strong>{artifacts.length}</strong></div></div>}{kind === "验证中心" && <div className="panel-stats"><div><span>验证事件</span><strong>{events.filter((item) => item.type === "verification.completed").length}</strong></div><div><span>失败事件</span><strong>{events.filter((item) => item.type.endsWith("failed")).length}</strong></div><div><span>当前任务</span><strong>{tasks.find((task) => task.status === "VERIFYING")?.status || "无"}</strong></div></div>}<div className="panel-actions"><button className="secondary-button" onClick={onClose}>关闭</button></div></section></div>;
}

function AccountSettings({ onClose }: { onClose: () => void }) {
  const [user, setUser] = useState<AccountUser | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => { let live = true; void api<AccountUser>("/api/auth/me").then(value => { if (live) setUser(value); }).catch(cause => { if (live) setError(cause instanceof Error ? cause.message : String(cause)); }); return () => { live = false; }; }, []);
  const signOut = async () => {
    setBusy(true);
    try { await logout(); } catch { /* Local session is cleared even if the server is unavailable. */ }
    finally { onClose(); }
  };
  return <div className="account-settings"><p className="account-identity">{user ? user.username : "正在读取账户…"}</p><p className="form-help">工程、对话和模型服务仅对当前账户可见。</p>{error && <div className="capsule-error">{error}</div>}<div className="panel-actions"><button className="secondary-button" disabled={busy} onClick={() => void signOut()}>{busy ? "正在退出…" : "退出登录"}</button></div></div>;
}
