import { useMemo, useState } from "react";
import { Search, Wrench } from "lucide-react";
import type { TaskEvent } from "./types";
import { toolLabel } from "./presentation";
import { buildToolCatalog } from "./tool-catalog-model";
import { activityDuration, activityText } from "./activityModel";

const states: Record<string, string> = { requested: "已请求", approval_required: "等待批准", started: "执行中", completed: "已完成", failed: "失败" };
const risks: Record<string, string> = { read: "读取", write: "写入", exec: "执行" };
export function ToolCatalog({ events, attempt }: { events: TaskEvent[]; attempt?: number }) {
  const { tools, readOnly, reported } = useMemo(() => buildToolCatalog(events, attempt), [events, attempt]);
  const [query, setQuery] = useState(""), [source, setSource] = useState("all"), [activity, setActivity] = useState("all");
  const q = query.trim().toLocaleLowerCase();
  const visible = tools.filter(tool => (!q || `${tool.name} ${toolLabel(tool.name)} ${tool.description}`.toLocaleLowerCase().includes(q)) && (source === "all" || tool.source === source) && (activity === "all" || activity === "used" && tool.calls.length > 0 || activity === "failed" && tool.calls.some(call => call.state === "failed") || activity === "approval" && tool.calls.some(call => call.state === "approval_required")));
  return <section className="runtime-tool-catalog" aria-label="本次运行工具目录">
    <div className="runtime-tool-filters"><label className="runtime-tool-search"><Search size={15}/><input aria-label="搜索可用工具" value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索名称或用途"/></label><select aria-label="工具来源" value={source} onChange={event => setSource(event.target.value)}><option value="all">全部来源</option><option value="builtin">内置工具</option><option value="mcp">MCP 服务</option></select><select aria-label="工具调用状态" value={activity} onChange={event => setActivity(event.target.value)}><option value="all">全部状态</option><option value="used">已调用</option><option value="failed">有失败记录</option><option value="approval">等待批准</option></select></div>
    <p className="runtime-tool-summary" role="status">{reported ? `显示 ${visible.length} / ${tools.length} 个工具${readOnly ? "，本次运行受只读限制" : ""}` : "本次运行尚未报告工具目录"}</p>
    {!reported ? <p className="runtime-tool-empty">代码或规划任务开始后，这里显示实际注册的工具；普通聊天仅在配置 Skills 时提供读取技能的工具。</p> : !visible.length ? <p className="runtime-tool-empty">{tools.length ? "没有匹配的工具，调整搜索或筛选条件。" : "本次运行没有注册工具。"}</p> : <div className="runtime-tool-list">{visible.map(tool => <details key={tool.name} className="runtime-tool-entry"><summary><Wrench size={16}/><span className="runtime-tool-title"><strong>{toolLabel(tool.name)}</strong><code>{tool.name}</code><span>{tool.description}</span></span><span className="runtime-tool-source">{tool.source === "mcp" ? "MCP" : "内置"}</span><span className="runtime-tool-count">{tool.calls.length} 次</span></summary><div className="runtime-tool-detail"><p>风险按实际调用参数判定{readOnly ? "；只读模式会拦截有副作用的操作" : ""}。{tool.source === "mcp" && "外部 MCP 调用需要明确批准。"}</p><h3>输入参数</h3>{tool.parameters != null ? <pre tabIndex={0}>{activityText(tool.parameters)}</pre> : <p>本次运行未报告参数格式。</p>}<h3>调用记录</h3>{!tool.calls.length ? <p>尚未调用此工具。</p> : tool.calls.slice(-10).map(call => <details className="runtime-tool-call" key={call.id}><summary><span>{states[call.state] || call.state}</span><code>{call.id}</code><span>{call.risk ? risks[call.risk] : "风险未报告"}</span>{call.durationMs != null && <span>{activityDuration(call.durationMs)}</span>}</summary>{call.input != null && <><h4>调用参数</h4><pre tabIndex={0}>{activityText(call.input)}</pre></>}{call.output != null && <><h4>返回记录</h4><pre tabIndex={0}>{activityText(call.output)}</pre></>}</details>)}{tool.calls.length > 10 && <p>显示最近 10 次调用，完整记录保存在执行链中。</p>}</div></details>)}</div>}
  </section>;
}
