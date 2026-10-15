import { CheckCircle2, ChevronDown, Loader2, TerminalSquare, XCircle } from "lucide-react";
import type { TaskEvent } from "./types";
import { toolLabel } from "./presentation";

// Collapse durable lifecycle events into one row per actual tool call.
export function ToolActivity({ events }: { events: TaskEvent[] }) {
  const calls = new Map<string, { name: string; state: string; payload: Record<string, unknown> }>();
  for (const event of events) {
    if (!event.type.startsWith("tool.")) continue;
    const payload = event.payload || {};
    const key = `${event.runId || event.attempt || ""}:${String(payload.callId || `${payload.tool || "tool"}:${event.sequence}`)}`;
    const old = calls.get(key);
    calls.set(key, { name: String(payload.tool || old?.name || "工具"), state: event.type, payload: { ...old?.payload, ...payload } });
  }
  if (!calls.size) return null;
  return <details className="tool-activity">
    <summary><TerminalSquare size={16}/><span>工具执行</span><span className="tool-count">{calls.size}</span><ChevronDown size={14}/></summary>
    <div className="tool-activity-list">{Array.from(calls.entries()).map(([key, call]) => {
      const complete = call.state === "tool.completed", failed = call.state === "tool.failed";
      const detail = call.payload.result ?? call.payload.error ?? call.payload.arguments ?? call.payload.input;
      return <details className={`tool-activity-row ${failed ? "failed" : ""}`} key={key}>
        <summary>{complete ? <CheckCircle2 size={14}/> : failed ? <XCircle size={14}/> : <Loader2 size={14}/>}<span>{toolLabel(call.name)}</span><small>{complete ? "完成" : failed ? "失败" : call.state === "tool.requested" ? "已请求" : "执行中"}</small><ChevronDown size={12}/></summary>
        {detail != null ? <pre>{typeof detail === "string" ? detail : JSON.stringify(detail, null, 2)}</pre> : <p>暂无详细输出</p>}
      </details>;
    })}</div>
  </details>;
}
