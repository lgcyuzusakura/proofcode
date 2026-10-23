import type { TaskEvent } from "./types";

export function selectedResourceEvents(events: TaskEvent[], attempt?: number): TaskEvent[] {
  const ordered = [...new Map(events.map(event => [event.sequence, event])).values()]
    .filter(event => Number.isSafeInteger(event.sequence) && event.sequence > 0)
    .sort((a, b) => a.sequence - b.sequence);
  const selected = attempt ?? ordered.reduce((latest, event) => Math.max(latest, event.attempt ?? 1), 1);
  return ordered.filter(event => (event.attempt ?? 1) === selected);
}

export function currentResourceRun(events: TaskEvent[]): TaskEvent[] {
  const start = [...events].reverse().find(event => event.type === "task.started");
  const runId = start?.runId ?? [...events].reverse().find(event => event.runId != null)?.runId;
  return events.filter(event => event.runId === runId && (!start || event.sequence >= start.sequence));
}

export function requestUsageEvents(events: TaskEvent[]): TaskEvent[] {
  return events.flatMap(event => {
    if (event.type === "usage.updated") return [event];
    if (event.type !== "agent.delegated" || event.payload.kind !== "usage.updated" || typeof event.payload.agentId !== "string") return [];
    const payload = event.payload.payload;
    return payload && typeof payload === "object" && !Array.isArray(payload)
      ? [{ ...event, payload: payload as Record<string, unknown> }]
      : [];
  });
}

const validNumber = (value: unknown): value is number => typeof value === "number" && Number.isFinite(value) && value >= 0;

export function resourceCostSummary(usage: TaskEvent[]): string {
  const priced = usage.filter(item => item.payload.requestUsageReported !== false && validNumber(item.payload.requestEstimatedCost) && typeof item.payload.costCurrency === "string" && item.payload.costCurrency.length > 0);
  if (!priced.length) return "未估算";
  const currencies = new Set(priced.map(item => String(item.payload.costCurrency)));
  if (currencies.size !== 1) return "多币种（见详情）";
  return `${priced.reduce((sum, item) => sum + Number(item.payload.requestEstimatedCost), 0).toFixed(6)} ${[...currencies][0]}${priced.length < usage.length ? "（部分）" : ""}`;
}

export function resourceTokenSummary(usage: TaskEvent[]): string {
  const reported = usage.filter(item => item.payload.requestUsageReported !== false && validNumber(item.payload.requestInputTokens) && validNumber(item.payload.requestOutputTokens));
  if (!reported.length) return "未报告";
  const total = reported.reduce((sum, item) => sum + Number(item.payload.requestInputTokens) + Number(item.payload.requestOutputTokens), 0);
  return `${total.toLocaleString()}${reported.length < usage.length ? "（部分）" : ""}`;
}

export type ResourceChild = { id: string; name: string; status: string; result: string; usage: Record<string, unknown>; costs: TaskEvent[] };

export function resourceChildren(events: TaskEvent[]): ResourceChild[] {
  const children = new Map<string, ResourceChild>();
  const terminal = new Set<string>();
  for (const event of events) {
    if (event.type !== "agent.delegated" || typeof event.payload.agentId !== "string") continue;
    const raw = event.payload.payload;
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) continue;
    const data = raw as Record<string, unknown>;
    const id = event.payload.agentId;
    const child = children.get(id) ?? { id, name: id, status: "排队中", result: "", usage: {}, costs: [] };
    if (event.payload.kind === "usage.updated") {
      child.costs.push({ ...event, payload: data });
      if (!terminal.has(id)) child.usage = data;
    }
    if (!terminal.has(id)) {
      if (event.payload.kind === "queued" || event.payload.kind === "started") {
        child.name = String(data.name || child.name);
        child.status = event.payload.kind === "queued" ? "排队中" : "运行中";
      }
      if (event.payload.kind === "completed") {
        child.name = String(data.name || child.name);
        child.status = ({ completed: "已完成", failed: "失败", cancelled: "已取消", interrupted: "已中断" } as Record<string, string>)[String(data.status)] || "已结束";
        child.result = String(data.result || data.error || "");
        child.usage = data.usage && typeof data.usage === "object" && !Array.isArray(data.usage)
          ? { reported: false, ...data.usage as Record<string, unknown> }
          : { reported: false };
        terminal.add(id);
      }
    }
    children.set(id, child);
  }
  return [...children.values()];
}
