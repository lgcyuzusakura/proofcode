import type { TaskEvent } from "./types";
import { toolLabel } from "./presentation";

export type ActivityTone = "observe" | "decide" | "working" | "success" | "warning" | "error" | "muted";
export type ActivityItem = { key: string; title: string; summary: string; tone: ActivityTone; timestamp: string; durationMs?: number; event: TaskEvent; input?: unknown; output?: unknown; pending?: boolean; agent?: string };
export const isTaskActive = (status: string) => ["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"].includes(status);
const record = (value: unknown): Record<string, unknown> => value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
export function activityText(value: unknown): string {
  return typeof value === "string" ? value : value == null ? "" : JSON.stringify(value, null, 2);
}
const brief = (value: unknown) => activityText(value).replace(/\s+/g, " ").trim().slice(0, 220);
function inputSummary(value: unknown): string {
  const data = record(value);
  if (data.path) return brief(data.path);
  if (Array.isArray(data.paths)) return data.paths.map(String).join(" · ").slice(0, 220);
  if (data.program) return `${data.program} ${Array.isArray(data.args) ? data.args.join(" ") : ""}`.trim().slice(0, 220);
  return brief(data.query || data.pattern || data.command || data.url || value);
}
const callKey = (event: TaskEvent) => `${event.attempt ?? ""}:${event.runId ?? ""}:${event.payload.callId || `event-${event.sequence}`}`;
export function activityDuration(ms: number): string { return `${(ms / 1000).toFixed(ms >= 10000 ? 1 : 2)} s`; }

// A view of persisted events, never a generated plan or an inferred success.
// Keep start/result milestones separate; only suppress a redundant request
// once the same call has actually started or reached an approval/result.
export function buildActivities(events: TaskEvent[]): ActivityItem[] {
  const ordered = [...new Map(events.map(event => [event.sequence, event])).values()].sort((a, b) => a.sequence - b.sequence);
  const advanced = new Set(ordered.filter(event => event.type.startsWith("tool.") && event.type !== "tool.requested").map(callKey));
  const requests = new Map<string, TaskEvent>();
  const starts = new Map<string, TaskEvent>();
  const agents = new Map<string, string>();
  const items: ActivityItem[] = [];
  for (const event of ordered) {
    const p = event.payload || {};
    let title = "", summary = "", tone: ActivityTone = "muted", pending = false;
    let durationMs: number | undefined, input: unknown, output: unknown, agent: string | undefined;
    if (event.type === "task.started") { title = "开始处理任务"; summary = p.executionMode === "CHAT" ? "正在读取对话上下文，准备回复" : "正在读取工程上下文，准备执行"; tone = "observe"; }
    else if (event.type.startsWith("tool.")) {
      const key = callKey(event);
      if (event.type === "tool.requested") { requests.set(key, event); if (advanced.has(key)) continue; }
      if (event.type === "tool.started") starts.set(key, event);
      const name = toolLabel(String(p.tool || requests.get(key)?.payload.tool || "工具"));
      input = p.arguments ?? p.input ?? requests.get(key)?.payload.arguments;
      output = p.error ?? p.result;
      const labels: Record<string, string> = { "tool.requested": "已选择", "tool.started": "正在执行", "tool.completed": "已完成", "tool.failed": "执行失败", "tool.approval_required": "等待批准" };
      if (!labels[event.type]) continue;
      title = `${labels[event.type]}：${name}`;
      tone = event.type === "tool.completed" ? "success" : event.type === "tool.failed" ? "error" : event.type === "tool.approval_required" ? "warning" : event.type === "tool.requested" ? "decide" : "working";
      pending = event.type === "tool.started" || event.type === "tool.requested";
      summary = (output != null ? brief(output) : inputSummary(input)) || (event.type === "tool.approval_required" ? "需要你确认后才会执行，可打开审查处理" : event.type === "tool.completed" ? "工具已返回结果" : event.type === "tool.failed" ? "工具返回失败，请展开查看记录" : "等待工具返回结果");
      const start = starts.get(key);
      if (start && ["tool.completed", "tool.failed"].includes(event.type)) {
        const elapsed = Date.parse(event.timestamp) - Date.parse(start.timestamp);
        if (Number.isFinite(elapsed) && elapsed >= 0) durationMs = elapsed;
      }
    } else if (event.type.startsWith("decision.")) {
      title = event.type === "decision.tool_route_fallback" ? "已回退到模型决策" : p.applied === false ? "已评估下一步" : "已确定下一步";
      summary = brief(p.reason || p.error || (p.choice ? toolLabel(String(p.choice)) : "")) || "已记录本次工具选择"; tone = "decide";
    } else if (event.type === "context.selected") {
      if (!p.kind && !Number(p.droppedMessages)) continue;
      title = p.kind === "compression" ? (p.error ? "上下文压缩未完成" : "已压缩上下文") : p.kind === "retrieval" ? "已检索代码上下文" : "已调整上下文";
      summary = p.kind === "compression" ? p.error ? brief(p.error) : `估算 Token：${p.beforeEstimatedTokens ?? "—"} → ${p.afterEstimatedTokens ?? "—"}，原始记录仍保留` : brief(p.query || p.reason) || (p.droppedMessages ? `本次输入裁剪了 ${p.droppedMessages} 条消息，原始记录仍保留` : "已选择本次任务需要的代码证据");
      tone = p.error ? "warning" : "observe";
    } else if (event.type === "agent.delegated") {
      const data = record(p.payload), id = String(p.agentId || "");
      if (data.name) agents.set(id, String(data.name));
      agent = agents.get(id) || id || "子任务";
      if (p.kind === "started") { title = "已启动协作任务"; summary = agent; tone = "decide"; }
      else if (p.kind === "completed") {
        const status = String(data.status || "");
        tone = status === "completed" ? "success" : status === "failed" ? "error" : "muted";
        title = status === "completed" ? "协作任务已完成" : status === "failed" ? "协作任务失败" : status === "cancelled" ? "协作任务已取消" : "协作任务已结束";
        summary = brief(data.result || data.error) || agent; output = data.result || data.error;
      } else if (p.kind === "tool.started" || p.kind === "tool.completed" || p.kind === "tool.failed") {
        title = `${p.kind === "tool.started" ? "协作中" : p.kind === "tool.completed" ? "协作完成" : "协作失败"}：${toolLabel(String(data.tool || "工具"))}`;
        tone = p.kind === "tool.completed" ? "success" : p.kind === "tool.failed" ? "error" : "working";
        summary = brief(data.result || data.arguments) || agent; output = data.result; pending = p.kind === "tool.started";
      } else continue;
    } else if (event.type === "verification.completed") {
      const report = record(p.report), failed = p.passed === false || report.passed === false;
      const passed = p.passed === true || report.passed === true;
      title = failed ? "验证未通过" : passed ? "验证已通过" : "验证结果已返回";
      tone = failed ? "error" : passed ? "success" : "observe";
      summary = brief(p.output || p.report) || "展开查看验证报告"; output = p.output ?? p.report;
    } else if (event.type === "checkpoint.created" || event.type === "recovery.created") {
      title = event.type === "checkpoint.created" ? "已保存代码检查点" : "已保存恢复补丁";
      summary = [p.branch, p.commit && String(p.commit).slice(0, 12)].filter(Boolean).join(" · ") || "可在审查中查看修改产物"; tone = "success";
    } else if (event.type === "workspace.updated") {
      title = "工作区变更预览已更新";
      summary = p.state === "ready" ? "已记录当前累计修改" : p.state === "oversized" ? "修改较大，结束后可查看完整审查产物" : "预览暂不可用，任务仍可继续";
      tone = p.state === "ready" ? "observe" : "warning";
    } else if (event.type === "task.completed") { title = "任务已完成"; summary = p.executionMode === "CHAT" ? "回复已保存" : p.unchanged ? "任务已完成，本次没有代码变更" : "执行记录已保存，可打开审查查看修改"; tone = "success"; }
    else if (event.type === "task.failed") { title = "任务未完成"; summary = brief(p.error) || "展开查看错误记录"; tone = "error"; output = p.error; }
    else if (event.type === "task.cancelled") { title = "任务已停止"; summary = brief(p.reason) || "本次运行已取消"; tone = "muted"; }
    else continue;
    items.push({ key: `${event.attempt ?? ""}:${event.runId ?? ""}:${event.sequence}`, title, summary, tone, timestamp: event.timestamp, durationMs, event, input, output, pending, agent });
  }
  return items;
}
