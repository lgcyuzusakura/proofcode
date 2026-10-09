import { FormEvent, useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Clock3, Database, FileCode2, Filter, GitBranch, GripVertical, KeyRound, Layers3, Loader2, LockKeyhole, Pencil, Play, Plus, RefreshCw, RotateCcw, Search, ShieldCheck, Trash2, X, XCircle } from "lucide-react";
import { api, post, scopedPath } from "./api";
import type { Project } from "./types";
import { compileCache, compileQuery, formatCell, parseJSON, queryPage, recoveryAvailable, resourceRequest, rowMutation, sessionActive, tableKey, tableQuery } from "./dataModel";
import type { Approval, DataAudit, DataIR, DataPlan, DataResource, DataSession, ExecutionResult, FilterGroup, QueryDraft, SchemaSnapshot, TableMetadata } from "./dataModel";

type PlanResult = { plan: DataPlan; result: ExecutionResult };
type EditRow = { table: TableMetadata; original: Record<string, unknown>; rowHash: string };
type CacheEntry = { key: string; type: string; ttlMs: number; sizeBytes?: number; readAt?: string };
const initialQuery: QueryDraft = { selected: [], groups: [], connective: "and", orderBy: [], limit: 50, offset: 0 };
const supportedOperators = [["eq", "="], ["ne", "≠"], ["lt", "<"], ["lte", "≤"], ["gt", ">"], ["gte", "≥"], ["in", "IN"], ["is_null", "IS NULL"], ["is_not_null", "IS NOT NULL"]];

export function DataWorkbench({ project, onNotice }: { project: Project; onNotice: (message: string) => void }) {
  const [resources, setResources] = useState<DataResource[]>([]);
  const [sessions, setSessions] = useState<DataSession[]>([]);
  const [session, setSession] = useState<DataSession | null>(null);
  const [selectedResource, setSelectedResource] = useState<DataResource | null>(null);
  const [schema, setSchema] = useState<SchemaSnapshot | null>(null);
  const [selectedTable, setSelectedTable] = useState<TableMetadata | null>(null);
  const [query, setQuery] = useState<QueryDraft>(initialQuery);
  const [plans, setPlans] = useState<DataPlan[]>([]);
  const [audits, setAudits] = useState<DataAudit[]>([]);
  const [selectedPlan, setSelectedPlan] = useState<DataPlan | null>(null);
  const [results, setResults] = useState<Record<string, PlanResult>>({});
  const [explain, setExplain] = useState("");
  const [showResourceForm, setShowResourceForm] = useState(false);
  const [resourceToEdit, setResourceToEdit] = useState<DataResource | null>(null);
  const [showSessionForm, setShowSessionForm] = useState(false);
  const [activeTab, setActiveTab] = useState<"query" | "cache" | "history" | "audit" | "migration">("query");
  const [busy, setBusy] = useState(""); const [error, setError] = useState("");
  const [tableFilter, setTableFilter] = useState("");
  const [cacheOperation, setCacheOperation] = useState<"scan" | "get" | "set" | "delete" | "expire">("scan");
  const [cacheKey, setCacheKey] = useState(""); const [cacheValue, setCacheValue] = useState(""); const [cacheTTL, setCacheTTL] = useState(3600);
  const [cacheCursor, setCacheCursor] = useState("0"); const [cacheCount, setCacheCount] = useState(50);
  const [cacheEntries, setCacheEntries] = useState<CacheEntry[]>([]); const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [cacheRead, setCacheRead] = useState<ExecutionResult | null>(null);
  const [editRow, setEditRow] = useState<EditRow | null>(null);
  const capability = useRef<Map<string, Approval>>(new Map());
  const planKeys = useRef<Map<string, string>>(new Map());
  const selectedResourceRef = useRef<string | null>(null);
  const resourceStateRef = useRef<DataResource | null>(null);
  const selectedPlanRef = useRef<string | null>(null);
  const sessionRef = useRef<DataSession | null>(null);
  const busyRef = useRef(false);
  const mounted = useRef(true);
  const selectionGeneration = useRef(0);
  const loadRequest = useRef(0);
  const ledgerRequest = useRef(0);
  const base = `${scopedPath(project.id)}/data`;
  const current = (generation?: number) => mounted.current && (generation === undefined || selectionGeneration.current === generation);
  const begin = (operation: string) => { if (busyRef.current) return false; busyRef.current = true; setBusy(operation); setError(""); return true; };
  const finish = () => { busyRef.current = false; if (current()) setBusy(""); };
  const pickPlan = (value: DataPlan | null) => { selectedPlanRef.current = value?.id || null; setSelectedPlan(value); setExplain(""); };
  const pickSession = (value: DataSession | null) => {
    selectionGeneration.current++; sessionRef.current = value; setSession(value); setSchema(null); setSelectedTable(null); setQuery(initialQuery); pickPlan(null);
    setResults({}); setCacheEntries([]); setCacheRead(null); setNextCursor(null); setCacheCursor("0"); setEditRow(null); capability.current.clear();
  };
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; capability.current.clear(); }; }, []);

  const load = async () => {
    const generation = selectionGeneration.current, request = ++loadRequest.current;
    try {
      const values = await Promise.all([api<DataResource[]>(`${base}/resources`), api<DataSession[]>(`${base}/sessions`), api<DataPlan[]>(`${base}/plans`), api<DataAudit[]>(`${base}/audit`)]);
      if (!current(generation) || request !== loadRequest.current) return;
      setResources(values[0]); setSessions(values[1]); setPlans(values[2]); setAudits(values[3]);
      const nextSession = values[1].find((value) => value.id === sessionRef.current?.id && sessionActive(value)) || values[1].find((value) => value.purpose === "MANAGEMENT" && sessionActive(value)) || null;
      if (nextSession?.id !== sessionRef.current?.id) pickSession(nextSession);
      else { sessionRef.current = nextSession; setSession(nextSession); }
      const oldResource = resourceStateRef.current;
      const nextResource = values[0].find((value) => value.id === oldResource?.id) || null;
      if (nextResource && (oldResource?.resourceVersion !== nextResource.resourceVersion || oldResource.policyVersion !== nextResource.policyVersion || oldResource.active !== nextResource.active)) selectResource(nextResource);
      else { resourceStateRef.current = nextResource; selectedResourceRef.current = nextResource?.id || null; setSelectedResource(nextResource); }
      if (!nextResource && oldResource) { selectionGeneration.current++; setSchema(null); setSelectedTable(null); setQuery(initialQuery); setResults({}); setCacheEntries([]); setCacheRead(null); setEditRow(null); capability.current.clear(); pickPlan(null); }
      const nextPlan = values[2].find((value) => value.id === selectedPlanRef.current) || null;
      selectedPlanRef.current = nextPlan?.id || null; setSelectedPlan(nextPlan);
    } catch (cause) { if (current(generation) && request === loadRequest.current) setError(`数据工作台加载失败：${String(cause)}`); }
  };
  useEffect(() => { void load(); }, [project.id]);
  const refreshLedger = async () => {
    const generation = selectionGeneration.current, request = ++ledgerRequest.current;
    const [newPlans, newAudits, newSessions] = await Promise.all([api<DataPlan[]>(`${base}/plans`), api<DataAudit[]>(`${base}/audit`), api<DataSession[]>(`${base}/sessions`)]);
    if (!current(generation) || request !== ledgerRequest.current) return;
    setPlans(newPlans); setAudits(newAudits); setSessions(newSessions);
    const nextPlan = newPlans.find((value) => value.id === selectedPlanRef.current);
    if (nextPlan) setSelectedPlan(nextPlan);
    const nextSession = newSessions.find((value) => value.id === sessionRef.current?.id);
    if (nextSession && !sessionActive(nextSession)) pickSession(null);
    else if (nextSession) { sessionRef.current = nextSession; setSession(nextSession); }
  };
  const selectResource = (value: DataResource) => {
    selectionGeneration.current++; selectedResourceRef.current = value.id; resourceStateRef.current = value;
    setSelectedResource(value); setSchema(null); setSelectedTable(null); setQuery(initialQuery); pickPlan(null); setError(""); setEditRow(null); setResults({}); setTableFilter("");
    setCacheEntries([]); setCacheRead(null); setCacheCursor("0"); setNextCursor(null); setCacheKey(""); setCacheValue("");
    setActiveTab(value.provider === "redis" ? "cache" : "query");
  };
  const ensureSession = async (): Promise<DataSession> => {
    if (sessionRef.current && sessionActive(sessionRef.current)) return sessionRef.current;
    const generation = selectionGeneration.current;
    const value = await post<DataSession>(`${base}/sessions`, { name: `管理 ${new Date().toLocaleString()}`, purpose: "MANAGEMENT", expiresInMinutes: 120 });
    if (current(generation)) { sessionRef.current = value; setSession(value); setSessions((old) => [value, ...old]); } return value;
  };
  const inspectSchema = async () => {
    if (!selectedResource?.active) return;
    const generation = selectionGeneration.current; const resource = selectedResource;
    if (!begin("schema")) return;
    setSchema(null); setSelectedTable(null); setQuery(initialQuery); pickPlan(null); setResults({}); setCacheEntries([]); setCacheRead(null); setNextCursor(null); setEditRow(null);
    try {
      const management = await ensureSession();
      if (!current(generation)) return;
      const value = await post<SchemaSnapshot>(`${base}/sessions/${management.id}/schema`, { resourceId: resource.id });
      if (!current(generation) || selectedResourceRef.current !== resource.id) return;
      setSchema(value); setSelectedTable(null); setQuery(initialQuery);
      await refreshLedger();
    } catch (cause) { if (current(generation)) setError(`结构读取失败：${String(cause)}`); } finally { finish(); }
  };
  const chooseTable = (table: TableMetadata) => { selectionGeneration.current++; setSelectedTable(table); setQuery(tableQuery(table)); pickPlan(null); setEditRow(null); };
  const createPlan = async (ir: DataIR) => {
    if (!selectedResource?.active) throw new Error("请选择已启用的连接");
    if (!schema) throw new Error("请先读取并确认连接结构");
    if (schema.resourceVersion !== selectedResource.resourceVersion || schema.policyVersion !== selectedResource.policyVersion) throw new Error("连接或策略版本已变化，请重新读取结构");
    const resource = selectedResource; const generation = selectionGeneration.current; const management = await ensureSession();
    if (!current(generation)) throw new Error("连接或管理会话已切换，请在当前连接生成计划");
    const identity = JSON.stringify([management.id, resource.id, schema.resourceVersion, schema.schemaVersion, ir]);
    if (!planKeys.current.has(identity)) planKeys.current.set(identity, crypto.randomUUID());
    const value = await post<DataPlan>(`${base}/sessions/${management.id}/plans`, { resourceId: resource.id, resourceVersion: resource.resourceVersion, schemaVersion: schema.schemaVersion, ir, idempotencyKey: planKeys.current.get(identity) });
    // Only an uncertain request retains its identity for retry. A new draft gets a new plan.
    planKeys.current.delete(identity);
    if (current()) setPlans((old) => [value, ...old.filter((item) => item.id !== value.id)]);
    if (current(generation) && selectedResourceRef.current === resource.id) pickPlan(value);
    await refreshLedger().catch(() => undefined); return value;
  };
  const runDraft = async (draft: () => DataIR): Promise<boolean> => {
    if (!begin("plan")) return false;
    const generation = selectionGeneration.current;
    try { await createPlan(draft()); return current(generation); } catch (cause) { if (current(generation)) setError(`计划生成失败：${String(cause)}`); return false; } finally { finish(); }
  };
  const decidePlan = async (plan: DataPlan, approved: boolean) => {
    if (!begin("approval")) return;
    const generation = selectionGeneration.current;
    try {
      const value = await post<Approval>(`${base}/plans/${plan.id}/approval`, { digest: plan.digest, approved });
      if (!current()) return;
      if (current(generation) && approved && value.capability) capability.current.set(plan.id, value); else capability.current.delete(plan.id);
      if (current(generation)) setSelectedPlan((old) => old?.id === plan.id ? { ...old, status: value.status } : old);
      await refreshLedger().catch(() => undefined);
    } catch (cause) { if (current(generation)) setError(`批准结果暂无法确认，请刷新审计核对：${String(cause)}`); } finally { finish(); }
  };
  const executePlan = async (plan: DataPlan) => {
    const approval = capability.current.get(plan.id);
    if (!approval?.capability || approval.digest !== plan.digest || !approval.expiresAt || new Date(approval.expiresAt).getTime() <= Date.now()) { setError("当前页面没有有效执行资格，请生成并批准新计划"); return; }
    if (!begin("execute")) return;
    const generation = selectionGeneration.current;
    // Consume in the UI before dispatch. A lost response never triggers automatic external replay.
    capability.current.delete(plan.id);
    try {
      const result = await post<ExecutionResult>(`${base}/plans/${plan.id}/execute`, { digest: plan.digest, capability: approval.capability, ...(plan.taskId ? { attempt: plan.attempt } : {}) });
      if (!current()) return;
      const complete = { ...plan, status: result.status };
      if (current(generation)) {
        setResults((old) => ({ ...old, [plan.id]: { plan: complete, result } }));
        setSelectedPlan((old) => old?.id === plan.id ? complete : old);
      }
      if (current(generation) && selectedResourceRef.current === plan.connectionId && plan.kind === "cache" && result.status === "SUCCEEDED") {
        if (plan.ir.operation === "scan") {
          const entries = Array.isArray(result.result.entries) ? result.result.entries as CacheEntry[] : [];
          setCacheEntries((old) => {
            const map = new Map((plan.ir.cursor === "0" ? [] : old).map((entry) => [entry.key, entry]));
            entries.forEach((entry) => map.set(entry.key, entry)); return [...map.values()];
          });
          setNextCursor(String(result.result.cursor || "0"));
        } else if (plan.ir.operation === "get") setCacheRead(result);
      }
      if (current(generation) && selectedResourceRef.current === plan.connectionId && plan.kind === "migration" && result.status === "COMMITTED") { setSchema(null); setSelectedTable(null); }
      await refreshLedger();
    } catch (cause) {
      if (current(generation)) { setError(`执行结果暂无法确认，请刷新计划和审计核对；不会自动重放：${String(cause)}`); await refreshLedger().catch(() => undefined); }
    } finally { finish(); }
  };
  const explainPlan = async (plan: DataPlan) => {
    if (!begin("explain")) return;
    const generation = selectionGeneration.current;
    try { const value = await post<{ plan: string }>(`${base}/plans/${plan.id}/explain`, { digest: plan.digest }); if (current(generation) && selectedPlanRef.current === plan.id) setExplain(value.plan); }
    catch (cause) { if (current(generation) && selectedPlanRef.current === plan.id) setError(`EXPLAIN 失败：${String(cause)}`); } finally { finish(); }
  };
  const recoverPlan = async (plan: DataPlan) => {
    if (!begin("recovery")) return;
    const generation = selectionGeneration.current;
    try {
      const identity = `recovery:${plan.id}`; if (!planKeys.current.has(identity)) planKeys.current.set(identity, crypto.randomUUID());
      const value = await post<DataPlan>(`${base}/plans/${plan.id}/compensation`, { idempotencyKey: planKeys.current.get(identity) });
      planKeys.current.delete(identity);
      if (current(generation)) { pickPlan(value); setActiveTab("history"); } await refreshLedger().catch(() => undefined);
    } catch (cause) { if (current(generation)) setError(`恢复计划生成失败：${String(cause)}`); } finally { finish(); }
  };
  const cachedResult = selectedResource ? Object.values(results).reverse().find((value) => value.plan.connectionId === selectedResource.id && value.plan.kind === "query" && value.result.status === "SUCCEEDED" && value.plan.ir.schema === selectedTable?.schema && value.plan.ir.table === selectedTable?.name) : undefined;
  const resultRows = Array.isArray(cachedResult?.result.result.rows) ? cachedResult!.result.result.rows as Record<string, unknown>[] : [];
  const resultHashes = Array.isArray(cachedResult?.result.result.rowHashes) ? cachedResult!.result.result.rowHashes as string[] : [];
  const resultTable = schema?.schemaVersion === cachedResult?.plan.schemaVersion ? schema?.metadata.tables?.find((table) => table.schema === cachedResult?.plan.ir.schema && table.name === cachedResult?.plan.ir.table) : undefined;
  const visiblePlans = plans.filter((plan) => !selectedResource || plan.connectionId === selectedResource.id);
  const cacheDraft = (): DataIR => {
    if (cacheOperation === "scan") return compileCache({ kind: "cache", operation: "scan", cursor: cacheCursor, count: cacheCount });
    const ir: DataIR = { kind: "cache", operation: cacheOperation, key: cacheKey };
    if (cacheOperation === "set") ir.value = cacheValue;
    if (cacheOperation === "set" || cacheOperation === "expire") ir.ttlSeconds = cacheTTL;
    return compileCache(ir);
  };

  return <section className="data-page"><div className="data-heading"><div><span className="section-kicker">PROJECT DATA WORKBENCH</span><h1>数据库与缓存</h1><p>{project.name} 的连接、计划、恢复和审计</p></div><div className="heading-actions"><button className="secondary-button" onClick={() => void load()} disabled={!!busy}><RefreshCw size={14}/>刷新</button><button className="primary-button" onClick={() => { setResourceToEdit(null); setShowResourceForm(true); }}><Plus size={15}/>添加连接</button></div></div>
    <div className="data-session-bar"><ShieldCheck size={15}/><span>管理会话</span><select aria-label="管理会话" value={session?.id || ""} disabled={!!busy} onChange={(event) => pickSession(sessions.find((value) => value.id === event.target.value) || null)}><option value="">下一次读取时创建 120 分钟会话</option>{sessions.filter(sessionActive).map((value) => <option key={value.id} value={value.id}>{value.name} · {value.purpose} · 至 {new Date(value.expiresAt).toLocaleTimeString()}</option>)}</select><button className="text-button" onClick={() => setShowSessionForm(true)}><Plus size={13}/>新会话</button>{session && <button className="text-button" disabled={!!busy} onClick={() => { void post<DataSession>(`${base}/sessions/${session.id}/close`, {}).then(() => { if (current()) { pickSession(null); void load(); } }).catch((cause) => setError(String(cause))); }}>结束会话</button>}</div>
    {error && <div className="data-error" role="alert"><XCircle size={15}/><span>{error}</span><button aria-label="关闭错误" onClick={() => setError("")}><X size={14}/></button></div>}
    <div className="data-workbench-layout"><aside className="data-resource-panel"><div className="data-panel-heading"><h2>项目连接</h2><span>{resources.length}</span></div><div className="data-resource-list">{resources.map((value) => <button key={value.id} className={`resource-card ${selectedResource?.id === value.id ? "selected" : ""} ${!value.active ? "inactive" : ""}`} onClick={() => selectResource(value)}><span className={`resource-icon ${value.provider}`}>{value.provider === "redis" ? <KeyRound size={17}/> : <Database size={17}/>}</span><span><strong>{value.name}</strong><small>{value.provider === "redis" ? "Redis" : "PostgreSQL"} · {value.environment} · v{value.resourceVersion}</small></span><span className={`resource-active ${value.active ? "active" : ""}`} title={value.active ? "启用" : "停用"}/></button>)}{!resources.length && <div className="empty-state"><Database size={23}/><p>本工程还没有连接</p><button className="text-button" onClick={() => setShowResourceForm(true)}>添加 PostgreSQL 或 Redis</button></div>}</div>
      {selectedResource && <div className="resource-detail"><div><strong>{selectedResource.name}</strong><button className="icon-button" title="编辑连接" onClick={() => { setResourceToEdit(selectedResource); setShowResourceForm(true); }}><Pencil size={13}/></button></div><small>{selectedResource.id}</small><div className="resource-tags"><span>{selectedResource.environment}</span><span>资源 v{selectedResource.resourceVersion}</span><span>{selectedResource.policyVersion}</span></div><button className="secondary-button full-width" onClick={() => void inspectSchema()} disabled={!selectedResource.active || !!busy}>{busy === "schema" ? <Loader2 size={14}/> : <RefreshCw size={14}/>}读取连接与结构</button>{schema && <><small className="schema-version">结构 {schema.schemaVersion.slice(0, 16)}…</small>{selectedResource.provider === "postgres" && <div className="schema-tree"><div className="schema-tree-search"><Search size={12}/><input aria-label="筛选数据库表" value={tableFilter} onChange={(event) => setTableFilter(event.target.value)} placeholder="搜索表"/></div>{(schema.metadata.tables || []).filter((table) => tableKey(table).toLocaleLowerCase().includes(tableFilter.toLocaleLowerCase())).map((table) => <div className="schema-table" key={tableKey(table)}><button className={tableKey(selectedTable || { schema: "", name: "" } as TableMetadata) === tableKey(table) ? "selected" : ""} onClick={() => chooseTable(table)}><Layers3 size={13}/><span>{tableKey(table)}</span><small>{table.columns.length}</small></button>{selectedTable && tableKey(selectedTable) === tableKey(table) && <div className="schema-columns">{table.columns.map((column) => <button key={column.name} draggable onDragStart={(event) => { event.dataTransfer.setData("application/proofcode-column", JSON.stringify({ table: tableKey(table), column: column.name })); event.dataTransfer.effectAllowed = "copy"; }} title={`${column.type}${column.comment ? ` · ${column.comment}` : ""}`} onClick={() => setQuery((old) => ({ ...old, selected: old.selected.includes(column.name) ? old.selected : [...old.selected, column.name] }))}><GripVertical size={11}/><span>{column.name}</span>{table.primaryKey.includes(column.name) && <KeyRound size={10}/>}<small>{column.type}</small></button>)}</div>}</div>)}</div>}{selectedResource.provider === "redis" && <div className="namespace-card"><small>受控命名空间</small><code>{schema.metadata.namespace}</code><p>Standalone · string · v{schema.metadata.namespaceVersion}</p></div>}</>}
      </div>}
    </aside><div className="data-main"><div className="data-tabs">{(selectedResource?.provider === "redis" ? [["cache", "缓存管理"]] : [["query", "查询画布"], ["migration", "结构变更"]]).concat([["history", "计划与恢复"], ["audit", "审计记录"]]).map(([tab, label]) => <button key={tab} className={activeTab === tab ? "active" : ""} onClick={() => setActiveTab(tab as typeof activeTab)}>{label}</button>)}</div>
      {activeTab === "query" && <><div className="query-canvas"><div className="data-panel-heading"><div><h2>{selectedTable ? tableKey(selectedTable) : "单表查询画布"}</h2><p>拖入字段、添加条件、排序与分页，再生成查询计划。</p></div><span className="draft-badge">草稿</span></div>{selectedTable ? <><QueryCanvas table={selectedTable} query={query} onChange={setQuery}/><div className="draft-footer"><span><LockKeyhole size={12}/>读取也需要批准</span><button className="primary-button" disabled={!!busy || !schema} onClick={() => void runDraft(() => compileQuery(selectedTable, query))}>{busy === "plan" ? <Loader2 size={14}/> : <FileCode2 size={14}/>}生成查询计划</button></div><details className="ir-draft"><summary>查看 Query IR 草稿</summary><DraftPreview draft={() => compileQuery(selectedTable, query)}/></details></> : <div className="canvas-empty"><Database size={28}/><strong>选择连接并读取结构</strong><span>从左侧选择一个表，字段可以拖入查询画布。</span></div>}</div>{cachedResult && <ResultGrid data={cachedResult} table={resultTable} rows={resultRows} hashes={resultHashes} onEdit={(row, rowHash) => { if (resultTable) setEditRow({ table: resultTable, original: row, rowHash }); }} busy={!!busy || !schema} onPage={(offset) => void runDraft(() => queryPage(cachedResult.plan.ir, offset))}/>}</>}
      {activeTab === "cache" && <div className="cache-workbench"><div className="data-panel-heading"><div><h2>Redis 缓存管理</h2><p>逻辑键映射到当前项目、连接和环境的命名空间。</p></div></div><div className="cache-operation-form"><label>操作<select value={cacheOperation} onChange={(event) => setCacheOperation(event.target.value as typeof cacheOperation)}><option value="scan">SCAN 分页</option><option value="get">读取字符串</option><option value="set">设置字符串</option><option value="delete">删除键</option><option value="expire">设置 TTL</option></select></label>{cacheOperation === "scan" ? <><label>游标<input inputMode="numeric" value={cacheCursor} onChange={(event) => setCacheCursor(event.target.value)}/></label><label>COUNT<input type="number" min={1} max={100} value={cacheCount} onChange={(event) => setCacheCount(Number(event.target.value))}/></label></> : <label className="cache-key-input">逻辑键<input value={cacheKey} onChange={(event) => setCacheKey(event.target.value)} maxLength={200} placeholder="例如 session:123"/></label>}{(cacheOperation === "set" || cacheOperation === "expire") && <label>TTL / 秒<input type="number" min={1} max={86400} value={cacheTTL} onChange={(event) => setCacheTTL(Number(event.target.value))}/></label>}{cacheOperation === "set" && <label className="cache-value-input">字符串值<textarea value={cacheValue} onChange={(event) => setCacheValue(event.target.value)} rows={4}/></label>}<button className="primary-button" disabled={!!busy || !schema || !selectedResource?.active || (cacheOperation !== "scan" && !cacheKey)} onClick={() => void runDraft(cacheDraft)}><FileCode2 size={14}/>生成缓存计划</button></div><p className="data-help">SCAN 和 GET 也会生成计划。分页结果可能重复或变化，页面按逻辑键去重。</p><div className="cache-key-list"><div className="data-panel-heading"><h2>已批准读取的键</h2><span>{cacheEntries.length}</span></div><div className="data-table-wrap"><table className="data-table"><thead><tr><th>逻辑键</th><th>类型</th><th>TTL</th><th>字节</th><th>读取时间</th></tr></thead><tbody>{cacheEntries.map((entry) => <tr key={entry.key}><td><button className="key-link" onClick={() => { setCacheKey(entry.key); setCacheOperation("get"); setCacheRead(null); }}>{entry.key}</button></td><td>{entry.type}</td><td>{entry.ttlMs < 0 ? entry.ttlMs === -1 ? "无期限" : "不存在" : `${Math.floor(entry.ttlMs / 1000)} s`}</td><td>{entry.sizeBytes ?? "—"}</td><td>{entry.readAt ? new Date(entry.readAt).toLocaleTimeString() : "—"}</td></tr>)}</tbody></table></div>{!cacheEntries.length && <div className="empty-state">生成并执行 SCAN 计划后显示键元数据</div>}{nextCursor && nextCursor !== "0" && <button className="secondary-button cache-next" disabled={!!busy} onClick={() => { setCacheCursor(nextCursor); setCacheOperation("scan"); void runDraft(() => compileCache({ kind: "cache", operation: "scan", cursor: nextCursor, count: cacheCount })); }}>下一页生成计划 · {nextCursor}</button>}{nextCursor === "0" && <small className="cache-complete">当前 SCAN 游标已完成</small>}</div>{cacheRead && <div className="cache-value-card"><div className="data-panel-heading"><h2>已批准读取的值</h2><span>{cacheRead.result.type as string}</span></div><pre>{formatCell(cacheRead.result.value)}</pre><div className="resource-tags"><span>TTL {Number(cacheRead.result.ttlMs)} ms</span><span>{Number(cacheRead.result.sizeBytes)} bytes</span><span>generation {Number(cacheRead.result.generation)}</span></div><small>{String(cacheRead.result.readAt || "")}</small></div>}</div>}
      {activeTab === "migration" && <MigrationDraft key={`${session?.id || ""}:${selectedResource?.id || ""}:${schema?.schemaVersion || ""}`} tables={schema?.metadata.tables || []} allowedSchemas={parseJSON<string[]>(selectedResource?.allowedSchemas, [])} busy={!!busy} enabled={!!schema && selectedResource?.provider === "postgres" && !!selectedResource.active} onPlan={(ir) => void runDraft(() => ir)}/>}
      {activeTab === "history" && <div className="data-history"><div className="data-panel-heading"><h2>不可变计划与恢复</h2><span>{visiblePlans.length}</span></div>{visiblePlans.map((plan) => <button className={`data-plan-row ${selectedPlan?.id === plan.id ? "selected" : ""}`} key={plan.id} onClick={() => pickPlan(plan)}><span className="plan-kind">{plan.kind === "compensation" ? <RotateCcw size={15}/> : <FileCode2 size={15}/>}</span><span><strong>{plan.preview}</strong><small>{resources.find((resource) => resource.id === plan.connectionId)?.name || plan.connectionId} · {new Date(plan.createdAt).toLocaleString()} · {plan.digest.slice(0, 12)}</small></span><DataStatus status={plan.status}/></button>)}{!visiblePlans.length && <div className="empty-state">还没有数据操作计划</div>}</div>}
      {activeTab === "audit" && <div className="data-audit"><div className="data-panel-heading"><h2>项目审计</h2><span>{audits.length}</span></div><div className="data-table-wrap"><table className="data-table"><thead><tr><th>时间</th><th>操作</th><th>状态</th><th>计划</th><th>详情</th></tr></thead><tbody>{audits.map((audit, index) => <tr key={audit.id || `${audit.operationId}-${audit.action}-${index}`}><td>{new Date(audit.createdAt).toLocaleString()}</td><td>{audit.action}</td><td><DataStatus status={audit.status}/></td><td><button className="text-button" onClick={() => { const plan = plans.find((value) => value.id === audit.operationId); if (plan) { pickPlan(plan); setActiveTab("history"); } }}>{audit.operationId?.slice(0, 8)}</button></td><td><details><summary>元数据</summary><pre className="audit-detail">{JSON.stringify(parseJSON(audit.details, audit.details), null, 2)}</pre></details></td></tr>)}</tbody></table></div>{!audits.length && <div className="empty-state">暂无审计记录</div>}</div>}
    </div><aside className="data-plan-inspector">{selectedPlan ? <PlanInspector plan={selectedPlan} resource={resources.find((value) => value.id === selectedPlan.connectionId)} approval={capability.current.get(selectedPlan.id)} result={results[selectedPlan.id]?.result} explain={explain} busy={!!busy} onApprove={(approved) => void decidePlan(selectedPlan, approved)} onExecute={() => void executePlan(selectedPlan)} onExplain={() => void explainPlan(selectedPlan)} onRecovery={() => void recoverPlan(selectedPlan)}/> : <div className="plan-empty"><ShieldCheck size={25}/><strong>先预览，再批准</strong><p>生成的 SQL 或缓存操作会显示在这里。拖动字段和编辑草稿不会执行数据库操作。</p></div>}</aside></div>
    {showResourceForm && <ResourceForm base={base} resource={resourceToEdit} onClose={() => setShowResourceForm(false)} onSaved={(value) => { if (!current()) return; setResources((old) => [value, ...old.filter((item) => item.id !== value.id)]); selectResource(value); setShowResourceForm(false); onNotice(`连接 ${value.name} 已保存`); }}/>} {showSessionForm && <SessionForm base={base} onClose={() => setShowSessionForm(false)} onSaved={(value) => { if (current()) { pickSession(value); setSessions((old) => [value, ...old]); setShowSessionForm(false); } }}/>} {editRow && <RowEditor row={editRow} onClose={() => setEditRow(null)} busy={!!busy} onPlan={(values) => { const editing = editRow; void runDraft(() => rowMutation(editing.table, editing.original, values, editing.rowHash)).then((created) => { if (created) setEditRow(null); }); }}/>}
  </section>;
}

function QueryCanvas({ table, query, onChange }: { table: TableMetadata; query: QueryDraft; onChange: (query: QueryDraft) => void }) {
  const dragged = useRef<string | null>(null);
  const addColumn = (name: string, before?: string) => {
    if (!table.columns.some((column) => column.name === name)) return;
    const next = query.selected.filter((column) => column !== name);
    const at = before ? next.indexOf(before) : -1;
    if (at >= 0) next.splice(at, 0, name); else next.push(name);
    onChange({ ...query, selected: next });
  };
  const receiveColumn = (event: React.DragEvent, before?: string) => {
    event.preventDefault(); event.stopPropagation();
    const encoded = event.dataTransfer.getData("application/proofcode-column");
    if (encoded) {
      const value = parseJSON<{ table: string; column: string } | null>(encoded, null);
      if (value?.table === tableKey(table)) addColumn(value.column, before);
    } else if (dragged.current) addColumn(dragged.current, before);
    dragged.current = null;
  };
  const updateGroup = (group: FilterGroup) => onChange({ ...query, groups: query.groups.map((value) => value.id === group.id ? group : value) });
  return <div className="query-canvas-body"><section className="projection-board" onDragOver={(event) => { event.preventDefault(); event.dataTransfer.dropEffect = "copy"; }} onDrop={(event) => receiveColumn(event)}><div className="canvas-section-title"><Layers3 size={14}/><strong>返回字段</strong><button className="text-button" onClick={() => onChange({ ...query, selected: table.columns.map((column) => column.name) })}>全选</button><button className="text-button" onClick={() => onChange({ ...query, selected: [] })}>清空</button></div><div className="projection-columns">{query.selected.map((name, index) => <div className="projection-column" key={name} draggable onDragStart={(event) => { dragged.current = name; event.dataTransfer.setData("application/proofcode-column", JSON.stringify({ table: tableKey(table), column: name })); event.dataTransfer.effectAllowed = "move"; }} onDragOver={(event) => event.preventDefault()} onDrop={(event) => receiveColumn(event, name)}><GripVertical size={13}/><span>{name}</span><small>{index + 1}</small><button aria-label={`移除 ${name}`} onClick={() => onChange({ ...query, selected: query.selected.filter((column) => column !== name) })}><X size={12}/></button></div>)}{!query.selected.length && <span className="projection-placeholder">拖入左侧字段，或点击字段添加</span>}</div></section>
    <div className="table-key-metadata"><span><KeyRound size={12}/>主键 {table.primaryKey.join(", ") || "无"}</span>{table.foreignKeys.map((key) => <span key={key.name}><GitBranch size={12}/>{key.columns.join(", ")} → {key.referencesSchema}.{key.referencesTable}</span>)}{table.indexes.map((index) => <span key={index.name}>{index.unique ? "唯一索引" : "索引"} {index.name} ({index.columns.join(", ")})</span>)}</div>
    <section className="filter-board"><div className="canvas-section-title"><Filter size={14}/><strong>筛选条件</strong>{query.groups.length > 1 && <label className="filter-connective">组间<select value={query.connective} onChange={(event) => onChange({ ...query, connective: event.target.value as "and" | "or" })}><option value="and">AND</option><option value="or">OR</option></select></label>}<button className="text-button" onClick={() => onChange({ ...query, groups: [...query.groups, { id: crypto.randomUUID(), operator: "and", filters: [{ id: crypto.randomUUID(), column: table.columns[0]?.name || "", operator: "eq", value: "" }] }] })}><Plus size={12}/>条件组</button></div>{query.groups.map((group) => <div className="filter-group" key={group.id}><div className="filter-group-header"><select aria-label="组内逻辑运算" value={group.operator} onChange={(event) => updateGroup({ ...group, operator: event.target.value as "and" | "or" })}><option value="and">组内 AND</option><option value="or">组内 OR</option></select><button className="text-button" onClick={() => updateGroup({ ...group, filters: [...group.filters, { id: crypto.randomUUID(), column: table.columns[0]?.name || "", operator: "eq", value: "" }] })}><Plus size={12}/>条件</button><button className="icon-button" title="删除条件组" onClick={() => onChange({ ...query, groups: query.groups.filter((value) => value.id !== group.id) })}><Trash2 size={12}/></button></div>{group.filters.map((filter) => <div className="filter-row" key={filter.id}><select aria-label="条件字段" value={filter.column} onChange={(event) => updateGroup({ ...group, filters: group.filters.map((value) => value.id === filter.id ? { ...value, column: event.target.value } : value) })}>{table.columns.map((column) => <option key={column.name} value={column.name}>{column.name} · {column.type}</option>)}</select><select aria-label="条件运算符" value={filter.operator} onChange={(event) => updateGroup({ ...group, filters: group.filters.map((value) => value.id === filter.id ? { ...value, operator: event.target.value } : value) })}>{supportedOperators.map(([operator, label]) => <option key={operator} value={operator}>{label}</option>)}</select>{!["is_null", "is_not_null"].includes(filter.operator) ? <input aria-label="条件值" value={filter.value} onChange={(event) => updateGroup({ ...group, filters: group.filters.map((value) => value.id === filter.id ? { ...value, value: event.target.value } : value) })} placeholder={filter.operator === "in" ? 'JSON 数组，如 [1,2] 或 ["a","b"]' : "按字段类型输入值"}/> : <span className="filter-no-value">无参数</span>}<button className="icon-button" title="删除条件" onClick={() => updateGroup({ ...group, filters: group.filters.filter((value) => value.id !== filter.id) })}><X size={13}/></button></div>)}</div>)}{!query.groups.length && <p className="canvas-help">无筛选条件。查询会受分页和服务端数量限制。</p>}</section>
    <section className="sort-board"><div className="canvas-section-title"><ChevronDown size={14}/><strong>排序与分页</strong><button className="text-button" disabled={query.orderBy.length >= Math.min(8, table.columns.length)} onClick={() => { const column = table.columns.find((value) => !query.orderBy.some((sort) => sort.column === value.name)); if (column) onChange({ ...query, orderBy: [...query.orderBy, { column: column.name, direction: "asc" }] }); }}><Plus size={12}/>排序</button></div><div className="sort-rows">{query.orderBy.map((sort, index) => <div className="sort-row" key={index}><select aria-label="排序字段" value={sort.column} onChange={(event) => onChange({ ...query, orderBy: query.orderBy.map((value, position) => position === index ? { ...value, column: event.target.value } : value) })}>{table.columns.map((column) => <option key={column.name}>{column.name}</option>)}</select><select aria-label="排序方向" value={sort.direction} onChange={(event) => onChange({ ...query, orderBy: query.orderBy.map((value, position) => position === index ? { ...value, direction: event.target.value as "asc" | "desc" } : value) })}><option value="asc">ASC</option><option value="desc">DESC</option></select><button className="icon-button" title="删除排序" onClick={() => onChange({ ...query, orderBy: query.orderBy.filter((_, position) => position !== index) })}><X size={12}/></button></div>)}</div><div className="pagination-fields"><label>每页行数<input type="number" min={1} max={1000} value={query.limit} onChange={(event) => onChange({ ...query, limit: Number(event.target.value) })}/></label><label>Offset<input type="number" min={0} max={100000} value={query.offset} onChange={(event) => onChange({ ...query, offset: Number(event.target.value) })}/></label><span>主键会自动补入稳定排序。</span></div></section>
  </div>;
}
function DraftPreview({ draft }: { draft: () => DataIR }) { try { return <pre>{JSON.stringify(draft(), null, 2)}</pre>; } catch (cause) { return <p className="draft-validation">{String(cause)}</p>; } }
function DataStatus({ status }: { status: string }) {
  const labels: Record<string, string> = { PENDING: "待批准", APPROVED: "已批准", DENIED: "已拒绝", EXECUTING: "执行中", SUCCEEDED: "读取成功", COMMITTED: "已提交", FAILED_ROLLED_BACK: "失败已回滚", FAILED_PRECONDITION: "前置条件失败", COMMIT_UNKNOWN: "提交结果未知", COMPENSATED: "已恢复", COMPENSATION_CONFLICT: "恢复冲突", COMPENSATION_UNKNOWN: "恢复结果未知" };
  return <span className={`data-status ${status.toLowerCase()}`}>{labels[status] || status}</span>;
}
function PlanInspector({ plan, resource, approval, result, explain, busy, onApprove, onExecute, onExplain, onRecovery }: { plan: DataPlan; resource?: DataResource; approval?: Approval; result?: ExecutionResult; explain: string; busy: boolean; onApprove: (approved: boolean) => void; onExecute: () => void; onExplain: () => void; onRecovery: () => void }) {
  const validApproval = !!approval?.capability && !!approval.expiresAt && new Date(approval.expiresAt).getTime() > Date.now();
  const unknown = ["COMMIT_UNKNOWN", "COMPENSATION_UNKNOWN", "EXECUTING"].includes(plan.status);
  return <div className="plan-inspector"><div className="data-panel-heading"><h2>{plan.kind === "compensation" ? "恢复计划" : "计划预览"}</h2><DataStatus status={plan.status}/></div><div className="plan-target"><strong>{resource?.name || plan.connectionId}</strong><span>{resource?.provider} · {resource?.environment}</span>{resource?.environment === "production" && <p className="production-warning">生产环境：请核对操作范围与参数。</p>}</div><dl className="plan-binding"><div><dt>资源版本</dt><dd>{plan.resourceVersion}</dd></div><div><dt>策略</dt><dd>{plan.policyVersion}</dd></div><div><dt>结构版本</dt><dd title={plan.schemaVersion}>{plan.schemaVersion.slice(0, 16)}…</dd></div><div><dt>上下文</dt><dd>{plan.dataSessionId ? `管理 ${plan.dataSessionId.slice(0, 8)}` : `任务 ${plan.taskId?.slice(0, 8)} / ${plan.attempt}`}</dd></div></dl><label className="inspector-label">SQL / 操作预览<pre className="plan-sql">{plan.preview}</pre></label><label className="inspector-label">不可变摘要<code className="plan-digest">{plan.digest}</code></label><details className="plan-ir" open><summary>参数与 IR</summary><pre>{JSON.stringify(plan.ir, null, 2)}</pre></details>{plan.kind === "query" && <button className="secondary-button full-width" disabled={busy} onClick={onExplain}><Search size={13}/>EXPLAIN（不执行 ANALYZE）</button>}{explain && <details className="explain-result" open><summary>EXPLAIN 结果</summary><pre>{JSON.stringify(parseJSON(explain, explain), null, 2)}</pre></details>}
    {plan.status === "PENDING" && <div className="plan-approval-actions"><p>批准绑定此连接、结构和参数摘要。执行会单独消费一次性资格。</p><button className="primary-button" disabled={busy} onClick={() => onApprove(true)}><Check size={14}/>批准此计划</button><button className="secondary-button" disabled={busy} onClick={() => onApprove(false)}><X size={14}/>拒绝</button></div>}{plan.status === "APPROVED" && <div className="plan-execution"><p>{validApproval ? `执行资格至 ${new Date(approval!.expiresAt!).toLocaleTimeString()}，只保存在当前页面。` : "此页面没有执行资格，或资格已过期。请生成并批准新计划。"}</p><button className="primary-button full-width" disabled={busy || !validApproval} onClick={onExecute}>{busy ? <Loader2 size={14}/> : <Play size={14}/>}执行已批准计划</button></div>}{unknown && <p className="unknown-outcome">结果需要核对。刷新计划和审计查看证据，不能自动重放此操作。</p>}{plan.resultSummary && <div className="plan-summary"><strong>持久结果摘要</strong><pre>{JSON.stringify(parseJSON(plan.resultSummary, plan.resultSummary), null, 2)}</pre></div>}{result && <div className="plan-summary"><strong>本次结果</strong><pre>{JSON.stringify(result.result, null, 2)}</pre></div>}{recoveryAvailable(plan) && <div className="plan-recovery"><p>恢复会创建新计划并重新批准。当前行或键有并发变化时会拒绝覆盖。</p><button className="secondary-button full-width" disabled={busy} onClick={onRecovery}><RotateCcw size={14}/>生成条件恢复计划</button></div>}{plan.kind === "migration" && plan.status === "COMMITTED" && <p className="data-help">事务已提交的结构变更不提供通用自动撤销。失败事务会由服务端回滚并记录。</p>}
  </div>;
}
function ResultGrid({ data, table, rows, hashes, busy, onEdit, onPage }: { data: PlanResult; table?: TableMetadata; rows: Record<string, unknown>[]; hashes: string[]; busy: boolean; onEdit: (row: Record<string, unknown>, hash: string) => void; onPage: (offset: number) => void }) {
  const selected = Array.isArray(data.plan.ir.select) ? data.plan.ir.select as string[] : Object.keys(rows[0] || {});
  const editable = !!table?.primaryKey.length && hashes.length === rows.length && hashes.every((hash) => /^[a-f0-9]{64}$/.test(hash));
  const limit = Number(data.plan.ir.limit || 50), offset = Number(data.plan.ir.offset || 0);
  return <section className="query-results"><div className="data-panel-heading"><div><h2>已批准查询结果</h2><p>{String(data.plan.ir.schema)}.{String(data.plan.ir.table)} · {rows.length} 行 · Offset {offset}</p></div><DataStatus status={data.result.status}/></div><div className="data-table-wrap"><table className="data-table"><thead><tr>{selected.map((column) => <th key={column}>{column}</th>)}{editable && <th>行操作</th>}</tr></thead><tbody>{rows.map((row, index) => <tr key={`${index}-${hashes[index] || ""}`}>{selected.map((column) => <td key={column} title={formatCell(row[column])}>{formatCell(row[column])}</td>)}{editable && <td><button className="text-button" onClick={() => onEdit(row, hashes[index])}><Pencil size={12}/>编辑草稿</button></td>}</tr>)}</tbody></table></div>{!rows.length && <div className="empty-state">查询返回 0 行</div>}<div className="result-pagination"><button className="secondary-button" disabled={busy || offset === 0} onClick={() => onPage(Math.max(0, offset - limit))}>上一页生成计划</button><span>{data.result.result.truncated ? "结果达到字节限制" : rows.length ? `${offset + 1} – ${offset + rows.length}` : "0 行"}</span><button className="secondary-button" disabled={busy || rows.length < limit || offset + limit > 100000} onClick={() => onPage(offset + limit)}>下一页生成计划</button></div>{!editable && <p className="data-help">行编辑需要主键及全部字段的行版本；当前结果只用于浏览。分页始终基于原批准查询生成新计划。</p>}</section>;
}

function ResourceForm({ base, resource, onClose, onSaved }: { base: string; resource: DataResource | null; onClose: () => void; onSaved: (value: DataResource) => void }) {
  const [name, setName] = useState(resource?.name || "");
  const [provider, setProvider] = useState<"postgres" | "redis">(resource?.provider || "postgres");
  const [environment, setEnvironment] = useState(resource?.environment || "dev");
  const [secretRef, setSecretRef] = useState(""); const [allowedSchema, setAllowedSchema] = useState(parseJSON<string[]>(resource?.allowedSchemas, ["public"]).join(", "));
  const [active, setActive] = useState(resource?.active ?? true); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  const mounted = useRef(true), pending = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (pending.current) return; pending.current = true; setBusy(true); setError("");
    try {
      const body = resourceRequest({ name, provider, environment, secretRef, allowedSchema, active }, resource);
      let saved: DataResource;
      if (resource) {
        saved = await api<DataResource>(`${base}/resources/${resource.id}`, { method: "PATCH", body: JSON.stringify(body) });
      } else saved = await post<DataResource>(`${base}/resources`, body);
      if (mounted.current) onSaved(saved);
    } catch (cause) { if (mounted.current) setError(String(cause)); } finally { pending.current = false; if (mounted.current) setBusy(false); }
  };
  return <div className="modal-backdrop"><form className="modal data-modal" onSubmit={(event) => void submit(event)}><div className="modal-head"><div><span className="section-kicker">PROJECT CONNECTION</span><h2>{resource ? "更新项目连接" : "添加数据库或缓存"}</h2></div><button type="button" className="icon-button" title="关闭" onClick={onClose}><X size={17}/></button></div><label>连接名称<input required maxLength={120} value={name} onChange={(event) => setName(event.target.value)} placeholder="例如 开发数据库"/></label><div className="form-grid"><label>类型<select disabled={!!resource} value={provider} onChange={(event) => setProvider(event.target.value as typeof provider)}><option value="postgres">PostgreSQL</option><option value="redis">Redis</option></select></label><label>环境<select value={environment} onChange={(event) => setEnvironment(event.target.value as typeof environment)}><option value="dev">开发</option><option value="staging">预发布</option><option value="production">生产</option></select></label></div><label>{resource ? "新凭据引用（留空保留当前凭据）" : "服务端凭据引用"}<input required={!resource} pattern="[A-Z][A-Z0-9_]{0,79}" maxLength={80} autoComplete="off" value={secretRef} onChange={(event) => setSecretRef(event.target.value)} placeholder="例如 DEV_POSTGRES"/></label><p className="form-help">引用已由服务端配置的凭据。连接密码保存在服务端，项目接口只保存引用并返回非敏感元数据。</p>{provider === "postgres" && <label>允许的 schema<input required value={allowedSchema} onChange={(event) => setAllowedSchema(event.target.value)} placeholder="public, reporting"/></label>}{resource && <label className="checkbox-label"><input type="checkbox" checked={active} onChange={(event) => setActive(event.target.checked)}/>启用连接</label>}{resource && <p className="form-help">基于资源版本 {resource.resourceVersion} 更新。连接、策略或凭据变化会使旧计划批准失效。</p>}{error && <div className="capsule-error">{error}</div>}<div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>取消</button><button type="submit" className="primary-button" disabled={busy}>{busy ? <Loader2 size={14}/> : <Check size={14}/>}保存连接</button></div></form></div>;
}
function SessionForm({ base, onClose, onSaved }: { base: string; onClose: () => void; onSaved: (value: DataSession) => void }) {
  const [name, setName] = useState(`管理 ${new Date().toLocaleDateString()}`); const [minutes, setMinutes] = useState(120); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  const mounted = useRef(true), pending = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const submit = async (event: FormEvent) => { event.preventDefault(); if (pending.current) return; pending.current = true; setBusy(true); setError(""); try { const saved = await post<DataSession>(`${base}/sessions`, { name, purpose: "MANAGEMENT", expiresInMinutes: minutes }); if (mounted.current) onSaved(saved); } catch (cause) { if (mounted.current) setError(String(cause)); } finally { pending.current = false; if (mounted.current) setBusy(false); } };
  return <div className="modal-backdrop"><form className="modal" onSubmit={(event) => void submit(event)}><div className="modal-head"><h2>新建管理会话</h2><button type="button" className="icon-button" title="关闭" onClick={onClose}><X size={17}/></button></div><label>名称<input required maxLength={120} value={name} onChange={(event) => setName(event.target.value)}/></label><label>有效分钟<input type="number" required min={5} max={1440} value={minutes} onChange={(event) => setMinutes(Number(event.target.value))}/></label><p className="form-help">管理会话独立于代码任务。操作计划仍需逐项批准；会话结束或过期后无法执行旧资格。</p>{error && <div className="capsule-error">{error}</div>}<div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>取消</button><button className="primary-button" disabled={busy} type="submit">创建会话</button></div></form></div>;
}
function RowEditor({ row, onClose, onPlan, busy }: { row: EditRow; onClose: () => void; onPlan: (values: Record<string, unknown> | null) => void; busy: boolean }) {
  const [values, setValues] = useState(JSON.stringify(row.original, null, 2)); const [error, setError] = useState("");
  const submit = (event: FormEvent) => {
    event.preventDefault(); setError("");
    try {
      const parsed: unknown = JSON.parse(values);
      if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") throw new Error("行值必须为 JSON 对象");
      rowMutation(row.table, row.original, parsed as Record<string, unknown>, row.rowHash);
      onPlan(parsed as Record<string, unknown>);
    } catch (cause) { setError(String(cause)); }
  };
  return <div className="modal-backdrop"><form className="modal data-modal row-editor" onSubmit={submit}><div className="modal-head"><div><span className="section-kicker">ROW MUTATION DRAFT</span><h2>编辑 {tableKey(row.table)}</h2></div><button type="button" className="icon-button" title="关闭" onClick={onClose}><X size={17}/></button></div><p className="form-help">主键和生成字段不能修改。计划锁定原行哈希与 expectedRows=1，数据变化会拒绝执行。</p><label>字段值（保留 JSON 类型）<textarea className="json-editor" value={values} onChange={(event) => setValues(event.target.value)} rows={14}/></label><small className="row-hash">原行版本 {row.rowHash}</small>{error && <div className="capsule-error">{error}</div>}<div className="modal-actions"><button type="button" className="danger-button" disabled={busy} onClick={() => onPlan(null)}><Trash2 size={13}/>生成删除计划</button><button type="button" className="secondary-button" onClick={onClose}>取消</button><button type="submit" className="primary-button" disabled={busy}><FileCode2 size={13}/>生成修改计划</button></div></form></div>;
}
function MigrationDraft({ tables, allowedSchemas, enabled, busy, onPlan }: { tables: TableMetadata[]; allowedSchemas: string[]; enabled: boolean; busy: boolean; onPlan: (ir: DataIR) => void }) {
  type DraftColumn = { id: string; name: string; type: string; nullable: boolean; primary: boolean };
  const [operation, setOperation] = useState<"create_table" | "add_column" | "create_index">("add_column");
  const [schema, setSchema] = useState(allowedSchemas[0] || "public"); const [table, setTable] = useState(""); const [indexName, setIndexName] = useState("");
  const [columns, setColumns] = useState<DraftColumn[]>([{ id: crypto.randomUUID(), name: "", type: "text", nullable: true, primary: false }]);
  const [indexColumns, setIndexColumns] = useState<string[]>([]); const [steps, setSteps] = useState<DataIR[]>([]); const [error, setError] = useState("");
  const types = ["text", "integer", "bigint", "boolean", "date", "timestamp", "uuid"];
  const selected = tables.find((value) => value.schema === schema && value.name === table);
  useEffect(() => { setSchema(allowedSchemas[0] || "public"); setSteps([]); setTable(""); setIndexColumns([]); }, [allowedSchemas.join(",")]);
  const addStep = () => {
    setError("");
    try {
      const identifier = /^[A-Za-z_][A-Za-z0-9_]{0,62}$/;
      if (!identifier.test(schema) || !identifier.test(table)) throw new Error("schema 和表名需要合法标识符");
      if (steps.length >= 8) throw new Error("每份迁移最多 8 个步骤");
      let step: DataIR;
      if (operation === "create_table") {
        if (!columns.length || columns.length > 32 || columns.some((column) => !identifier.test(column.name)) || new Set(columns.map((column) => column.name)).size !== columns.length) throw new Error("列名无效或重复");
        if (!columns.some((column) => column.primary) || columns.filter((column) => column.primary).length > 4) throw new Error("创建表需要 1 到 4 个主键字段");
        if (tables.some((value) => value.schema === schema && value.name === table) || steps.some((value) => value.operation === "create_table" && value.table === table)) throw new Error("表已存在或已加入创建步骤");
        step = { operation, table, columns: columns.map(({ name, type, nullable }) => ({ name, type, nullable })), primaryKey: columns.filter((column) => column.primary).map((column) => column.name) };
      } else if (operation === "add_column") {
        const column = columns[0]; if (!selected) throw new Error("请选择当前结构中已存在的表"); if (selected.columns.some((value) => value.name === column.name)) throw new Error("列已存在"); if (!identifier.test(column.name)) throw new Error("列名无效");
        step = { operation, table, column: { name: column.name, type: column.type, nullable: true } };
      } else {
        if (!selected || !identifier.test(indexName) || !indexColumns.length || indexColumns.length > 4) throw new Error("请选择索引字段并输入索引名称");
        step = { operation, table, name: indexName, columns: indexColumns };
      }
      setSteps((old) => [...old, step]);
    } catch (cause) { setError(String(cause)); }
  };
  return <div className="migration-draft"><div className="data-panel-heading"><div><h2>受限结构变更</h2><p>类型化迁移草稿；批准后同一事务执行，失败时回滚。</p></div><span className="draft-badge">草稿</span></div><div className="migration-form"><label>Schema<select value={schema} onChange={(event) => { setSchema(event.target.value); setSteps([]); setTable(""); }}>{allowedSchemas.length ? allowedSchemas.map((value) => <option key={value}>{value}</option>) : <option>public</option>}</select></label><label>步骤<select value={operation} onChange={(event) => { setOperation(event.target.value as typeof operation); setColumns([{ id: crypto.randomUUID(), name: "", type: "text", nullable: true, primary: false }]); }}><option value="add_column">新增可空列</option><option value="create_table">创建表</option><option value="create_index">创建普通索引</option></select></label><label>表名<input required list="migration-tables" value={table} onChange={(event) => { setTable(event.target.value); setIndexColumns([]); }}/><datalist id="migration-tables">{tables.filter((value) => value.schema === schema).map((value) => <option key={value.name} value={value.name}/>)}</datalist></label>{operation === "create_index" ? <><label>索引名称<input value={indexName} onChange={(event) => setIndexName(event.target.value)}/></label><div className="migration-index-columns">{selected?.columns.map((column) => <label className="checkbox-label" key={column.name}><input type="checkbox" checked={indexColumns.includes(column.name)} onChange={(event) => setIndexColumns((old) => event.target.checked ? [...old, column.name] : old.filter((value) => value !== column.name))}/>{column.name}</label>)}</div></> : <div className="migration-columns">{columns.map((column, index) => <div className="migration-column" key={column.id}><input aria-label="新列名" value={column.name} onChange={(event) => setColumns((old) => old.map((value) => value.id === column.id ? { ...value, name: event.target.value } : value))} placeholder="列名"/><select aria-label="新列类型" value={column.type} onChange={(event) => setColumns((old) => old.map((value) => value.id === column.id ? { ...value, type: event.target.value } : value))}>{types.map((value) => <option key={value}>{value}</option>)}</select>{operation === "create_table" && <><label className="checkbox-label"><input type="checkbox" checked={column.nullable} onChange={(event) => setColumns((old) => old.map((value) => value.id === column.id ? { ...value, nullable: event.target.checked } : value))}/>可空</label><label className="checkbox-label"><input type="checkbox" checked={column.primary} onChange={(event) => setColumns((old) => old.map((value) => value.id === column.id ? { ...value, primary: event.target.checked, nullable: event.target.checked ? false : value.nullable } : value))}/>主键</label>{index > 0 && <button className="icon-button" title="移除列" onClick={() => setColumns((old) => old.filter((value) => value.id !== column.id))}><X size={12}/></button>}</>}</div>)}{operation === "create_table" && <button className="text-button" onClick={() => setColumns((old) => [...old, { id: crypto.randomUUID(), name: "", type: "text", nullable: true, primary: false }])}><Plus size={12}/>增加列</button>}</div>}<button className="secondary-button" disabled={!enabled || busy} onClick={addStep}><Plus size={13}/>加入迁移步骤</button></div>{error && <div className="data-error">{error}</div>}<div className="migration-steps">{steps.map((step, index) => <div key={index}><span>#{index + 1} {String(step.operation)} · {String(step.table)}</span><button className="icon-button" title="移除步骤" onClick={() => setSteps((old) => old.filter((_, position) => position !== index))}><X size={13}/></button></div>)}{!steps.length && <p className="canvas-help">先读取结构，再加入最多 8 个受限步骤。</p>}</div><details className="ir-draft"><summary>Migration IR 草稿</summary><pre>{JSON.stringify({ kind: "migration", schema, steps }, null, 2)}</pre></details><div className="draft-footer"><span>结构变更提交后不开放通用自动撤销。</span><button className="primary-button" disabled={!enabled || busy || !steps.length} onClick={() => onPlan({ kind: "migration", schema, steps })}><FileCode2 size={14}/>生成迁移计划</button></div></div>;
}
