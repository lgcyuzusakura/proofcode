import { useState } from "react";
import { CheckCircle2, CircleAlert, PlugZap, Plus, RotateCw, Save, Search, Trash2 } from "lucide-react";
import { api } from "./api";
import { McpOAuthSettings } from "./McpOAuthSettings";
import { McpServiceGuide } from "./McpServiceGuide";

export type McpServer = { id: string; name: string; url: string; hasBearer: boolean; headerName?: string; hasHeader?: boolean; enabled: boolean; version: number };
type McpInspection = { savedVersion: number; result: { ok: boolean; code: string; stage: string; httpStatus?: number; protocol?: string; checkedAt: string; elapsedMs: number; tools: { name: string; qualifiedName: string; description: string; inputSchema: Record<string, unknown> }[] } };
const diagnosticMessages: Record<string, string> = {
 ok: "连接成功，已读取工具目录", configuration: "服务配置无效，请检查地址和别名。", dns: "无法解析服务地址，请检查域名。", private_host: "服务指向本机或内网，需由管理员启用内网连接。", timeout: "连接或读取工具目录超时，请稍后重试。", network: "无法连接服务，请检查网络、端口和 TLS 证书。", authentication: "服务拒绝认证，请检查访问令牌。", forbidden: "服务拒绝访问，请检查权限或服务端策略。", redirect: "服务返回重定向，请直接填写最终 MCP 地址。", rate_limit: "服务请求过于频繁，请稍后重试。", http: "服务返回异常 HTTP 状态，请检查 MCP 地址和服务状态。", protocol: "初始化响应不兼容，请确认服务支持 Streamable HTTP 工具协议。", catalog: "工具目录无效或分页异常，请检查服务实现。", tool_name: "工具完整名称超过 64 字符，请缩短服务别名或工具名。", catalog_size: "工具目录超过预览容量，请减少工具或参数说明。", unavailable: "服务器未安装 MCP 诊断程序，请配置后重试。", helper_error: "MCP 诊断程序异常，请检查服务器安装。", busy: "已有其他连接正在诊断，请稍后重试。", cancelled: "本次诊断已取消。"
};

export function McpServers({ projectId, servers, loadingError, busy, onBusy, onChanged }: { projectId: string; servers: McpServer[]; loadingError: string; busy: boolean; onBusy: (value: boolean) => void; onChanged: (servers: McpServer[]) => void }) {
 const path = `/api/projects/${projectId}/mcp-servers`;
 const [editing, setEditing] = useState<McpServer | null>(null), [name, setName] = useState(""), [url, setUrl] = useState(""), [bearer, setBearer] = useState(""), [clearBearer, setClearBearer] = useState(false), [headerName, setHeaderName] = useState(""), [headerValue, setHeaderValue] = useState(""), [clearHeader, setClearHeader] = useState(false), [enabled, setEnabled] = useState(true), [error, setError] = useState("");
 const [inspection, setInspection] = useState<McpInspection | null>(null), [query, setQuery] = useState(""), [operation, setOperation] = useState("");
 const [reloadFailed, setReloadFailed] = useState(false);
 const blocked = !!loadingError || reloadFailed;
 const dirty = !editing || name !== editing.name || url !== editing.url || bearer !== "" || clearBearer || headerName !== (editing.headerName || "") || headerValue !== "" || clearHeader || enabled !== editing.enabled;
 const invalidate = () => { setInspection(null); setQuery(""); };
 const edit = (value: McpServer | null) => { setEditing(value); setName(value?.name || ""); setUrl(value?.url || ""); setBearer(""); setClearBearer(false); setHeaderName(value?.headerName || ""); setHeaderValue(""); setClearHeader(false); setEnabled(value?.enabled ?? true); setError(""); invalidate(); };
 const probe = async () => {
  if (!editing || dirty || blocked) return;
  onBusy(true); setOperation("probe"); setError(""); invalidate();
  try { setInspection(await api<McpInspection>(`${path}/${editing.id}/probe?version=${editing.version}`, { method: "POST" })); }
  catch (cause) { setError(String(cause)); } finally { onBusy(false); setOperation(""); }
 };
 const reload = async () => {
  onBusy(true); setOperation("reload"); setError(""); invalidate();
  try { const values = await api<McpServer[]>(path); if (!Array.isArray(values)) throw new Error("MCP 服务列表不可用，请重新加载。"); onChanged(values); edit(values.find(value => value.id === editing?.id) || null); setReloadFailed(false); }
  catch (cause) { setError(String(cause)); setReloadFailed(true); } finally { onBusy(false); setOperation(""); }
 };
 const save = async () => {
  if (blocked) return;
  onBusy(true); setOperation("save"); setError(""); invalidate();
  try {
   const result = await api<McpServer>(`${path}${editing ? `/${editing.id}` : ""}`, { method: editing ? "PATCH" : "POST", body: JSON.stringify({ name, url, bearer, clearBearer, headerName, headerValue, clearHeader, enabled, version: editing?.version || 0 }) });
   onChanged([...servers.filter(value => value.id !== result.id), result].sort((a, b) => a.name.localeCompare(b.name))); edit(result);
  } catch (cause) { setError(String(cause)); } finally { onBusy(false); setOperation(""); }
 };
 const remove = async () => {
  if (!editing || !window.confirm(`删除 MCP 服务“${editing.name}”？引用它的智能体需要重新配置。已开始的任务保留原连接。`)) return;
  onBusy(true); setOperation("delete"); setError(""); invalidate();
  try { await api(`${path}/${editing.id}/delete?version=${editing.version}`, { method: "POST" }); onChanged(servers.filter(value => value.id !== editing.id)); edit(null); }
  catch (cause) { setError(String(cause)); } finally { onBusy(false); setOperation(""); }
 };
 const tools = inspection?.result.tools.filter(tool => `${tool.name} ${tool.description}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())) || [];
 return <div className="agent-platform-body mcp-servers-body">
  <aside><button type="button" disabled={busy} onClick={() => edit(null)}><Plus size={15}/>添加 MCP 服务</button><button type="button" disabled={busy} onClick={() => void reload()}><RotateCw size={15}/>重新加载服务</button>{servers.map(value => <button type="button" key={value.id} disabled={busy} aria-pressed={editing?.id === value.id} onClick={() => edit(value)}><strong>{value.name}</strong><small>{value.enabled ? "已启用" : "已停用"}</small></button>)}{!servers.length && <p>连接自己的工具服务，再在智能体中选择。</p>}</aside>
  <form onSubmit={event => { event.preventDefault(); void save(); }}><McpServiceGuide busy={busy || blocked} onBusy={onBusy} onCreated={value => { edit(null); setName(value.name); setUrl(value.url); setBearer(value.bearer); }}/><fieldset disabled={busy || blocked}>
   <label>服务别名<input required pattern="[A-Za-z][A-Za-z0-9_-]{0,31}" maxLength={32} value={name} onChange={event => { setName(event.target.value); invalidate(); }} placeholder="例如：project_docs"/></label>
   <label>服务地址<input required type="url" maxLength={2048} value={url} onChange={event => { setUrl(event.target.value); invalidate(); }} placeholder="https://tools.example.com/mcp"/></label>
   <p>使用 Streamable HTTP。服务需提供工具发现和调用；只读规划不会连接外部服务。</p>
   <label>访问令牌（可选）<input type="password" autoComplete="new-password" maxLength={4096} disabled={clearBearer} value={bearer} onChange={event => { setBearer(event.target.value); invalidate(); }} placeholder={editing?.hasBearer ? "已保存，留空保留" : "无认证的服务可留空"}/></label>
   {editing?.hasBearer && <label className="agent-platform-check"><input type="checkbox" checked={clearBearer} onChange={event => { setClearBearer(event.target.checked); setBearer(""); invalidate(); }}/>清除已保存的访问令牌</label>}
   <label>自定义认证头（可选）<input autoComplete="off" maxLength={128} disabled={clearHeader} value={headerName} onChange={event => { setHeaderName(event.target.value); invalidate(); }} placeholder="例如：x-api-key"/></label>
   <label>认证头值（可选）<input type="password" autoComplete="new-password" maxLength={4096} disabled={clearHeader} value={headerValue} onChange={event => { setHeaderValue(event.target.value); invalidate(); }} placeholder={editing?.hasHeader ? "已保存，留空保留" : "填写上方认证头对应的值"}/></label>
   {editing?.hasHeader && <label className="agent-platform-check"><input type="checkbox" checked={clearHeader} onChange={event => { setClearHeader(event.target.checked); if (event.target.checked) { setHeaderName(""); setHeaderValue(""); } else setHeaderName(editing.headerName || ""); invalidate(); }}/>清除已保存的自定义认证头</label>}
   {editing && <McpOAuthSettings key={editing.id} path={`${path}/${editing.id}`} version={editing.version} disabled={dirty || busy || blocked} onBusy={onBusy} onVersion={version => { const value = { ...editing, version }; setEditing(value); onChanged(servers.map(server => server.id === value.id ? value : server)); invalidate(); }} />}
   <label className="agent-platform-check"><input type="checkbox" checked={enabled} onChange={event => { setEnabled(event.target.checked); invalidate(); }}/>启用服务</label>
   <p>令牌加密保存。修改地址时需要重新输入或清除令牌；保存配置不会立即连接服务。</p>
   <footer><button type="submit" className="primary-button" disabled={!name.trim() || !url.trim()}><Save size={14}/>{operation === "save" ? "保存中…" : "保存服务"}</button><button type="button" disabled={dirty} onClick={() => void probe()}><PlugZap size={14}/>{operation === "probe" ? "诊断中…" : "诊断连接"}</button>{editing && <button type="button" onClick={() => void remove()}><Trash2 size={14}/>删除服务</button>}</footer>
   {dirty && <small>保存当前配置后，可以诊断连接并预览工具。</small>}
  </fieldset>{(error || loadingError) && <p role="alert" className="agent-platform-error">{error || loadingError}</p>}
  {inspection && <section className="mcp-inspection" aria-label="MCP 连接诊断">
   <div className={`mcp-inspection-status${inspection.result.ok ? " is-success" : " is-failure"}`} role="status">{inspection.result.ok ? <CheckCircle2 size={17}/> : <CircleAlert size={17}/>}<strong>{diagnosticMessages[inspection.result.code] || "诊断失败，请重新加载配置后重试。"}</strong></div>
   <small>配置版本 {inspection.savedVersion} · {new Date(inspection.result.checkedAt).toLocaleString()} · {inspection.result.elapsedMs} ms{inspection.result.protocol && ` · 协议 ${inspection.result.protocol}`}{inspection.result.httpStatus && ` · HTTP ${inspection.result.httpStatus}`}</small>
   {inspection.result.ok && <>
    <div className="mcp-tool-heading"><h3>工具目录 <span>{inspection.result.tools.length}</span></h3><label><Search size={14}/><input aria-label="搜索 MCP 工具" type="search" value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索名称或描述"/></label></div>
    <div className="mcp-tool-list">{tools.map(tool => <details key={tool.qualifiedName}><summary><strong>{tool.name}</strong><span>{tool.description || "暂无说明"}</span></summary><code>{tool.qualifiedName}</code><pre aria-label={`${tool.name} 输入参数`}>{JSON.stringify(tool.inputSchema, null, 2)}</pre></details>)}</div>
    {!tools.length && <p>{inspection.result.tools.length ? "没有匹配的工具。" : "服务连接正常，当前没有发布工具。"}</p>}
    <p>已验证连接和工具发现，未执行任何工具。工具说明由服务提供，实际调用仍需批准。</p>
   </>}
  </section>}
  <p className="agent-platform-note">在“智能体”中勾选服务后，新代码任务启动时连接并读取工具目录。外部工具每次调用仍需批准；停用或删除不改变已开始任务的连接快照。</p></form>
 </div>;
}
