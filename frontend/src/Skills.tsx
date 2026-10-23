import { useRef, useState } from "react";
import { Download, FileUp, Plus, RotateCcw, Save, Trash2 } from "lucide-react";
import { api } from "./api";

export type Skill = { id: string; name: string; description: string; content: string; enabled: boolean; source: string; version: number; hash: string };
export type SkillInfo = Pick<Skill, "id" | "name" | "description" | "source" | "version" | "hash">;

export function SkillEvidence({ skills }: { skills: SkillInfo[] }) {
 if (!skills.length) return null;
 return <section className="agent-skill-evidence" aria-label="本次可用 Skills"><strong>可按需读取的 Skills</strong>{skills.map(skill => <article key={skill.id}><strong>{skill.name}</strong><small>当前项目 · 版本 {skill.version} · {skill.source === "SKILL.md" ? "导入" : "编辑"}</small><p>{skill.description}</p><code title={skill.hash}>SHA-256 {skill.hash.slice(0, 12)}</code></article>)}</section>;
}

export function Skills({ projectId, skills, loading, loadError, busy, onBusy, onChanged, onReload }: { projectId: string; skills: Skill[]; loading: boolean; loadError: string; busy: boolean; onBusy: (value: boolean) => void; onChanged: (values: Skill[]) => void; onReload: () => Promise<Skill[]> }) {
 const path = `/api/projects/${projectId}/skills`, file = useRef<HTMLInputElement>(null);
 const [editing, setEditing] = useState<Skill | null>(null), [name, setName] = useState(""), [description, setDescription] = useState(""), [content, setContent] = useState(""), [enabled, setEnabled] = useState(true), [error, setError] = useState(""), [status, setStatus] = useState("");
 const blocked = busy || loading || !!loadError;
 const edit = (skill: Skill | null) => { setEditing(skill); setName(skill?.name || ""); setDescription(skill?.description || ""); setContent(skill?.content || ""); setEnabled(skill?.enabled ?? true); setError(""); setStatus(""); };
 const accept = (value: Skill) => { onChanged([...skills.filter(skill => skill.id !== value.id), value].sort((a, b) => a.name.localeCompare(b.name))); edit(value); };
 const save = async () => {
  onBusy(true); setError(""); setStatus("");
  try { accept(await api<Skill>(`${path}${editing ? `/${editing.id}` : ""}`, { method: editing ? "PATCH" : "POST", body: JSON.stringify({ name, description, content, enabled, version: editing?.version ?? 0 }) })); setStatus("Skill 已保存，新任务会使用这份版本。"); }
  catch (cause) { setError(String(cause)); } finally { onBusy(false); }
 };
 const importFile = async (selected?: File) => {
  if (!selected) return;
  onBusy(true); setError(""); setStatus("");
  try {
   if (!selected.name.toLowerCase().endsWith(".md") || selected.size > 128 * 1024) throw new Error("请选择不超过 128 KiB 的 SKILL.md 文件。");
   const markdown = new TextDecoder("utf-8", { fatal: true }).decode(await selected.arrayBuffer());
   accept(await api<Skill>(`${path}/import`, { method: "POST", body: JSON.stringify({ markdown, enabled: true, version: 0 }) }));
   setStatus("SKILL.md 已导入，可以编辑或分配给智能体。");
  } catch (cause) { setError(String(cause)); } finally { if (file.current) file.current.value = ""; onBusy(false); }
 };
 const remove = async () => {
  if (!editing || !window.confirm(`删除 Skill“${editing.name}”？已开始的任务保留原版本；引用它的智能体需要重新选择。`)) return;
  onBusy(true); setError("");
  try { await api(`${path}/${editing.id}/delete?version=${editing.version}`, { method: "POST" }); onChanged(skills.filter(skill => skill.id !== editing.id)); edit(null); setStatus("Skill 已删除。"); }
  catch (cause) { setError(String(cause)); } finally { onBusy(false); }
 };
 const reload = async () => { onBusy(true); setError(""); try { const values = await onReload(); edit(values.find(skill => skill.id === editing?.id) || null); setStatus("已加载最新 Skills。"); } catch (cause) { setError(String(cause)); } finally { onBusy(false); } };
 const download = () => {
  if (!editing) return;
  const data = `---\nname: ${JSON.stringify(editing.name)}\ndescription: ${JSON.stringify(editing.description)}\n---\n\n${editing.content}\n`;
  const url = URL.createObjectURL(new Blob([data], { type: "text/markdown;charset=utf-8" })), link = document.createElement("a");
  link.href = url; link.download = `${editing.name}-SKILL.md`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
 };
 return <div className="agent-platform-body agent-skills-body">
  <aside><button type="button" disabled={blocked} onClick={() => edit(null)}><Plus size={15}/>新建 Skill</button><button type="button" disabled={blocked} onClick={() => file.current?.click()}><FileUp size={15}/>导入 SKILL.md</button><input ref={file} type="file" accept=".md,text/markdown" hidden aria-label="导入 Skill 文件" disabled={blocked} onChange={event => void importFile(event.target.files?.[0])}/>{skills.map(skill => <button type="button" key={skill.id} disabled={busy} aria-pressed={editing?.id === skill.id} onClick={() => edit(skill)}><strong>{skill.name}</strong><small>{skill.enabled ? "可按需读取" : "已停用"}</small></button>)}{!loading && !loadError && !skills.length && <p>还没有保存的工作流程。</p>}</aside>
  <form onSubmit={event => { event.preventDefault(); void save(); }}><fieldset disabled={blocked}>
   <label>Skill 名称<input aria-label="Skill 名称" required pattern="[a-z0-9]+(-[a-z0-9]+)*" maxLength={64} value={name} onChange={event => setName(event.target.value)} placeholder="例如：project-review"/></label>
   <label>适用场景<textarea aria-label="适用场景" required rows={2} maxLength={500} value={description} onChange={event => setDescription(event.target.value)} placeholder="例如：审查接口修改，检查权限、兼容性与测试覆盖。"/></label>
   <label>工作流程<textarea aria-label="工作流程" className="agent-skill-content" required rows={9} maxLength={24000} value={content} onChange={event => setContent(event.target.value)} placeholder="写下这个 Skill 的步骤、检查标准和交付要求。"/></label>
   <label className="agent-platform-check"><input type="checkbox" checked={enabled} onChange={event => setEnabled(event.target.checked)}/>启用 Skill</label>
   {editing && <div className="agent-skill-version"><span>版本 {editing.version} · {editing.source === "SKILL.md" ? "来自 SKILL.md" : "手动编辑"}</span><code title={editing.hash}>SHA-256 {editing.hash.slice(0, 12)}</code></div>}
   <footer><button type="submit" className="primary-button" disabled={!name.trim() || !description.trim() || !content.trim()}><Save size={14}/>{busy ? "保存中…" : "保存 Skill"}</button>{editing && <><button type="button" onClick={download}><Download size={14}/>导出</button><button type="button" onClick={() => void remove()}><Trash2 size={14}/>删除 Skill</button></>}</footer>
  </fieldset>{loading && <p role="status">正在读取 Skills…</p>}{(error || loadError) && <p role="alert" className="agent-platform-error">{error || loadError}</p>}{status && <p role="status">{status}</p>}<button type="button" disabled={busy || loading} onClick={() => void reload()}><RotateCcw size={14}/>重新加载 Skills</button><p className="agent-platform-note">默认智能体可读取项目中已启用的 Skills；自定义智能体使用勾选的条目。只导入文档正文，配套文件需另行提供。工具权限与审批规则仍然生效。</p></form>
 </div>;
}
