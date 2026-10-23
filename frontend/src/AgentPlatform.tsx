import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Bot, BookOpen, Library, Plus, Save, Trash2, UserRound, Wrench, X } from "lucide-react";
import { api } from "./api";
import type { ModelProvider, TaskEvent } from "./types";
import "./agent-platform.css";
import { McpServers, type McpServer } from "./McpServers";
import { PersonalPreferences, PreferenceEvidence, type Personalization } from "./PersonalPreferences";
import { Skills, SkillEvidence, type Skill, type SkillInfo } from "./Skills";

export type AgentModelChoice = { providerId: string; model: string };
type Profile = { instructions: string; allowedTools: string[]; mcpServers: string[]; readOnly: boolean; modelChoice?: AgentModelChoice | null; remoteMcpIds?: string[]; skillIds?: string[] };
type Memory = { content: string; keywords: string; pinned: boolean; enabled: boolean };
type Resource = { id: string; name: string; version: number; profile: Profile | null; memory: Memory | null };
type Evidence = { id: string; name: string; content: string; version: number; scope?: string; score?: number; reason?: string };
const emptyProfile = (): Profile => ({ instructions: "", allowedTools: [], mcpServers: [], readOnly: false });
const emptyMemory = (): Memory => ({ content: "", keywords: "", pinned: false, enabled: true });
const splitNames = (text: string) => [...new Set(text.split(/[\s,，]+/).filter(Boolean))];

export function AgentConfiguration({ events }: { events: TaskEvent[] }) {
 const entry = [...events].reverse().find(value => value.type === "agent.configuration");
 if (!entry) return null;
 const memories = (Array.isArray(entry.payload?.memories) ? entry.payload.memories : []) as Evidence[];
 const personalization = entry.payload?.personalization as Personalization | undefined;
 const hasPersonalization = !!personalization?.values && Object.values(personalization.values).some(Boolean);
 const skills = (Array.isArray(entry.payload?.skills) ? entry.payload.skills : []) as SkillInfo[];
 return <details className="agent-used-configuration"><summary>{String(entry.payload?.name || "默认智能体")}{entry.payload?.readOnly ? " · 只读" : ""} · 项目记忆 {memories.length} 条{hasPersonalization ? " · 已应用个人偏好" : ""}{skills.length > 0 ? ` · Skills ${skills.length}` : ""}</summary><p>本次任务使用创建时固定的配置和上下文。</p><PreferenceEvidence value={personalization}/><SkillEvidence skills={skills}/>{memories.map(item => <article key={item.id}><strong>{item.name}</strong><small>{item.scope === "project" ? "当前项目" : item.scope || "项目记忆"} · 版本 {item.version}{typeof item.score === "number" ? ` · 匹配度 ${item.score}` : ""}</small>{item.reason && <small>{item.reason}</small>}<p>{item.content}</p></article>)}</details>;
}

export function AgentPlatform({ projectId, conversationId, query, disabled, onBusy, providers, onModelChoice }: { projectId: string; conversationId?: string; query: string; disabled: boolean; onBusy: (busy: boolean) => void; providers: ModelProvider[]; onModelChoice: (choice: AgentModelChoice | null) => void }) {
 const path = `/api/projects/${projectId}/agent-platform`;
 const [agents, setAgents] = useState<Resource[]>([]), [memories, setMemories] = useState<Resource[]>([]);
 const [selected, setSelected] = useState("");
 const [open, setOpen] = useState(false), [tab, setTab] = useState<"AGENT" | "PERSONAL" | "MEMORY" | "MCP" | "SKILL">("AGENT");
 const [skills, setSkills] = useState<Skill[]>([]), [skillError, setSkillError] = useState(""), [skillsLoading, setSkillsLoading] = useState(false);
 const reloadSkills = async () => { setSkillsLoading(true); setSkillError(""); try { const values = await api<Skill[]>(`/api/projects/${projectId}/skills`); if (!Array.isArray(values)) throw new Error("Skills 列表不可用，请重新加载。"); setSkills(values); return values; } catch (cause) { setSkillError(String(cause)); throw cause; } finally { setSkillsLoading(false); } };
 useEffect(() => { if (!open) return; let live = true; setSkills([]); setSkillError(""); setSkillsLoading(true); void api<Skill[]>(`/api/projects/${projectId}/skills`).then(values => { if (!Array.isArray(values)) throw new Error("Skills 列表不可用，请重新加载。"); if (live) setSkills(values); }).catch(cause => { if (live) setSkillError(String(cause)); }).finally(() => { if (live) setSkillsLoading(false); }); return () => { live = false; }; }, [open, projectId]);
 const [remoteServers, updateRemoteServers] = useState<McpServer[]>([]), [mcpError, setMcpError] = useState(""), [mcpLoading, setMcpLoading] = useState(false);
 const setRemoteServers = (values: McpServer[]) => { updateRemoteServers(values); setMcpError(""); };
 useEffect(() => { if (!open) return; let live = true; setMcpError(""); setMcpLoading(true); void api<McpServer[]>(`/api/projects/${projectId}/mcp-servers`).then(values => { if (!Array.isArray(values)) throw new Error("MCP 服务列表不可用，请关闭设置后重试。"); if (live) setRemoteServers(values); }).catch(cause => { if (live) setMcpError(String(cause)); }).finally(() => { if (live) setMcpLoading(false); }); return () => { live = false; }; }, [open, projectId]);
 const [editing, setEditing] = useState<Resource | null>(null), [name, setName] = useState("");
 const [toolsText, setToolsText] = useState(""), [mcpText, setMcpText] = useState("");
 const [profile, setProfile] = useState<Profile>(emptyProfile), [memory, setMemory] = useState<Memory>(emptyMemory);
 const [busy, setBusy] = useState(false), [loading, setLoading] = useState(true), [error, setError] = useState("");
 const [preview, setPreview] = useState<Evidence[] | null>(null);
 const trigger = useRef<HTMLButtonElement>(null), dialog = useRef<HTMLElement>(null);
 useEffect(() => { if (!open) return; const ensureVisible = () => dialog.current?.querySelector<HTMLElement>('nav button[aria-pressed="true"]')?.scrollIntoView({ block: "nearest", inline: "nearest" }); ensureVisible(); window.addEventListener("resize", ensureVisible); return () => window.removeEventListener("resize", ensureVisible); }, [open, tab]);
 const generation = useRef(0);
 const setWorking = (value: boolean) => { setBusy(value); onBusy(value); };
 useEffect(() => { onModelChoice(loading ? null : agents.find(item => item.id === selected)?.profile?.modelChoice || null); }, [agents, selected, loading, onModelChoice]);
 useEffect(() => {
  const id = ++generation.current; setError(""); setLoading(true); onBusy(true);
  if (!projectId || !conversationId) { setLoading(false); onBusy(false); return; }
  void Promise.all([api<Resource[]>(`${path}/AGENT`), api<Resource[]>(`${path}/MEMORY`), api<{ profileId: string | null }>(`${path}/selection/${conversationId}`)])
   .then(([a, m, selection]) => { if (!Array.isArray(a) || !Array.isArray(m) || !selection || !("profileId" in selection)) throw new Error("智能体配置响应无效，请重新打开对话后重试。"); if (generation.current === id) { setAgents(a); setMemories(m); setSelected(a.some(v => v.id === selection.profileId) ? selection.profileId || "" : ""); } })
   .catch(e => { if (generation.current === id) setError(String(e)); })
   .finally(() => { if (generation.current === id) { setLoading(false); onBusy(false); } });
  return () => { generation.current++; onBusy(false); };
 }, [projectId, conversationId]);
 const edit = (item: Resource | null) => { setEditing(item); setName(item?.name || ""); setProfile(item?.profile || emptyProfile()); setToolsText(item?.profile?.allowedTools.join(", ") || ""); setMcpText(item?.profile?.mcpServers.join(", ") || ""); setMemory(item?.memory || emptyMemory()); setError(""); };
 const close = () => { if (busy) return; setOpen(false); trigger.current?.focus(); };
 useEffect(() => {
  if (!open) return;
  dialog.current?.querySelector<HTMLButtonElement>("button")?.focus();
  const key = (event: KeyboardEvent) => {
   if (event.key === "Escape" && !busy) { event.preventDefault(); setOpen(false); trigger.current?.focus(); }
   if (event.key === "Tab") {
    const items = dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), summary, [tabindex="0"]');
    if (!items?.length) return;
    const first = items[0], last = items[items.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
   }
  };
  window.addEventListener("keydown", key); return () => window.removeEventListener("keydown", key);
 }, [open, busy]);
 const choose = async (profileId: string) => {
  if (!conversationId || busy || loading) return; setWorking(true); setError("");
  try { await api(`${path}/selection/${conversationId}`, { method: "PATCH", body: JSON.stringify({ profileId: profileId || null }) }); setSelected(profileId); }
  catch (e) { setError(String(e)); } finally { setWorking(false); }
 };
 const save = async () => {
  setWorking(true); setError("");
  try {
   const value = await api<Resource>(`${path}/${tab}${editing ? `/${editing.id}` : ""}`, { method: editing ? "PATCH" : "POST", body: JSON.stringify({ name, version: editing?.version || 0, profile: tab === "AGENT" ? { ...profile, allowedTools: splitNames(toolsText), mcpServers: splitNames(mcpText) } : null, memory: tab === "MEMORY" ? memory : null }) });
   const update = (values: Resource[]) => [...values.filter(item => item.id !== value.id), value].sort((a, b) => a.name.localeCompare(b.name));
   if (tab === "AGENT") setAgents(update); else setMemories(update);
   edit(value); setPreview(null);
  } catch (e) { setError(String(e)); } finally { setWorking(false); }
 };
 const remove = async () => {
  if (!editing || !window.confirm(`删除“${editing.name}”？已开始的任务会保留原配置。`)) return;
  setWorking(true); setError("");
  try {
   await api(`${path}/${tab}/${editing.id}/delete?version=${editing.version}`, { method: "POST" });
   if (tab === "AGENT") { setAgents(old => old.filter(v => v.id !== editing.id)); if (selected === editing.id) setSelected(""); } else setMemories(old => old.filter(v => v.id !== editing.id));
   edit(null); setPreview(null);
  } catch (e) { setError(String(e)); } finally { setWorking(false); }
 };
 const inspect = async () => { setWorking(true); setError(""); try { setPreview(await api<Evidence[]>(`${path}/preview?query=${encodeURIComponent(query)}`)); } catch (e) { setError(String(e)); } finally { setWorking(false); } };
 return <div className="agent-profile-control">
  <select aria-label="当前智能体" disabled={disabled || busy || loading || !conversationId} value={selected} onChange={e => void choose(e.target.value)}><option value="">默认智能体</option>{agents.map(item => <option key={item.id} value={item.id}>{item.name}{item.profile?.readOnly ? "（只读）" : ""}</option>)}</select>
  <button ref={trigger} type="button" disabled={!projectId || !conversationId || loading} onClick={() => { edit(null); setOpen(true); }}><Bot size={14}/>智能体与记忆</button>
  {!open && error && <span role="alert" className="agent-platform-error">{error}</span>}
  {open && createPortal(<div className="agent-platform-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) close(); }}><section ref={dialog} className="agent-platform-panel" role="dialog" aria-modal="true" aria-labelledby="agent-platform-title">
   <header><div><h2 id="agent-platform-title">智能体、偏好与项目记忆</h2><p>账户偏好跨项目生效；项目记忆只在当前项目检索。</p></div><button type="button" aria-label="关闭智能体设置" disabled={busy} onClick={close}><X size={18}/></button></header>
   <nav aria-label="智能体设置分类"><button type="button" aria-pressed={tab === "AGENT"} disabled={busy} onClick={() => { setTab("AGENT"); edit(null); }}><Bot size={15}/>智能体</button><button type="button" aria-pressed={tab === "PERSONAL"} disabled={busy} onClick={() => { setTab("PERSONAL"); edit(null); }}><UserRound size={15}/>个人偏好</button><button type="button" aria-pressed={tab === "MEMORY"} disabled={busy} onClick={() => { setTab("MEMORY"); edit(null); }}><BookOpen size={15}/>项目记忆</button><button type="button" aria-pressed={tab === "SKILL"} disabled={busy} onClick={() => { setTab("SKILL"); edit(null); }}><Library size={15}/>Skills</button><button type="button" aria-pressed={tab === "MCP"} disabled={busy} onClick={() => { setTab("MCP"); edit(null); }}><Wrench size={15}/>MCP 服务</button></nav>
   {tab === "SKILL" ? <Skills key={projectId} projectId={projectId} skills={skills} loading={skillsLoading} loadError={skillError} busy={busy} onBusy={setWorking} onChanged={setSkills} onReload={reloadSkills}/> : tab === "MCP" ? <McpServers projectId={projectId} servers={remoteServers} loadingError={mcpError} busy={busy || mcpLoading} onBusy={setWorking} onChanged={setRemoteServers}/> : tab === "PERSONAL" ? <PersonalPreferences onBusy={setWorking}/> : <div className="agent-platform-body"><aside><button type="button" disabled={busy} onClick={() => edit(null)}><Plus size={15}/>新建{tab === "AGENT" ? "智能体" : "项目记忆"}</button>{(tab === "AGENT" ? agents : memories).map(item => <button type="button" key={item.id} disabled={busy} aria-pressed={editing?.id === item.id} onClick={() => edit(item)}><strong>{item.name}</strong><small>{item.profile ? item.profile.readOnly ? "只读调查" : "代码协作" : item.memory?.enabled ? item.memory.pinned ? "始终参考" : "按需检索" : "已停用"}</small></button>)}{!(tab === "AGENT" ? agents : memories).length && <p>还没有保存内容。可以先创建一个。</p>}</aside>
   <form onSubmit={e => { e.preventDefault(); void save(); }}><fieldset disabled={busy}><label>名称<input required maxLength={160} value={name} onChange={e => setName(e.target.value)}/></label>
    {tab === "AGENT" ? <>
     <label>默认模型服务<select aria-label="默认模型服务" value={profile.modelChoice?.providerId || ""} onChange={e => setProfile({ ...profile, modelChoice: e.target.value ? { providerId: e.target.value, model: "" } : null })}><option value="">使用输入区选择</option>{profile.modelChoice?.providerId && !providers.some(p => p.id === profile.modelChoice?.providerId) && <option value={profile.modelChoice.providerId}>原服务已不可用，请重新选择</option>}{providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></label>
     {profile.modelChoice && <label>默认模型<select aria-label="默认模型" required value={profile.modelChoice.model} onChange={e => setProfile({ ...profile, modelChoice: { ...profile.modelChoice!, model: e.target.value } })}><option value="">选择可用模型</option>{profile.modelChoice.model && !providers.find(p => p.id === profile.modelChoice?.providerId)?.models.some(m => m.id === profile.modelChoice?.model) && <option value={profile.modelChoice.model}>原模型已不可用：{profile.modelChoice.model}</option>}{providers.find(p => p.id === profile.modelChoice?.providerId)?.models.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select></label>}
     <p>绑定后，新任务使用这个模型；选择“使用输入区选择”可恢复自由切换。</p>
     <label>角色与工作方式<textarea rows={6} maxLength={16000} value={profile.instructions} onChange={e => setProfile({ ...profile, instructions: e.target.value })} placeholder="例如：先检查现有测试，优先小范围修改，用中文解释验证结果。"/></label>
     <label className="agent-platform-check"><input type="checkbox" checked={profile.readOnly} onChange={e => setProfile({ ...profile, readOnly: e.target.checked })}/>只读调查，禁止修改与执行命令</label>
     <label>允许的工具<input value={toolsText} onChange={e => setToolsText(e.target.value)} placeholder="留空使用默认工具；或填写 read_file, search_code, git_diff"/></label>
     <p>填写工具名称可限制范围。工具原有的审批要求仍生效。</p>
     <section className="agent-remote-servers" aria-label="智能体 Skills"><strong>Skills</strong>{skills.map(skill => <label className="agent-platform-check" key={skill.id}><input type="checkbox" checked={(profile.skillIds || []).includes(skill.id)} disabled={skillsLoading || !!skillError || (!skill.enabled && !(profile.skillIds || []).includes(skill.id))} onChange={event => setProfile({ ...profile, skillIds: event.target.checked ? [...(profile.skillIds || []), skill.id] : (profile.skillIds || []).filter(id => id !== skill.id) })}/>{skill.name}{!skill.enabled && "（已停用）"}</label>)}{!skillsLoading && !skillError && (profile.skillIds || []).filter(id => !skills.some(skill => skill.id === id)).map(id => <label className="agent-platform-check" key={id}><input type="checkbox" checked onChange={() => setProfile({ ...profile, skillIds: (profile.skillIds || []).filter(value => value !== id) })}/>Skill 已不可用，取消勾选后保存</label>)}{skillsLoading ? <p>正在读取 Skills…</p> : skillError ? <p role="alert" className="agent-platform-error">{skillError}</p> : <p>{skills.length ? "只向这个智能体提供勾选的 Skills。设置工具白名单时，需要允许 skill_read。" : "在 Skills 中创建或导入工作流程。"}</p>}</section>
     <label>MCP 服务别名<input value={mcpText} onChange={e => setMcpText(e.target.value)} placeholder="例如：docs, issue_tracker"/></label><p>选择运行环境已配置的服务。外部调用需要批准；只读智能体禁止外部执行。</p>
     <section className="agent-remote-servers" aria-label="远程 MCP 服务"><strong>远程 MCP 服务</strong>{remoteServers.map(server => <label className="agent-platform-check" key={server.id}><input type="checkbox" checked={(profile.remoteMcpIds || []).includes(server.id)} disabled={mcpLoading || !!mcpError || (!server.enabled && !(profile.remoteMcpIds || []).includes(server.id))} onChange={event => setProfile({ ...profile, remoteMcpIds: event.target.checked ? [...(profile.remoteMcpIds || []), server.id] : (profile.remoteMcpIds || []).filter(id => id !== server.id) })}/>{server.name}{!server.enabled && "（已停用）"}</label>)}{!mcpLoading && !mcpError && (profile.remoteMcpIds || []).filter(id => !remoteServers.some(server => server.id === id)).map(id => <label className="agent-platform-check" key={id}><input type="checkbox" checked onChange={() => setProfile({ ...profile, remoteMcpIds: (profile.remoteMcpIds || []).filter(value => value !== id) })}/>服务已不可用，取消勾选后保存</label>)}{!mcpLoading && !mcpError && !remoteServers.length && <p>在“MCP 服务”中添加自己的工具服务。</p>}{mcpLoading && <p>正在读取服务列表…</p>}{mcpError && <p role="alert" className="agent-platform-error">{mcpError}</p>}</section>
    </> : <>
     <label>记忆内容<textarea required rows={6} maxLength={8000} value={memory.content} onChange={e => setMemory({ ...memory, content: e.target.value })} placeholder="例如：项目使用 pytest；新增接口需要参数校验与测试。"/></label>
     <label>检索关键词<input maxLength={1000} value={memory.keywords} onChange={e => setMemory({ ...memory, keywords: e.target.value })} placeholder="例如：接口 测试 pytest"/></label>
     <label className="agent-platform-check"><input type="checkbox" checked={memory.pinned} onChange={e => setMemory({ ...memory, pinned: e.target.checked })}/>始终参考这条偏好</label>
     <label className="agent-platform-check"><input type="checkbox" checked={memory.enabled} onChange={e => setMemory({ ...memory, enabled: e.target.checked })}/>启用</label>
      <p>这条内容只属于当前项目。普通记忆按当前问题检索，最多带入 8 条、共 12000 字符；账户级表达偏好请使用“个人偏好”。</p>
    </>}
    <footer><button type="submit" className="primary-button" disabled={!name.trim() || (tab === "MEMORY" && !memory.content.trim())}><Save size={14}/>{busy ? "保存中…" : "保存"}</button>{editing && <button type="button" onClick={() => void remove()}><Trash2 size={14}/>删除</button>}{tab === "MEMORY" && <button type="button" onClick={() => void inspect()}>预览本次引用</button>}</footer>
   </fieldset>{error && <p role="alert" className="agent-platform-error">{error}</p>}{preview && <section className="agent-memory-preview" aria-label="本次记忆引用"><h3>本次草稿会参考</h3>{preview.length ? preview.map(item => <article key={item.id}><strong>{item.name}</strong><small>当前项目 · 版本 {item.version}{item.reason ? ` · ${item.reason}` : ""}</small><p>{item.content}</p></article>) : <p>没有匹配的已启用记忆。</p>}</section>}<p className="agent-platform-note">更改只影响新任务。正在执行或恢复的任务使用创建时保存的配置。</p></form></div>}
   </section></div>, document.body)}
 </div>;
}
