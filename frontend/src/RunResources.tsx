import { Bot, Coins, Database, Gauge, Layers3, Timer, Wrench, Zap } from "lucide-react";
import { useEffect, useState } from "react";
import type { Task, TaskEvent } from "./types";
import { toolLabel } from "./presentation";

const number = (value: unknown) => typeof value === "number" ? value.toLocaleString() : "—";
const prettyTokens = (value: number) => value >= 1000000 ? `${(value / 1000000).toFixed(value % 1000000 ? 1 : 0)}m` : value >= 1000 ? `${Math.round(value / 1000)}k` : String(value);
const decimal = (value: unknown, digits = 1) => typeof value === "number" && Number.isFinite(value) ? value.toLocaleString(undefined, { maximumFractionDigits: digits }) : "—";
const duration = (value: unknown) => typeof value === "number" && Number.isFinite(value) ? value < 1000 ? `${Math.round(value)} ms` : `${(value / 1000).toFixed(value >= 10000 ? 1 : 2)} s` : "—";
const requestNumber = (payload: Record<string, unknown> | undefined, field: string) => payload?.requestUsageReported === false ? "未报告" : number(payload?.[field]);
const hitRate = (payload: Record<string, unknown> | undefined) => payload?.requestCacheReported && payload?.requestUsageReported !== false && Number(payload.requestInputTokens) > 0 ? `${decimal(Number(payload.requestCachedInputTokens) / Number(payload.requestInputTokens) * 100)}%` : "未报告";
function costSummary(usage: TaskEvent[]) {
  const priced = usage.filter(item => typeof item.payload.requestEstimatedCost === "number");
  if (!priced.length) return "未估算";
  const currencies = new Set(priced.map(item => String(item.payload.costCurrency || "")));
  if (currencies.size !== 1) return "多币种（见详情）";
  return `${priced.reduce((sum, item) => sum + Number(item.payload.requestEstimatedCost), 0).toFixed(6)} ${[...currencies][0]}${priced.length < usage.length ? "（部分）" : ""}`;
}
export function RunUsageSummary({ events, task }: { events: TaskEvent[]; task: Task }) {
  const usage = events.filter(e => e.type === "usage.updated" && typeof e.payload.requestInputTokens === "number");
  const last = usage[usage.length - 1]?.payload;
  const [now, setNow] = useState(Date.now());
  const active = ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(task.status);
  useEffect(() => { setNow(Date.now()); if (!active) return; const timer = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, [active, task.id]);
  const start = events.find(e => e.type === "task.started");
  const end = [...events].reverse().find(e => ["task.completed", "task.failed", "task.cancelled"].includes(e.type));
  const elapsed = start && (active || end) ? Math.max(0, (end ? new Date(end.timestamp).getTime() : now) - new Date(start.timestamp).getTime()) : null;
  if (!last && elapsed == null) return null;
  return <div className="run-usage-summary" aria-label="回合用量统计"><span><Timer size={12}/>回合用时 <b>{duration(elapsed)}</b></span><span title="最近一次模型请求收到首段文本的时间">首字 <b>{duration(last?.requestFirstTokenMs)}</b></span><span title="最近一次模型请求的总耗时">请求 <b>{duration(last?.requestDurationMs)}</b></span><span title="按服务商输出 Token 和首字后的请求耗时估算">速度 <b>{last?.requestTokensPerSecond != null ? `${decimal(last.requestTokensPerSecond)} t/s` : "—"}</b></span><span>Token <b>{last?.usageReported === false ? "未完整报告" : number(last?.totalTokens)}</b></span><span>缓存读 <b>{last?.requestCacheReported ? number(last.requestCachedInputTokens) : "未报告"}</b></span><span>命中 <b>{hitRate(last)}</b></span><span>费用 <b>{costSummary(usage)}</b></span></div>;
}
export function ContextUsage({ events }: { events: TaskEvent[] }) {
  const usage = events.filter(value => value.type === "usage.updated" && typeof value.payload.requestInputTokens === "number");
  const last = usage[usage.length - 1]?.payload;
  const compressions = events.filter(value => value.type === "context.selected" && value.payload.kind === "compression");
  const compressed = compressions[compressions.length - 1]?.payload;
  const requestInput = Number(last?.requestInputTokens || 0), capacity = Number(last?.contextCeiling || 128000);
  const durationTotal = usage.reduce((sum, item) => sum + (typeof item.payload.requestDurationMs === "number" ? item.payload.requestDurationMs : 0), 0);
  const compression = ({gentle:"轻度",balanced:"均衡",aggressive:"强力"} as Record<string,string>)[String(last?.compression || "balanced")];
  return <section className="run-resources"><div className="run-resources-heading"><h2><Layers3 size={17}/>上下文、缓存与速度</h2><span>{prettyTokens(capacity)} 上限 · 输入 {prettyTokens(Number(last?.defaultInputBudget || 16000))} · 输出 {prettyTokens(Number(last?.maxOutputTokens || 2048))} · 压缩 {compression}</span></div>
    <div className="run-resource-metrics"><div><span><Timer size={13}/>首字时间</span><strong>{duration(last?.requestFirstTokenMs)}</strong></div><div><span><Gauge size={13}/>本次总用时</span><strong>{duration(last?.requestDurationMs)}</strong></div><div><span><Zap size={13}/>输出速度</span><strong>{last?.requestTokensPerSecond != null ? `${decimal(last.requestTokensPerSecond)} t/s` : "未报告"}</strong></div><div><span>本次输入 / 输出</span><strong>{requestNumber(last, "requestInputTokens")} / {requestNumber(last, "requestOutputTokens")}</strong></div><div><span>缓存读 / 写</span><strong>{last?.requestCacheReported ? number(last.requestCachedInputTokens) : "未报告"} / {last?.requestCacheWriteReported ? number(last.requestCacheWriteTokens) : "未报告"}</strong></div><div><span>主任务累计 Token</span><strong>{last?.usageReported === false ? "未完整报告" : number(last?.totalTokens)}</strong></div><div><span><Coins size={13}/>本回合费用</span><strong>{costSummary(usage)}</strong></div><div><span>缓存命中率</span><strong>{hitRate(last)}</strong></div><div><span>累计请求用时</span><strong>{durationTotal ? duration(durationTotal) : "—"}</strong></div></div>
    <div className="context-capacity"><div style={{width:`${Math.min(100, requestInput / capacity * 100)}%`}}/></div><p className="run-resource-help">首字和耗时由本地计时；速度按输出 Token 估算。缓存只展示上报值。费用按单次输入量选择整档价格；缺少用量或价格时不估算。</p>
    {compressed && <div className="compression-evidence"><span>最近压缩（估算）</span><strong>{number(compressed.beforeEstimatedTokens)} → {number(compressed.afterEstimatedTokens)}</strong><span>原始记录可按引用回取</span></div>}
    {usage.length > 0 && <div className="request-usage-table"><table><thead><tr><th>请求</th><th>首字</th><th>总用时</th><th>速度</th><th>输入 / 输出</th><th>缓存读 / 写</th><th>费用</th></tr></thead><tbody>{usage.slice(-12).map((item,index) => <tr key={`${item.attempt}:${item.sequence}`}><td>{usage.length > 12 ? usage.length - 11 + index : index + 1}</td><td>{duration(item.payload.requestFirstTokenMs)}</td><td>{duration(item.payload.requestDurationMs)}</td><td>{item.payload.requestTokensPerSecond != null ? `${decimal(item.payload.requestTokensPerSecond)} t/s` : "未报告"}</td><td>{requestNumber(item.payload, "requestInputTokens")} / {requestNumber(item.payload, "requestOutputTokens")}</td><td>{item.payload.requestCacheReported ? number(item.payload.requestCachedInputTokens) : "未报告"} / {item.payload.requestCacheWriteReported ? number(item.payload.requestCacheWriteTokens) : "未报告"}</td><td>{typeof item.payload.requestEstimatedCost === "number" ? `${item.payload.requestEstimatedCost.toFixed(6)} ${String(item.payload.costCurrency || "")}` : "—"}</td></tr>)}</tbody></table></div>}
  </section>;
}
export function AgentTeam({ events }: { events: TaskEvent[] }) {
  const children = new Map<string,{name:string;status:string;result:string;usage:Record<string,unknown>}>();
  for (const event of events) {
    if (event.type !== "agent.delegated" || typeof event.payload.agentId !== "string") continue;
    const data = event.payload.payload as Record<string,unknown> || {};
    const child = children.get(event.payload.agentId) || {name:event.payload.agentId,status:"运行中",result:"",usage:{}};
	if (event.payload.kind === "started") child.name = String(data.name || child.name);
    if (event.payload.kind === "usage.updated") child.usage = data;
    if (event.payload.kind === "completed") { child.name = String(data.name || child.name); child.status = ({completed:"已完成",failed:"失败",cancelled:"已取消",interrupted:"已中断"} as Record<string,string>)[String(data.status)] || String(data.status); child.result = String(data.result || data.error || ""); if(data.usage && typeof data.usage === "object")child.usage = data.usage as Record<string,unknown>; }
    children.set(event.payload.agentId,child);
  }
  return <section className="run-resources"><div className="run-resources-heading"><h2><Bot size={17}/>子任务协作</h2><span>按需启动 · 并发 1 · 最多 4 个 · 输入 12k / 输出 1k</span></div>{children.size ? <div className="child-task-list">{Array.from(children.entries()).map(([id,child]) => <article key={id}><div><strong>{child.name}</strong><span>{child.status}</span></div><p>{child.result || "正在调查工程，可与主任务互发消息"}</p><small>{child.usage.usageReported === false || child.usage.reported === false ? "用量未完整报告" : <>输入 {number(child.usage.inputTokens)} · 输出 {number(child.usage.outputTokens)}</>}</small></article>)}</div> : <p className="run-resource-help">本回合尚未启动子任务。子任务只读调查，使用独立上下文并可与主任务互发消息；用量单独统计。</p>}</section>;
}
type Definition = {name:string;description:string};
export function AvailableTools({events}:{events:TaskEvent[]}) {
  const starts = events.filter(e=>e.type === "task.started");
  const started = starts[starts.length - 1];
  const catalog = Array.isArray(started?.payload.tools) ? started.payload.tools as Definition[] : [];
  const calls = events.filter(e=>e.type === "tool.requested");
  return <div className="tool-catalog">{catalog.length ? catalog.map(tool=><div className="tool-catalog-row" key={tool.name}><span className="tool-catalog-icon"><Wrench size={16}/></span><div><strong>{toolLabel(tool.name)}</strong><small>{tool.description}</small></div><span className="tool-catalog-count">{calls.filter(e=>e.payload.tool === tool.name).length} 次</span></div>) : <p className="run-resource-help"><Database size={16}/>发送代码任务后，这里显示 runner 本回合实际注册的工具。工具不会凭空标记为可用。</p>}</div>;
}
